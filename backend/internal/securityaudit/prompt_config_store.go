package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type activeConfigSnapshot struct {
	storage  storageConfig
	active   ActiveConfig
	loadedAt time.Time
}

type ConfigManager struct {
	db        *sql.DB
	settings  service.SettingRepository
	redis     *redis.Client
	encryptor SecretEncryptor
	clock     Clock
	// encryptionKeyConfigured mirrors cfg.Totp.EncryptionKeyConfigured. With an
	// auto-generated (per-boot) key, newly saved endpoint tokens would become
	// undecryptable after the next restart, so Save rejects them (issue #4887).
	encryptionKeyConfigured bool

	snapshot       atomic.Pointer[activeConfigSnapshot]
	expected       atomic.Int64
	reloadSequence atomic.Uint64
	installMu      sync.Mutex
	// expectedBlocking records the last storage intent that could be decoded,
	// independently of whether endpoint credentials or the full config could be
	// activated. A config version alone cannot distinguish async from blocking.
	expectedBlocking    atomic.Bool
	expectedRiskControl atomic.Bool
	riskControlKnown    atomic.Bool
	// configUntrusted is set when a load/reload fails before a trustworthy
	// snapshot is installed. Combined with expectedBlocking, EffectiveMode
	// fails closed so a persisted blocking policy cannot be silently skipped
	// after startup or invalidation errors. Without blocking intent, untrusted
	// alone must not force ModeBlocking—Prompt Audit is default-off and must
	// not take the gateway down for every API request (see issue #4560).
	configUntrusted atomic.Bool

	stateMu       sync.RWMutex
	lastLoadError string
	lastErrorAt   *time.Time

	lifecycleMu  sync.Mutex
	state        promptLifecycleState
	cancel       context.CancelFunc
	startupDone  chan struct{}
	shutdownDone chan struct{}
	shutdownErr  error
	wg           sync.WaitGroup
}

func NewConfigManager(db *sql.DB, settings service.SettingRepository, redisClient *redis.Client, encryptor service.SecretEncryptor, cfg *config.Config) *ConfigManager {
	return &ConfigManager{
		db: db, settings: settings, redis: redisClient, encryptor: encryptor, clock: realClock{},
		encryptionKeyConfigured: cfg != nil && cfg.Totp.EncryptionKeyConfigured,
	}
}

func (m *ConfigManager) Start(ctx context.Context) error {
	if m == nil {
		return errors.New("prompt audit config manager unavailable")
	}
	m.lifecycleMu.Lock()
	switch m.state {
	case promptLifecycleStarting, promptLifecycleRunning:
		m.lifecycleMu.Unlock()
		return nil
	case promptLifecycleStopping, promptLifecycleStopped:
		m.lifecycleMu.Unlock()
		return ErrPromptAuditNotRestartable
	}
	runCtx, cancel := context.WithCancel(ctx)
	startupDone := make(chan struct{})
	m.state = promptLifecycleStarting
	m.cancel = cancel
	m.startupDone = startupDone
	m.shutdownDone = nil
	m.shutdownErr = nil
	m.lifecycleMu.Unlock()

	loadErr := m.Reload(runCtx)
	if loadErr != nil {
		m.markConfigUntrusted()
	}
	m.wg.Add(1)
	go m.refreshLoop(runCtx)
	if m.redis != nil {
		m.wg.Add(1)
		go m.subscribeLoop(runCtx)
	}

	m.lifecycleMu.Lock()
	if m.state == promptLifecycleStarting {
		// Initial load failures are recoverable degraded startup: refresh and
		// invalidation loops remain alive so a later valid config can activate.
		m.state = promptLifecycleRunning
	}
	m.lifecycleMu.Unlock()
	close(startupDone)
	return loadErr
}

func (m *ConfigManager) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.lifecycleMu.Lock()
	switch m.state {
	case promptLifecycleNew:
		m.lifecycleMu.Unlock()
		return nil
	case promptLifecycleStopped:
		shutdownErr := m.shutdownErr
		m.lifecycleMu.Unlock()
		return shutdownErr
	case promptLifecycleStopping:
		shutdownDone := m.shutdownDone
		m.lifecycleMu.Unlock()
		return m.waitForShutdown(ctx, shutdownDone)
	case promptLifecycleStarting, promptLifecycleRunning:
		m.state = promptLifecycleStopping
		cancel := m.cancel
		startupDone := m.startupDone
		shutdownDone := make(chan struct{})
		m.shutdownDone = shutdownDone
		m.lifecycleMu.Unlock()
		if cancel != nil {
			cancel()
		}
		go m.finishShutdown(startupDone, shutdownDone)
		return m.waitForShutdown(ctx, shutdownDone)
	default:
		m.lifecycleMu.Unlock()
		return nil
	}
}

func (m *ConfigManager) waitForShutdown(ctx context.Context, done <-chan struct{}) error {
	return waitPromptLifecycle(ctx, done, func() error {
		m.lifecycleMu.Lock()
		defer m.lifecycleMu.Unlock()
		return m.shutdownErr
	})
}

func (m *ConfigManager) finishShutdown(startupDone, shutdownDone chan struct{}) {
	if startupDone != nil {
		<-startupDone
	}
	m.wg.Wait()
	m.lifecycleMu.Lock()
	m.cancel = nil
	m.state = promptLifecycleStopped
	m.lifecycleMu.Unlock()
	close(shutdownDone)
}

func (m *ConfigManager) Reload(ctx context.Context) error {
	if m == nil || m.settings == nil {
		m.markUntrustedIfNoActiveSnapshot()
		return errors.New("prompt audit setting repository unavailable")
	}
	// Sequence allocation is serialized with snapshot/state installation. A
	// Save that commits while this reload is reading can advance the fence, and
	// this reload must then be rejected before it changes any runtime state.
	m.installMu.Lock()
	sequence := m.reloadSequence.Add(1)
	m.installMu.Unlock()
	values, err := m.settings.GetMultiple(ctx, []string{SettingKeyPromptAuditConfig, SettingKeyRiskControl})
	if err != nil {
		m.applyReloadFailure(sequence, func() {
			m.riskControlKnown.Store(false)
			m.recordLoadError(err)
			m.markConfigUntrustedLocked()
		})
		return err
	}
	riskControlEnabled, err := parseStoredRiskControl(values[SettingKeyRiskControl])
	if err != nil {
		m.applyReloadFailure(sequence, func() {
			m.riskControlKnown.Store(false)
			m.recordLoadError(err)
			m.markConfigUntrustedLocked()
		})
		return err
	}
	storage, err := ParseStorageConfig(values[SettingKeyPromptAuditConfig])
	if err != nil {
		m.applyReloadFailure(sequence, func() {
			m.observeExpectedStateLocked(values[SettingKeyPromptAuditConfig], riskControlEnabled)
			m.recordLoadError(err)
			m.markConfigUntrustedLocked()
		})
		return err
	}
	active, err := ActiveFromStorage(storage, riskControlEnabled, m.encryptor)
	if err != nil {
		m.applyReloadFailure(sequence, func() {
			m.riskControlKnown.Store(true)
			m.expectedRiskControl.Store(riskControlEnabled)
			m.expected.Store(storage.ConfigVersion)
			m.expectedBlocking.Store(riskControlEnabled && storage.Enabled && storage.BlockingEnabled)
			m.recordLoadError(err)
			// A newer configuration that cannot be activated must not leave an
			// older allow-capable snapshot trusted under risk control.
			m.markUntrustedIfNoActiveOrNewerSnapshotLocked(storage.ConfigVersion)
		})
		return err
	}
	m.installMu.Lock()
	defer m.installMu.Unlock()
	if sequence != m.reloadSequence.Load() {
		return nil
	}
	m.installActiveSnapshotLocked(storage, active, riskControlEnabled)
	LogInfo(EventConfigLoaded, map[string]any{
		"config_version": storage.ConfigVersion, "status": "loaded",
	})
	return nil
}

// applyReloadFailure serializes the failure state of a reload with every
// snapshot installation and reload-fence advance. Checking the sequence and
// updating state must be one critical section; otherwise an older reload can
// pass the check, pause, and overwrite a newer Save's fail-closed metadata.
func (m *ConfigManager) applyReloadFailure(sequence uint64, apply func()) {
	m.installMu.Lock()
	defer m.installMu.Unlock()
	if sequence != m.reloadSequence.Load() {
		return
	}
	apply()
}

func (m *ConfigManager) installActiveSnapshotLocked(storage storageConfig, active ActiveConfig, riskControlEnabled bool) {
	current := m.snapshot.Load()
	if current != nil && current.active.ConfigVersion > active.ConfigVersion {
		return
	}
	previous := current
	m.expected.Store(storage.ConfigVersion)
	m.riskControlKnown.Store(true)
	m.expectedRiskControl.Store(riskControlEnabled)
	m.expectedBlocking.Store(riskControlEnabled && storage.Enabled && storage.BlockingEnabled)
	m.snapshot.Store(&activeConfigSnapshot{
		storage:  cloneStorageConfig(storage),
		active:   cloneActiveConfig(active),
		loadedAt: m.clock.Now(),
	})
	m.configUntrusted.Store(false)
	m.clearLoadError()
	m.logInvalidTokenEndpoints(previous, active)
}

// logInvalidTokenEndpoints warns once per change (not on every 5s refresh)
// when stored endpoint tokens cannot be decrypted with the current key.
func (m *ConfigManager) logInvalidTokenEndpoints(previous *activeConfigSnapshot, active ActiveConfig) {
	invalid := active.InvalidTokenEndpointIDs()
	if len(invalid) == 0 {
		return
	}
	if previous != nil {
		prior := previous.active.InvalidTokenEndpointIDs()
		if len(prior) == len(invalid) {
			same := true
			for i := range invalid {
				if prior[i] != invalid[i] {
					same = false
					break
				}
			}
			if same && previous.active.ConfigVersion == active.ConfigVersion {
				return
			}
		}
	}
	LogWarn(EventConfigTokenInvalid, map[string]any{
		"config_version": active.ConfigVersion, "status": "degraded",
		"error_code": "endpoint_token_undecryptable", "guard_endpoint_id": strings.Join(invalid, ","),
	})
}

func (m *ConfigManager) Active() (ActiveConfig, bool) {
	if m == nil {
		return ActiveConfig{}, false
	}
	snapshot := m.snapshot.Load()
	if snapshot == nil {
		return ActiveConfig{}, false
	}
	return cloneActiveConfig(snapshot.active), true
}

func (m *ConfigManager) BlockingActivationDegraded() bool {
	if m == nil {
		return false
	}
	// Fail closed only when storage intent requires blocking. Untrusted config
	// without blocking intent must not upgrade the whole gateway to ModeBlocking.
	if !m.expectedBlocking.Load() {
		return false
	}
	if m.configUntrusted.Load() {
		return true
	}
	active, ok := m.Active()
	if !ok {
		return true
	}
	// A still-active weaker snapshot after a failed blocking activation must not
	// keep serving allow decisions under the old off/async mode.
	return active.EffectiveMode() != ModeBlocking
}

func (m *ConfigManager) EffectiveMode() Mode {
	if m != nil && m.BlockingActivationDegraded() {
		return ModeBlocking
	}
	active, ok := m.Active()
	if !ok {
		return ModeOff
	}
	return active.EffectiveMode()
}

func (m *ConfigManager) markConfigUntrusted() {
	if m == nil {
		return
	}
	m.installMu.Lock()
	m.markConfigUntrustedLocked()
	m.installMu.Unlock()
}

func (m *ConfigManager) markConfigUntrustedLocked() {
	if m == nil {
		return
	}
	m.configUntrusted.Store(true)
}

func (m *ConfigManager) markUntrustedIfNoActiveSnapshot() {
	m.markUntrustedIfNoActiveOrNewerSnapshot(0)
}

func (m *ConfigManager) markUntrustedIfNoActiveOrNewerSnapshot(expectedVersion int64) {
	if m == nil {
		return
	}
	m.installMu.Lock()
	m.markUntrustedIfNoActiveOrNewerSnapshotLocked(expectedVersion)
	m.installMu.Unlock()
}

func (m *ConfigManager) markUntrustedIfNoActiveOrNewerSnapshotLocked(expectedVersion int64) {
	if m == nil {
		return
	}
	active, ok := m.Active()
	if !ok || (expectedVersion > 0 && expectedVersion > active.ConfigVersion) {
		m.markConfigUntrustedLocked()
	}
}

func (m *ConfigManager) Public() (PublicConfig, error) {
	if m == nil {
		return PublicConfig{}, infraerrors.ServiceUnavailable(ErrorCodeConfigUnavailable, "提示词审计配置暂不可用")
	}
	snapshot := m.snapshot.Load()
	if snapshot == nil {
		return PublicConfig{}, infraerrors.ServiceUnavailable(ErrorCodeConfigUnavailable, "提示词审计配置暂不可用")
	}
	return PublicFromStorage(cloneStorageConfig(snapshot.storage), snapshot.active.RiskControlEnabled, snapshot.active.InvalidTokenEndpointIDs()), nil
}

func (m *ConfigManager) Save(ctx context.Context, req UpdateConfigRequest, actorID int64) (PublicConfig, error) {
	if m == nil || m.db == nil || m.encryptor == nil {
		return PublicConfig{}, errors.New("prompt audit config persistence unavailable")
	}
	if req.ExpectedConfigVersion < 1 {
		return PublicConfig{}, infraerrors.BadRequest("prompt_audit_expected_config_version_required", "必须提供有效的配置版本")
	}
	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return PublicConfig{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, promptAuditConfigLockKey); err != nil {
		return PublicConfig{}, err
	}
	current := DefaultStorageConfig()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, SettingKeyPromptAuditConfig).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PublicConfig{}, err
	}
	if err == nil {
		current, err = ParseStorageConfig(raw)
		if err != nil {
			return PublicConfig{}, err
		}
	}
	if current.ConfigVersion != req.ExpectedConfigVersion {
		return PublicConfig{}, infraerrors.Conflict(ErrorCodeConfigConflict, "提示词审计配置已被其他管理员更新")
	}
	next, err := m.buildNextStorage(current, req, actorID)
	if err != nil {
		return PublicConfig{}, err
	}
	next.ConfigVersion = current.ConfigVersion + 1
	next.UpdatedAt = m.clock.Now()
	next.UpdatedBy = actorID
	next.ChangeSummary = changeSummary(next)
	rawNext, err := json.Marshal(next)
	if err != nil {
		return PublicConfig{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key,value,updated_at) VALUES ($1,$2,NOW())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=EXCLUDED.updated_at`,
		SettingKeyPromptAuditConfig, string(rawNext)); err != nil {
		return PublicConfig{}, err
	}

	// Hold the same linearization lock across the durable commit and fence
	// publication. A Reload may already have read the old row, but it cannot
	// install that result after this commit because its final sequence check is
	// serialized below.
	m.installMu.Lock()
	if err := tx.Commit(); err != nil {
		m.installMu.Unlock()
		return PublicConfig{}, err
	}
	// A committed row is now the newest durable policy even though the global
	// risk-control gate and endpoint secrets still need to be read/activated.
	// Advance the reload fence before any post-commit I/O so an older Reload
	// cannot install a stale allow-capable snapshot after this Save returns.
	activationSequence := m.reloadSequence.Add(1)
	m.expected.Store(next.ConfigVersion)
	m.expectedBlocking.Store(m.expectedRiskControl.Load() && next.Enabled && next.BlockingEnabled)
	m.markConfigUntrustedLocked()
	m.installMu.Unlock()

	// Install the snapshot only when the current global gate is readable and
	// canonical. Treat an unavailable or malformed value as unknown instead of
	// converting it into a trusted ModeOff snapshot after the config commit.
	values, getErr := m.settings.GetMultiple(ctx, []string{SettingKeyRiskControl})
	if getErr != nil {
		m.installMu.Lock()
		if activationSequence == m.reloadSequence.Load() {
			m.riskControlKnown.Store(false)
			m.recordLoadError(getErr)
			m.markConfigUntrustedLocked()
		}
		m.installMu.Unlock()
		return PublicConfig{}, getErr
	}
	riskControlEnabled, parseErr := parseStoredRiskControl(values[SettingKeyRiskControl])
	if parseErr != nil {
		m.installMu.Lock()
		if activationSequence == m.reloadSequence.Load() {
			m.riskControlKnown.Store(false)
			m.recordLoadError(parseErr)
			m.markConfigUntrustedLocked()
		}
		m.installMu.Unlock()
		return PublicConfig{}, parseErr
	}
	active, err := ActiveFromStorage(next, riskControlEnabled, m.encryptor)
	if err != nil {
		m.installMu.Lock()
		if activationSequence == m.reloadSequence.Load() {
			m.riskControlKnown.Store(true)
			m.expectedRiskControl.Store(riskControlEnabled)
			m.expected.Store(next.ConfigVersion)
			m.expectedBlocking.Store(riskControlEnabled && next.Enabled && next.BlockingEnabled)
			m.recordLoadError(err)
			m.markUntrustedIfNoActiveOrNewerSnapshotLocked(next.ConfigVersion)
		}
		m.installMu.Unlock()
		return PublicConfig{}, err
	}
	m.installMu.Lock()
	if activationSequence == m.reloadSequence.Load() {
		m.installActiveSnapshotLocked(next, active, riskControlEnabled)
	}
	m.installMu.Unlock()
	LogInfo(EventConfigUpdated, map[string]any{
		"config_version": next.ConfigVersion, "status": "updated",
	})
	if m.redis != nil {
		if err := m.redis.Publish(ctx, ConfigInvalidationChannel, strconv.FormatInt(next.ConfigVersion, 10)).Err(); err != nil {
			LogWarn(EventConfigReloadDegraded, map[string]any{
				"config_version": next.ConfigVersion, "status": "degraded", "error_code": "config_invalidation_publish_failed",
			})
		}
	}
	return PublicFromStorage(next, active.RiskControlEnabled, active.InvalidTokenEndpointIDs()), nil
}

func (m *ConfigManager) buildNextStorage(current storageConfig, req UpdateConfigRequest, actorID int64) (storageConfig, error) {
	if err := validateUpdateConfigRequest(req); err != nil {
		return storageConfig{}, err
	}
	currentByID := make(map[string]StorageEndpoint, len(current.Endpoints))
	for _, endpoint := range current.Endpoints {
		currentByID[endpoint.ID] = endpoint
	}
	next := storageConfig{
		Enabled: req.Enabled, BlockingEnabled: req.BlockingEnabled, StorePassEvents: req.StorePassEvents,
		Strategy: strings.TrimSpace(req.Strategy), WorkerCount: req.WorkerCount,
		QueueCapacity: req.QueueCapacity, Scanners: append([]string(nil), req.Scanners...),
		AllGroups: req.AllGroups, GroupIDs: append([]int64(nil), req.GroupIDs...),
		ConfigVersion: current.ConfigVersion, UpdatedBy: actorID,
		Endpoints: make([]StorageEndpoint, 0, len(req.Endpoints)),
	}
	for _, endpoint := range req.Endpoints {
		baseURL, err := NormalizeBaseURL(endpoint.BaseURL)
		if err != nil {
			return storageConfig{}, err
		}
		stored := StorageEndpoint{
			ID: strings.TrimSpace(endpoint.ID), Name: strings.TrimSpace(endpoint.Name),
			Protocol: strings.TrimSpace(endpoint.Protocol), BaseURL: baseURL, Model: strings.TrimSpace(endpoint.Model),
			TimeoutMS: endpoint.TimeoutMS, InputLimit: endpoint.InputLimit, Enabled: endpoint.Enabled,
		}
		old, hadOld := currentByID[stored.ID]
		switch {
		case endpoint.ClearToken:
			stored.TokenCiphertext = ""
		case strings.TrimSpace(endpoint.Token) != "":
			if !m.encryptionKeyConfigured {
				return storageConfig{}, infraerrors.BadRequest(ErrorCodeEncryptionKeyRequired,
					"未配置固定加密密钥，审计节点 Token 将在服务重启后失效。请先设置 TOTP_ENCRYPTION_KEY 环境变量（64 位十六进制）并重启服务")
			}
			ciphertext, err := m.encryptor.Encrypt(strings.TrimSpace(endpoint.Token))
			if err != nil {
				return storageConfig{}, fmt.Errorf("encrypt prompt audit endpoint token: %w", err)
			}
			stored.TokenCiphertext = ciphertext
		case hadOld:
			if old.TokenCiphertext != "" {
				oldBaseURL, err := NormalizeBaseURL(old.BaseURL)
				if err != nil {
					return storageConfig{}, err
				}
				if oldBaseURL != stored.BaseURL {
					return storageConfig{}, infraerrors.BadRequest(
						"prompt_audit_token_required_for_base_url_change",
						"更改审计节点地址时必须提供新令牌或明确清除旧令牌",
					)
				}
			}
			stored.TokenCiphertext = old.TokenCiphertext
		}
		next.Endpoints = append(next.Endpoints, stored)
	}
	if err := normalizeStorageConfig(&next); err != nil {
		return storageConfig{}, err
	}
	if err := validateStorageConfig(next); err != nil {
		return storageConfig{}, err
	}
	return next, nil
}

func (m *ConfigManager) RuntimeState() (expected int64, active int64, loadedAt *time.Time, loadError string) {
	if m == nil {
		return 1, 0, nil, "config_manager_unavailable"
	}
	expected = m.expected.Load()
	if expected < 1 {
		expected = 1
	}
	if snapshot := m.snapshot.Load(); snapshot != nil {
		active = snapshot.active.ConfigVersion
		value := snapshot.loadedAt
		loadedAt = &value
	}
	m.stateMu.RLock()
	loadError = m.lastLoadError
	m.stateMu.RUnlock()
	return
}

func (m *ConfigManager) Encrypt(value string) (string, error) { return m.encryptor.Encrypt(value) }
func (m *ConfigManager) Decrypt(value string) (string, error) { return m.encryptor.Decrypt(value) }

func parseStoredRiskControl(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("invalid risk control setting")
	}
}

func (m *ConfigManager) observeExpectedState(raw string, riskControlEnabled bool) {
	if m == nil {
		return
	}
	m.installMu.Lock()
	m.observeExpectedStateLocked(raw, riskControlEnabled)
	m.installMu.Unlock()
}

func (m *ConfigManager) observeExpectedStateLocked(raw string, riskControlEnabled bool) {
	if m == nil {
		return
	}
	m.riskControlKnown.Store(true)
	m.expectedRiskControl.Store(riskControlEnabled)
	if strings.TrimSpace(raw) == "" {
		m.expected.Store(1)
		m.expectedBlocking.Store(false)
		return
	}
	var intent struct {
		Enabled         bool  `json:"enabled"`
		BlockingEnabled bool  `json:"blocking_enabled"`
		ConfigVersion   int64 `json:"config_version"`
	}
	// Best-effort field extraction deliberately remains separate from strict
	// config activation. Even an unknown/newer schema can reveal its version, but
	// it must not become a trusted active snapshot.
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return
	}
	if intent.ConfigVersion < 1 {
		intent.ConfigVersion = 1
	}
	m.expected.Store(intent.ConfigVersion)
	m.expectedBlocking.Store(riskControlEnabled && intent.Enabled && intent.BlockingEnabled)
}

func (m *ConfigManager) refreshLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Reload(ctx); err != nil {
				LogWarn(EventConfigReloadDegraded, map[string]any{"status": "degraded", "error_code": "config_ttl_reload_failed"})
			}
		}
	}
}

func (m *ConfigManager) subscribeLoop(ctx context.Context) {
	defer m.wg.Done()
	pubsub := m.redis.Subscribe(ctx, ConfigInvalidationChannel)
	defer func() { _ = pubsub.Close() }()
	channel := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-channel:
			if !ok {
				return
			}
			version, err := strconv.ParseInt(strings.TrimSpace(message.Payload), 10, 64)
			if err != nil || version < 1 {
				continue
			}
			m.expected.Store(version)
			if err := m.Reload(ctx); err != nil {
				// A newer published version failed to activate. Until reload
				// succeeds, do not keep serving a potentially stale weaker mode.
				m.installMu.Lock()
				if active, ok := m.Active(); !ok || active.ConfigVersion < version {
					m.markConfigUntrustedLocked()
				}
				m.installMu.Unlock()
				LogWarn(EventConfigReloadDegraded, map[string]any{
					"config_version": version, "status": "degraded", "error_code": "config_invalidation_reload_failed",
				})
			}
		}
	}
}

func (m *ConfigManager) recordLoadError(_ error) {
	if m == nil {
		return
	}
	now := m.clock.Now()
	m.stateMu.Lock()
	m.lastLoadError = stableErrorMessage("config_load_failed")
	m.lastErrorAt = &now
	m.stateMu.Unlock()
}

func (m *ConfigManager) clearLoadError() {
	m.stateMu.Lock()
	m.lastLoadError = ""
	m.lastErrorAt = nil
	m.stateMu.Unlock()
}

func cloneStorageConfig(cfg storageConfig) storageConfig {
	cfg.Scanners = append([]string(nil), cfg.Scanners...)
	cfg.GroupIDs = append([]int64(nil), cfg.GroupIDs...)
	cfg.Endpoints = append([]StorageEndpoint(nil), cfg.Endpoints...)
	return cfg
}

func cloneActiveConfig(cfg ActiveConfig) ActiveConfig {
	cfg.Scanners = append([]string(nil), cfg.Scanners...)
	cfg.GroupIDs = append([]int64(nil), cfg.GroupIDs...)
	cfg.Endpoints = append([]ActiveEndpoint(nil), cfg.Endpoints...)
	return cfg
}
