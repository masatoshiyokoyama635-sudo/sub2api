package securityaudit

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type prefixEncryptor struct{}

func (prefixEncryptor) Encrypt(value string) (string, error) { return "enc:" + value, nil }
func (prefixEncryptor) Decrypt(value string) (string, error) { return value[4:], nil }

type failingDecryptor struct{}

func (failingDecryptor) Encrypt(value string) (string, error) { return "enc:" + value, nil }
func (failingDecryptor) Decrypt(string) (string, error)       { return "", errors.New("decrypt failed") }

type sequencedSettingRead struct {
	values  map[string]string
	err     error
	started chan struct{}
	release chan struct{}
}

type sequencedSettingRepository struct {
	staticSettingRepository
	mu    sync.Mutex
	reads []sequencedSettingRead
}

func (r *sequencedSettingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	if len(r.reads) == 0 {
		r.mu.Unlock()
		return nil, errors.New("unexpected settings read")
	}
	read := r.reads[0]
	r.reads = r.reads[1:]
	r.mu.Unlock()
	close(read.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-read.release:
	}
	if read.err != nil {
		return nil, read.err
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = read.values[key]
	}
	return result, nil
}

func promptAuditStorageJSON(t *testing.T, version int64, enabled, blocking, endpointEnabled bool, token string) string {
	t.Helper()
	storage := DefaultStorageConfig()
	storage.ConfigVersion = version
	storage.Enabled = enabled
	storage.BlockingEnabled = blocking
	storage.Endpoints = []StorageEndpoint{{
		ID: "guard", Name: "Guard", Protocol: "openai_compatible", BaseURL: "https://guard.example.test",
		Model: DefaultGuardModel, TokenCiphertext: token, TimeoutMS: 1000, InputLimit: 1000, Enabled: endpointEnabled,
	}}
	raw, err := json.Marshal(storage)
	require.NoError(t, err)
	return string(raw)
}

func TestDefaultConfigIsOff(t *testing.T) {
	storage, err := ParseStorageConfig("")
	require.NoError(t, err)
	require.False(t, storage.Enabled)
	active, err := ActiveFromStorage(storage, true, prefixEncryptor{})
	require.NoError(t, err)
	require.Equal(t, ModeOff, active.EffectiveMode())
	require.Equal(t, AllScannerIDs, storage.Scanners)
	publicJSON, err := json.Marshal(PublicFromStorage(storage, true))
	require.NoError(t, err)
	require.Contains(t, string(publicJSON), `"group_ids":[]`)
	require.Contains(t, string(publicJSON), `"endpoints":[]`)
}

func TestConfigRejectsBlockingWithoutAudit(t *testing.T) {
	storage := DefaultStorageConfig()
	storage.BlockingEnabled = true
	require.Error(t, validateStorageConfig(storage))
}

func TestPublicConfigNeverMarshalsToken(t *testing.T) {
	storage := DefaultStorageConfig()
	storage.Endpoints = []StorageEndpoint{{ID: "one", Name: "One", Protocol: "openai_compatible", BaseURL: "http://127.0.0.1:8080", Model: DefaultGuardModel, TokenCiphertext: "GUARD_TOKEN_CANARY_SECRET", TimeoutMS: 1000, InputLimit: 1000, Enabled: true}}
	public := PublicFromStorage(storage, true)
	raw, err := json.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "GUARD_TOKEN_CANARY_SECRET")
	require.NotContains(t, string(raw), "ciphertext")
	require.True(t, public.Endpoints[0].HasToken)
}

func TestConfigRuntimeLoadErrorIsStableBoundedAndSecretFree(t *testing.T) {
	const canary = "CONFIG_LOAD_CANARY_SECRET"
	manager := &ConfigManager{clock: fixedClock{}}
	manager.recordLoadError(errors.New("decrypt failed for token " + canary + " Authorization: Bearer " + canary))
	_, _, _, message := manager.RuntimeState()
	require.Equal(t, stableErrorMessage("config_load_failed"), message)
	require.NotContains(t, message, canary)
	require.LessOrEqual(t, len([]rune(message)), 160)
}

func TestConfigManagerPublicRequiresSuccessfullyLoadedSnapshot(t *testing.T) {
	t.Run("absent persisted setting is legitimate default", func(t *testing.T) {
		manager := NewConfigManager(nil, staticSettingRepository{values: map[string]string{
			SettingKeyPromptAuditConfig: "",
			SettingKeyRiskControl:       "false",
		}}, nil, prefixEncryptor{})
		require.NoError(t, manager.Reload(context.Background()))

		public, err := manager.Public()
		require.NoError(t, err)
		require.Equal(t, int64(1), public.ConfigVersion)
		require.False(t, public.Enabled)
	})

	t.Run("persisted config activation failure is unavailable", func(t *testing.T) {
		const canary = "persisted-token-canary"
		manager := NewConfigManager(nil, staticSettingRepository{values: map[string]string{
			SettingKeyPromptAuditConfig: `{"enabled":true,"config_version":9,"endpoints":[{"token_ciphertext":"` + canary + `"}]}`,
			SettingKeyRiskControl:       "true",
		}}, nil, prefixEncryptor{})
		require.Error(t, manager.Reload(context.Background()))

		public, err := manager.Public()
		require.Error(t, err)
		require.Empty(t, public)
		require.Equal(t, ErrorCodeConfigUnavailable, infraerrors.Reason(err))
		require.NotContains(t, err.Error(), canary)
	})

	t.Run("reload failure preserves last successfully loaded snapshot", func(t *testing.T) {
		storage := DefaultStorageConfig()
		storage.ConfigVersion = 4
		storage.ChangeSummary = "trusted snapshot"
		raw, err := json.Marshal(storage)
		require.NoError(t, err)
		repository := &switchableSettingRepository{staticSettingRepository: staticSettingRepository{values: map[string]string{
			SettingKeyPromptAuditConfig: string(raw),
			SettingKeyRiskControl:       "false",
		}}}
		manager := NewConfigManager(nil, repository, nil, prefixEncryptor{})
		require.NoError(t, manager.Reload(context.Background()))
		repository.loadErr = errors.New("settings unavailable")
		require.Error(t, manager.Reload(context.Background()))

		public, err := manager.Public()
		require.NoError(t, err)
		require.Equal(t, int64(4), public.ConfigVersion)
		require.Equal(t, "trusted snapshot", public.ChangeSummary)
	})
}

func TestBuildNextStoragePreserveReplaceAndClearToken(t *testing.T) {
	manager := &ConfigManager{encryptor: prefixEncryptor{}}
	current := DefaultStorageConfig()
	current.Endpoints = []StorageEndpoint{{ID: "one", Name: "One", Protocol: "openai_compatible", BaseURL: "http://127.0.0.1:8080/v1/", Model: DefaultGuardModel, TokenCiphertext: "enc:old", TimeoutMS: 1000, InputLimit: 1000}}
	base := UpdateConfigRequest{ExpectedConfigVersion: 1, Strategy: "priority", WorkerCount: 1, QueueCapacity: 10, Scanners: []string{"PII"}, AllGroups: true,
		Endpoints: []UpdateEndpoint{{ID: "one", Name: "One", Protocol: "openai_compatible", BaseURL: " http://127.0.0.1:8080/ ", TimeoutMS: 1000, InputLimit: 1000}}}

	t.Run("semantically equivalent normalized URL preserves token", func(t *testing.T) {
		preserved, err := manager.buildNextStorage(current, base, 9)
		require.NoError(t, err)
		require.Equal(t, "http://127.0.0.1:8080", preserved.Endpoints[0].BaseURL)
		require.Equal(t, "enc:old", preserved.Endpoints[0].TokenCiphertext)
	})

	t.Run("changed URL without an old token succeeds empty", func(t *testing.T) {
		currentWithoutToken := current
		currentWithoutToken.Endpoints = append([]StorageEndpoint(nil), current.Endpoints...)
		currentWithoutToken.Endpoints[0].TokenCiphertext = ""
		changedReq := base
		changedReq.Endpoints = append([]UpdateEndpoint(nil), base.Endpoints...)
		changedReq.Endpoints[0].BaseURL = "https://guard.example.test/v1"

		changed, err := manager.buildNextStorage(currentWithoutToken, changedReq, 9)
		require.NoError(t, err)
		require.Equal(t, "https://guard.example.test", changed.Endpoints[0].BaseURL)
		require.Empty(t, changed.Endpoints[0].TokenCiphertext)
	})

	t.Run("changed URL without token disposition fails closed", func(t *testing.T) {
		changedReq := base
		changedReq.Endpoints = append([]UpdateEndpoint(nil), base.Endpoints...)
		changedReq.Endpoints[0].BaseURL = "https://guard.example.test/v1"

		_, err := manager.buildNextStorage(current, changedReq, 9)
		require.Error(t, err)
		require.True(t, infraerrors.IsBadRequest(err))
		require.Equal(t, "prompt_audit_token_required_for_base_url_change", infraerrors.Reason(err))
		require.Equal(t, "更改审计节点地址时必须提供新令牌或明确清除旧令牌", infraerrors.Message(err))
	})

	t.Run("changed URL with replacement token succeeds", func(t *testing.T) {
		replacedReq := base
		replacedReq.Endpoints = append([]UpdateEndpoint(nil), base.Endpoints...)
		replacedReq.Endpoints[0].BaseURL = "https://guard.example.test/v1/"
		replacedReq.Endpoints[0].Token = "new"

		replaced, err := manager.buildNextStorage(current, replacedReq, 9)
		require.NoError(t, err)
		require.Equal(t, "https://guard.example.test", replaced.Endpoints[0].BaseURL)
		require.Equal(t, "enc:new", replaced.Endpoints[0].TokenCiphertext)
	})

	t.Run("changed URL with clear token succeeds empty", func(t *testing.T) {
		clearedReq := base
		clearedReq.Endpoints = append([]UpdateEndpoint(nil), base.Endpoints...)
		clearedReq.Endpoints[0].BaseURL = "https://guard.example.test/v1/"
		clearedReq.Endpoints[0].ClearToken = true

		cleared, err := manager.buildNextStorage(current, clearedReq, 9)
		require.NoError(t, err)
		require.Equal(t, "https://guard.example.test", cleared.Endpoints[0].BaseURL)
		require.Empty(t, cleared.Endpoints[0].TokenCiphertext)
	})
}

func TestEffectiveModeTruthTable(t *testing.T) {
	tests := []struct {
		risk, enabled, blocking bool
		want                    Mode
	}{
		{false, false, false, ModeOff}, {false, true, true, ModeOff}, {true, false, false, ModeOff},
		{true, true, false, ModeAsync}, {true, true, true, ModeBlocking},
	}
	for _, tt := range tests {
		cfg := ActiveConfig{RiskControlEnabled: tt.risk, Enabled: tt.enabled, BlockingEnabled: tt.blocking}
		require.Equal(t, tt.want, cfg.EffectiveMode())
	}
}

func TestConfigManagerColdStartOnlyFailsClosedForExplicitBlockingIntent(t *testing.T) {
	manager := &ConfigManager{}

	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":false,"config_version":42}`, true)
	require.Equal(t, int64(42), manager.expected.Load())
	require.Equal(t, ModeOff, manager.EffectiveMode(), "an async config version must not imply blocking")
	require.False(t, manager.BlockingActivationDegraded())

	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":true,"config_version":43}`, false)
	require.Equal(t, ModeOff, manager.EffectiveMode(), "the global risk-control switch still gates blocking")

	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":true,"config_version":44}`, true)
	require.Equal(t, ModeBlocking, manager.EffectiveMode())
	require.True(t, manager.BlockingActivationDegraded())

	manager.observeExpectedState(`{"enabled":true`, true)
	require.Equal(t, ModeBlocking, manager.EffectiveMode(), "undecodable storage must not erase the last known strict intent")
}

func TestConfigManagerStaleWeakerSnapshotFailsClosedWhenBlockingExpected(t *testing.T) {
	manager := &ConfigManager{}
	async := ActiveConfig{RiskControlEnabled: true, Enabled: true, BlockingEnabled: false, ConfigVersion: 1}
	manager.snapshot.Store(&activeConfigSnapshot{active: async, storage: DefaultStorageConfig(), loadedAt: fixedClock{}.Now()})
	manager.expected.Store(2)
	manager.expectedBlocking.Store(true)

	require.True(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeBlocking, manager.EffectiveMode())

	service := &PromptService{config: manager, evaluator: NewGuardEvaluator(nil, nil, nil)}
	decision, err := service.Evaluate(context.Background(), Request{Protocol: "openai_chat_completions", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)})
	require.Error(t, err)
	require.Nil(t, decision)
	var guardErr *GuardError
	require.ErrorAs(t, err, &guardErr)
	require.Equal(t, ErrorCodeUnavailable, guardErr.Code)
}

type errorSettingRepository struct{ staticSettingRepository }

func (errorSettingRepository) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("settings unavailable")
}

type lifecycleBlockingSettingRepository struct {
	staticSettingRepository
	started chan struct{}
	release <-chan struct{}
}

func (r lifecycleBlockingSettingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	close(r.started)
	<-r.release
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.staticSettingRepository.GetMultiple(ctx, keys)
}

type switchableSettingRepository struct {
	staticSettingRepository
	loadErr error
}

func (r *switchableSettingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return r.staticSettingRepository.GetMultiple(ctx, keys)
}

func TestConfigManagerStoppedShutdownReturnsSavedError(t *testing.T) {
	shutdownErr := errors.New("config shutdown failed")
	manager := &ConfigManager{state: promptLifecycleStopped, shutdownErr: shutdownErr}

	require.ErrorIs(t, manager.Shutdown(context.Background()), shutdownErr)
}

func TestConfigManagerStartAndShutdownAreSerialized(t *testing.T) {
	loadStarted := make(chan struct{})
	loadRelease := make(chan struct{})
	repo := lifecycleBlockingSettingRepository{
		staticSettingRepository: staticSettingRepository{values: map[string]string{
			SettingKeyPromptAuditConfig: "",
			SettingKeyRiskControl:       "false",
		}},
		started: loadStarted,
		release: loadRelease,
	}
	manager := NewConfigManager(nil, repo, nil, prefixEncryptor{})
	startDone := make(chan error, 1)
	go func() { startDone <- manager.Start(context.Background()) }()
	<-loadStarted

	shutdownDone := make(chan error, 1)
	shutdownAttempted := make(chan struct{})
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	go func() {
		close(shutdownAttempted)
		shutdownDone <- manager.Shutdown(shutdownCtx)
	}()
	<-shutdownAttempted

	shutdownReturnedBeforeStart := false
	select {
	case <-shutdownDone:
		shutdownReturnedBeforeStart = true
	case <-time.After(50 * time.Millisecond):
	}
	close(loadRelease)
	startErr := <-startDone
	if !shutdownReturnedBeforeStart {
		require.NoError(t, <-shutdownDone)
	}

	require.ErrorIs(t, startErr, context.Canceled)
	require.False(t, shutdownReturnedBeforeStart, "Shutdown must not complete while Start is still installing reload loops")
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	require.NoError(t, manager.Shutdown(secondCtx))
}

func TestConfigManagerShutdownTimeoutIsTerminalAndSecondShutdownCanFinish(t *testing.T) {
	manager := NewConfigManager(nil, staticSettingRepository{values: map[string]string{
		SettingKeyPromptAuditConfig: "",
		SettingKeyRiskControl:       "false",
	}}, nil, prefixEncryptor{})
	require.NoError(t, manager.Start(context.Background()))

	release := make(chan struct{})
	manager.wg.Add(1)
	go func() {
		defer manager.wg.Done()
		<-release
	}()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShutdown()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- manager.Shutdown(shutdownCtx) }()

	var shutdownErr error
	returnedAtDeadline := false
	select {
	case shutdownErr = <-shutdownDone:
		returnedAtDeadline = true
	case <-time.After(100 * time.Millisecond):
	}
	startCtx, cancelStart := context.WithCancel(context.Background())
	cancelStart()
	startErr := manager.Start(startCtx)
	manager.lifecycleMu.Lock()
	restartInstalled := manager.state == promptLifecycleStarting || manager.state == promptLifecycleRunning
	manager.lifecycleMu.Unlock()
	close(release)
	if !returnedAtDeadline {
		shutdownErr = <-shutdownDone
	}
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	secondErr := manager.Shutdown(secondCtx)

	require.True(t, returnedAtDeadline, "Shutdown must honor its timeout instead of waiting indefinitely")
	require.ErrorIs(t, shutdownErr, context.DeadlineExceeded)
	require.False(t, restartInstalled, "Start must not install a new run once shutdown has begun")
	require.Error(t, startErr, "Start must remain disabled once shutdown has begun")
	require.NoError(t, secondErr, "a later Shutdown must finish draining the original run")
}

func TestConfigManagerStartupLoadFailureDoesNotBlockWhenBlockingNotIntended(t *testing.T) {
	// Settings unavailable and no prior blocking intent: stay ModeOff so the
	// gateway remains usable and admins can still disable/configure Prompt Audit.
	manager := NewConfigManager(nil, errorSettingRepository{}, nil, prefixEncryptor{})
	err := manager.Start(context.Background())
	require.Error(t, err)
	require.True(t, manager.configUntrusted.Load())
	require.False(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeOff, manager.EffectiveMode())

	service := &PromptService{config: manager, evaluator: NewGuardEvaluator(nil, nil, nil)}
	decision, evalErr := service.Evaluate(context.Background(), Request{
		Protocol: "openai_chat_completions",
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.NoError(t, evalErr)
	require.NotNil(t, decision)
	require.Equal(t, DecisionAllow, decision.Kind)
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestConfigManagerStartupLoadFailureFailsClosedWhenBlockingIntended(t *testing.T) {
	manager := NewConfigManager(nil, errorSettingRepository{}, nil, prefixEncryptor{})
	// Simulate intent observed before a later load failure (e.g. decrypt error).
	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":true,"config_version":3}`, true)
	manager.markConfigUntrusted()
	require.True(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeBlocking, manager.EffectiveMode())

	service := &PromptService{config: manager, evaluator: NewGuardEvaluator(nil, nil, nil)}
	decision, err := service.Evaluate(context.Background(), Request{
		Protocol: "openai_chat_completions",
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.Error(t, err)
	require.Nil(t, decision)
	var guardErr *GuardError
	require.ErrorAs(t, err, &guardErr)
	require.Equal(t, ErrorCodeUnavailable, guardErr.Code)
}

func TestConfigManagerUntrustedClearsOnSuccessfulDisable(t *testing.T) {
	// After a degraded fail-closed period, saving disabled config must restore ModeOff.
	manager := &ConfigManager{encryptor: prefixEncryptor{}, clock: fixedClock{}}
	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":true,"config_version":5}`, true)
	manager.markConfigUntrusted()
	require.Equal(t, ModeBlocking, manager.EffectiveMode())

	// Install a trusted disabled snapshot the same way Save does after commit.
	disabled := DefaultStorageConfig()
	disabled.ConfigVersion = 6
	disabled.Enabled = false
	disabled.BlockingEnabled = false
	active, err := ActiveFromStorage(disabled, true, manager.encryptor)
	require.NoError(t, err)
	manager.installMu.Lock()
	manager.installActiveSnapshotLocked(disabled, active, true)
	manager.installMu.Unlock()

	require.False(t, manager.configUntrusted.Load())
	require.False(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeOff, manager.EffectiveMode())

	service := &PromptService{config: manager, evaluator: NewGuardEvaluator(nil, nil, nil)}
	decision, evalErr := service.Evaluate(context.Background(), Request{
		Protocol: "openai_chat_completions",
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.NoError(t, evalErr)
	require.Equal(t, DecisionAllow, decision.Kind)
}

func TestConfigManagerUntrustedWithoutBlockingDoesNotForceBlockingMode(t *testing.T) {
	manager := &ConfigManager{}
	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":false,"config_version":2}`, true)
	manager.markConfigUntrusted()
	require.False(t, manager.expectedBlocking.Load())
	require.False(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeOff, manager.EffectiveMode(), "async intent + untrusted must not force blocking unavailable")
}

func TestConfigManagerInvalidStoredRiskControlOnlyBlocksForPreviouslyObservedBlockingIntent(t *testing.T) {
	tests := []struct {
		name         string
		observeBlock bool
		wantMode     Mode
		wantDegraded bool
	}{
		{name: "no trusted blocking intent", wantMode: ModeOff},
		{name: "previous blocking intent", observeBlock: true, wantMode: ModeBlocking, wantDegraded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewConfigManager(nil, staticSettingRepository{values: map[string]string{
				SettingKeyPromptAuditConfig: promptAuditStorageJSON(t, 1, false, false, false, ""),
				SettingKeyRiskControl:       "TRUE",
			}}, nil, prefixEncryptor{})
			if tt.observeBlock {
				manager.observeExpectedState(`{"enabled":true,"blocking_enabled":true,"config_version":2}`, true)
			}

			err := manager.Start(context.Background())
			require.Error(t, err)
			require.Equal(t, tt.wantDegraded, manager.BlockingActivationDegraded())
			require.Equal(t, tt.wantMode, manager.EffectiveMode())
			require.NoError(t, manager.Shutdown(context.Background()))
		})
	}
}

func TestConfigManagerStartupDecryptFailureOnlyFailsClosedForBlockingIntent(t *testing.T) {
	tests := []struct {
		name            string
		riskControl     bool
		enabled         bool
		blocking        bool
		endpointEnabled bool
		wantMode        Mode
		wantDegraded    bool
	}{
		{name: "blocking", riskControl: true, enabled: true, blocking: true, endpointEnabled: true, wantMode: ModeBlocking, wantDegraded: true},
		{name: "async", riskControl: true, enabled: true, blocking: false, endpointEnabled: true, wantMode: ModeOff},
		{name: "risk control off", riskControl: false, enabled: true, blocking: true, endpointEnabled: true, wantMode: ModeOff},
		{name: "audit disabled", riskControl: true, enabled: false, blocking: false, endpointEnabled: false, wantMode: ModeOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := staticSettingRepository{values: map[string]string{
				SettingKeyPromptAuditConfig: promptAuditStorageJSON(t, 9, tt.enabled, tt.blocking, tt.endpointEnabled, "corrupt"),
				SettingKeyRiskControl:       strconv.FormatBool(tt.riskControl),
			}}
			manager := NewConfigManager(nil, repo, nil, failingDecryptor{})
			err := manager.Start(context.Background())
			require.Error(t, err)
			require.Equal(t, tt.wantDegraded, manager.BlockingActivationDegraded())
			require.Equal(t, tt.wantMode, manager.EffectiveMode())
			_, _, _, loadError := manager.RuntimeState()
			require.NotEmpty(t, loadError)
			require.NoError(t, manager.Shutdown(context.Background()))
		})
	}
}

func TestConfigManagerConcurrentReloadCannotInstallOlderReadAfterNewerRead(t *testing.T) {
	oldStarted, oldRelease := make(chan struct{}), make(chan struct{})
	newStarted, newRelease := make(chan struct{}), make(chan struct{})
	repo := &sequencedSettingRepository{reads: []sequencedSettingRead{
		{values: map[string]string{
			SettingKeyPromptAuditConfig: promptAuditStorageJSON(t, 7, true, false, true, "enc:old"),
			SettingKeyRiskControl:       "true",
		}, started: oldStarted, release: oldRelease},
		{values: map[string]string{
			SettingKeyPromptAuditConfig: promptAuditStorageJSON(t, 8, true, false, true, "enc:new"),
			SettingKeyRiskControl:       "true",
		}, started: newStarted, release: newRelease},
	}}
	manager := NewConfigManager(nil, repo, nil, prefixEncryptor{})
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- manager.Reload(context.Background()) }()
	<-oldStarted
	go func() { errorsCh <- manager.Reload(context.Background()) }()
	<-newStarted
	close(newRelease)
	require.NoError(t, <-errorsCh)
	close(oldRelease)
	require.NoError(t, <-errorsCh)

	active, ok := manager.Active()
	require.True(t, ok)
	require.Equal(t, int64(8), active.ConfigVersion)
	require.Equal(t, "new", active.Endpoints[0].Token)
	expected, activeVersion, _, loadError := manager.RuntimeState()
	require.Equal(t, int64(8), expected)
	require.Equal(t, int64(8), activeVersion)
	require.Empty(t, loadError)
}

func TestConfigManagerSaveFenceRejectsOlderReloadSuccess(t *testing.T) {
	oldStarted, oldRelease := make(chan struct{}), make(chan struct{})
	saveStarted, saveRelease := make(chan struct{}), make(chan struct{})
	close(saveRelease)
	repo := &sequencedSettingRepository{reads: []sequencedSettingRead{
		{values: map[string]string{
			SettingKeyPromptAuditConfig: promptAuditStorageJSON(t, 1, true, false, true, "enc:old"),
			SettingKeyRiskControl:       "true",
		}, started: oldStarted, release: oldRelease},
		{values: map[string]string{SettingKeyRiskControl: "true"}, started: saveStarted, release: saveRelease},
	}}
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	manager := NewConfigManager(db, repo, nil, prefixEncryptor{})
	manager.clock = fixedClock{}
	old := promptAuditStorageJSON(t, 1, true, false, true, "enc:old")
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(promptAuditConfigLockKey).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1 FOR UPDATE`).WithArgs(SettingKeyPromptAuditConfig).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(old))
	mock.ExpectExec(`INSERT INTO settings`).WithArgs(SettingKeyPromptAuditConfig, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	manager.snapshot.Store(&activeConfigSnapshot{
		storage: DefaultStorageConfig(),
		active:  ActiveConfig{RiskControlEnabled: true, Enabled: true, AllGroups: true, ConfigVersion: 1}, loadedAt: fixedClock{}.Now(),
	})
	manager.expected.Store(1)
	manager.expectedRiskControl.Store(true)
	manager.riskControlKnown.Store(true)

	reloadErr := make(chan error, 1)
	go func() { reloadErr <- manager.Reload(context.Background()) }()
	<-oldStarted
	request := UpdateConfigRequest{
		ExpectedConfigVersion: 1, Enabled: true, BlockingEnabled: true, Strategy: "priority", WorkerCount: 1,
		QueueCapacity: 10, Scanners: []string{"pii"}, AllGroups: true,
		Endpoints: []UpdateEndpoint{{ID: "guard", Name: "Guard", Protocol: "openai_compatible", BaseURL: "https://guard.example.test", TimeoutMS: 1000, InputLimit: 1000, Enabled: true, Token: "replacement"}},
	}
	_, err = manager.Save(context.Background(), request, 7)
	require.NoError(t, err)
	<-saveStarted
	close(oldRelease)
	require.NoError(t, <-reloadErr)

	active, ok := manager.Active()
	require.True(t, ok)
	require.Equal(t, int64(2), active.ConfigVersion)
	require.Equal(t, "replacement", active.Endpoints[0].Token)
	expected, activeVersion, _, loadError := manager.RuntimeState()
	require.Equal(t, int64(2), expected)
	require.Equal(t, int64(2), activeVersion)
	require.Empty(t, loadError)
	require.False(t, manager.configUntrusted.Load())
	require.True(t, manager.expectedBlocking.Load())
	require.Equal(t, ModeBlocking, manager.EffectiveMode())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConfigManagerSaveFenceRejectsOlderReloadFailureState(t *testing.T) {
	oldStarted, oldRelease := make(chan struct{}), make(chan struct{})
	saveStarted, saveRelease := make(chan struct{}), make(chan struct{})
	close(saveRelease)
	repo := &sequencedSettingRepository{reads: []sequencedSettingRead{
		{values: map[string]string{
			SettingKeyPromptAuditConfig: `{"enabled":true,"blocking_enabled":true,"config_version":1,"unknown":true}`,
			SettingKeyRiskControl:       "true",
		}, started: oldStarted, release: oldRelease},
		{values: map[string]string{SettingKeyRiskControl: "true"}, started: saveStarted, release: saveRelease},
	}}
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	manager := NewConfigManager(db, repo, nil, prefixEncryptor{})
	manager.clock = fixedClock{}
	old := promptAuditStorageJSON(t, 1, true, false, true, "enc:old")
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(promptAuditConfigLockKey).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1 FOR UPDATE`).WithArgs(SettingKeyPromptAuditConfig).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(old))
	mock.ExpectExec(`INSERT INTO settings`).WithArgs(SettingKeyPromptAuditConfig, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	manager.snapshot.Store(&activeConfigSnapshot{
		storage: DefaultStorageConfig(),
		active:  ActiveConfig{RiskControlEnabled: true, Enabled: true, AllGroups: true, ConfigVersion: 1}, loadedAt: fixedClock{}.Now(),
	})
	manager.expected.Store(1)
	manager.expectedRiskControl.Store(true)
	manager.riskControlKnown.Store(true)

	reloadErr := make(chan error, 1)
	go func() { reloadErr <- manager.Reload(context.Background()) }()
	<-oldStarted
	request := UpdateConfigRequest{
		ExpectedConfigVersion: 1, Enabled: false, BlockingEnabled: false, Strategy: "priority", WorkerCount: 1,
		QueueCapacity: 10, Scanners: []string{"pii"}, AllGroups: true,
	}
	_, err = manager.Save(context.Background(), request, 7)
	require.NoError(t, err)
	<-saveStarted
	close(oldRelease)
	require.Error(t, <-reloadErr)

	expected, activeVersion, _, loadError := manager.RuntimeState()
	require.Equal(t, int64(2), expected)
	require.Equal(t, int64(2), activeVersion)
	require.Empty(t, loadError)
	require.True(t, manager.riskControlKnown.Load())
	require.False(t, manager.expectedBlocking.Load())
	require.False(t, manager.configUntrusted.Load())
	require.Equal(t, ModeOff, manager.EffectiveMode())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConfigManagerLatestFailedReloadPreservesPreviouslyObservedBlockingIntent(t *testing.T) {
	oldStarted, oldRelease := make(chan struct{}), make(chan struct{})
	newStarted, newRelease := make(chan struct{}), make(chan struct{})
	repo := &sequencedSettingRepository{reads: []sequencedSettingRead{
		{values: map[string]string{
			SettingKeyPromptAuditConfig: promptAuditStorageJSON(t, 7, true, false, true, "enc:old"),
			SettingKeyRiskControl:       "false",
		}, started: oldStarted, release: oldRelease},
		{err: errors.New("database unavailable"), started: newStarted, release: newRelease},
	}}
	manager := NewConfigManager(nil, repo, nil, prefixEncryptor{})
	manager.snapshot.Store(&activeConfigSnapshot{
		active:   ActiveConfig{RiskControlEnabled: true, Enabled: true, BlockingEnabled: false, ConfigVersion: 6},
		storage:  DefaultStorageConfig(),
		loadedAt: fixedClock{}.Now(),
	})
	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":true,"config_version":8}`, true)
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- manager.Reload(context.Background()) }()
	<-oldStarted
	go func() { errorsCh <- manager.Reload(context.Background()) }()
	<-newStarted
	close(newRelease)
	require.Error(t, <-errorsCh)
	close(oldRelease)
	require.NoError(t, <-errorsCh)

	require.True(t, manager.configUntrusted.Load())
	require.False(t, manager.riskControlKnown.Load())
	require.True(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeBlocking, manager.EffectiveMode())
}

func TestParseLegacyConfigDefaultsMissingFieldsWithoutEnablingBlocking(t *testing.T) {
	storage, err := ParseStorageConfig(`{"enabled":false,"config_version":9}`)
	require.NoError(t, err)
	require.False(t, storage.BlockingEnabled)
	require.Equal(t, "priority", storage.Strategy)
	require.Equal(t, DefaultWorkerCount, storage.WorkerCount)
	require.Equal(t, DefaultQueueCapacity, storage.QueueCapacity)
	require.Equal(t, AllScannerIDs, storage.Scanners)
	require.True(t, storage.AllGroups)
}

func TestParseStorageConfigRejectsUnknownFieldsTrailingJSONAndUnknownScanners(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown top-level field", raw: `{"enabled":false,"config_version":9,"blockng_enabled":true}`},
		{name: "unknown endpoint field", raw: `{"enabled":false,"config_version":9,"endpoints":[{"id":"guard","name":"Guard","base_url":"https://guard.example.test","timeout_ms":1000,"input_limit":1000,"enabeld":true}]}`},
		{name: "trailing object", raw: `{"enabled":false,"config_version":9} {"enabled":true}`},
		{name: "trailing scalar", raw: `{"enabled":false,"config_version":9} true`},
		{name: "top-level null", raw: `null`},
		{name: "unknown scanner", raw: `{"enabled":false,"config_version":9,"scanners":["pii","made_up"]}`},
		{name: "explicit empty scanners", raw: `{"enabled":false,"config_version":9,"scanners":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseStorageConfig(tt.raw)
			require.Error(t, err)
		})
	}
}

type postCommitRiskControlFailureRepository struct{ staticSettingRepository }

func (postCommitRiskControlFailureRepository) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("risk control unavailable after commit")
}

func TestConfigManagerSaveRiskControlFailureAdvancesBlockingFence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	manager := NewConfigManager(db, postCommitRiskControlFailureRepository{}, nil, prefixEncryptor{})
	manager.clock = fixedClock{}
	old := promptAuditStorageJSON(t, 1, true, false, true, "enc:old")
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(promptAuditConfigLockKey).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1 FOR UPDATE`).WithArgs(SettingKeyPromptAuditConfig).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(old))
	mock.ExpectExec(`INSERT INTO settings`).WithArgs(SettingKeyPromptAuditConfig, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	manager.snapshot.Store(&activeConfigSnapshot{
		storage: DefaultStorageConfig(),
		active:  ActiveConfig{RiskControlEnabled: true, Enabled: true, AllGroups: true, ConfigVersion: 1}, loadedAt: fixedClock{}.Now(),
	})
	manager.expected.Store(1)
	manager.expectedRiskControl.Store(true)

	request := UpdateConfigRequest{
		ExpectedConfigVersion: 1, Enabled: true, BlockingEnabled: true, Strategy: "priority", WorkerCount: 1,
		QueueCapacity: 10, Scanners: []string{"pii"}, AllGroups: true,
		Endpoints: []UpdateEndpoint{{ID: "guard", Name: "Guard", Protocol: "openai_compatible", BaseURL: "https://guard.example.test", TimeoutMS: 1000, InputLimit: 1000, Enabled: true, Token: "replacement"}},
	}
	_, err = manager.Save(context.Background(), request, 7)

	require.Error(t, err)
	expected, active, _, loadError := manager.RuntimeState()
	require.Equal(t, int64(2), expected)
	require.Equal(t, int64(1), active)
	require.NotEmpty(t, loadError)
	require.True(t, manager.configUntrusted.Load())
	require.True(t, manager.expectedBlocking.Load())
	require.True(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeBlocking, manager.EffectiveMode())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConfigManagerSaveActivationFailureMarksRuntimeUntrusted(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	settings := staticSettingRepository{values: map[string]string{SettingKeyRiskControl: "true"}}
	manager := NewConfigManager(db, settings, nil, failingDecryptor{})
	manager.clock = fixedClock{}
	old := promptAuditStorageJSON(t, 1, true, false, true, "enc:old")
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(promptAuditConfigLockKey).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1 FOR UPDATE`).WithArgs(SettingKeyPromptAuditConfig).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(old))
	mock.ExpectExec(`INSERT INTO settings`).WithArgs(SettingKeyPromptAuditConfig, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	manager.snapshot.Store(&activeConfigSnapshot{
		storage:  DefaultStorageConfig(),
		active:   ActiveConfig{RiskControlEnabled: true, Enabled: true, AllGroups: true, ConfigVersion: 1},
		loadedAt: fixedClock{}.Now(),
	})
	manager.expected.Store(1)
	manager.expectedRiskControl.Store(true)
	request := UpdateConfigRequest{
		ExpectedConfigVersion: 1, Enabled: true, BlockingEnabled: true, StorePassEvents: false,
		Strategy: "priority", WorkerCount: 1, QueueCapacity: 10, Scanners: []string{"pii"}, AllGroups: true,
		Endpoints: []UpdateEndpoint{{ID: "guard", Name: "Guard", Protocol: "openai_compatible", BaseURL: "https://guard.example.test", TimeoutMS: 1000, InputLimit: 1000, Enabled: true, Token: "replacement"}},
	}

	_, err = manager.Save(context.Background(), request, 7)

	require.Error(t, err)
	expected, active, _, loadError := manager.RuntimeState()
	require.Equal(t, int64(2), expected)
	require.Equal(t, int64(1), active)
	require.NotEmpty(t, loadError)
	require.True(t, manager.configUntrusted.Load())
	require.True(t, manager.expectedBlocking.Load())
	require.True(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeBlocking, manager.EffectiveMode())
	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: map[int64]string{51: "do not dispatch after failed activation"}}
	scannerCalls := 0
	runner := NewRunner(manager, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
		scannerCalls++
		return integrationResult(EventPass), nil
	}), NewAtomicMetrics())
	err = runner.processJob(context.Background(), 0, ActiveConfig{RiskControlEnabled: true, Enabled: true, AllGroups: true, ConfigVersion: 1}, workerJob(1, 3))
	require.Error(t, err)
	require.Zero(t, scannerCalls)
	require.Equal(t, "audit_config_changed", repo.failedCode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConfigManagerNewerUndecryptableAsyncReloadKeepsOlderAsyncSnapshot(t *testing.T) {
	values := map[string]string{
		SettingKeyPromptAuditConfig: promptAuditStorageJSON(t, 8, true, false, true, "corrupt"),
		SettingKeyRiskControl:       "true",
	}
	manager := NewConfigManager(nil, staticSettingRepository{values: values}, nil, failingDecryptor{})
	old := ActiveConfig{RiskControlEnabled: true, Enabled: true, BlockingEnabled: false, ConfigVersion: 7}
	manager.snapshot.Store(&activeConfigSnapshot{active: old, storage: DefaultStorageConfig(), loadedAt: fixedClock{}.Now()})
	manager.expected.Store(7)
	manager.expectedRiskControl.Store(true)

	require.Error(t, manager.Reload(context.Background()))
	require.True(t, manager.configUntrusted.Load())
	require.False(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeAsync, manager.EffectiveMode())
}

func TestConfigManagerNewerUninterpretableAsyncReloadKeepsOlderAsyncSnapshot(t *testing.T) {
	manager := &ConfigManager{}
	trusted := ActiveConfig{RiskControlEnabled: true, Enabled: true, BlockingEnabled: false, ConfigVersion: 7}
	manager.snapshot.Store(&activeConfigSnapshot{active: trusted, storage: DefaultStorageConfig(), loadedAt: fixedClock{}.Now()})
	manager.expected.Store(7)
	manager.expectedRiskControl.Store(true)
	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":false,"config_version":8,"future_mode":"blocking_v2"}`, true)
	manager.markConfigUntrusted()

	require.False(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeAsync, manager.EffectiveMode())
}

func TestConfigManagerUnknownConfigOnlyFailsClosedForExplicitBlockingIntent(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantMode     Mode
		wantDegraded bool
	}{
		{name: "cold start unknown async schema", raw: `{"enabled":true,"blocking_enabled":false,"config_version":12,"future_mode":"blocking_v2"}`, wantMode: ModeOff},
		{name: "cold start malformed JSON", raw: `{"config_version":12,"future_mode":"blocking_v2"`, wantMode: ModeOff},
		{name: "cold start unknown blocking schema", raw: `{"enabled":true,"blocking_enabled":true,"config_version":12,"future_mode":"blocking_v2"}`, wantMode: ModeBlocking, wantDegraded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewConfigManager(nil, staticSettingRepository{values: map[string]string{
				SettingKeyPromptAuditConfig: tt.raw,
				SettingKeyRiskControl:       "true",
			}}, nil, prefixEncryptor{})

			require.Error(t, manager.Start(context.Background()))
			require.Equal(t, tt.wantDegraded, manager.BlockingActivationDegraded())
			require.Equal(t, tt.wantMode, manager.EffectiveMode())
			require.NoError(t, manager.Shutdown(context.Background()))
		})
	}

	manager := &ConfigManager{}
	trusted := ActiveConfig{RiskControlEnabled: true, Enabled: true, BlockingEnabled: false, ConfigVersion: 7}
	manager.snapshot.Store(&activeConfigSnapshot{active: trusted, storage: DefaultStorageConfig(), loadedAt: fixedClock{}.Now()})
	manager.expected.Store(7)
	manager.observeExpectedState(`{"enabled":true,"blocking_enabled":false,"config_version":8,"future_mode":"blocking_v2"}`, true)
	manager.markConfigUntrusted()

	require.False(t, manager.BlockingActivationDegraded())
	require.Equal(t, ModeAsync, manager.EffectiveMode())
}

func TestUpdateConfigStrictBoundsAndKnownValues(t *testing.T) {
	valid := promptAuditUpdateRequest(1, 1, "")
	require.NoError(t, validateUpdateConfigRequest(valid))

	tests := []struct {
		name   string
		mutate func(*UpdateConfigRequest)
		reason string
	}{
		{name: "strategy", mutate: func(req *UpdateConfigRequest) { req.Strategy = "round_robin" }, reason: "prompt_audit_invalid_strategy"},
		{name: "worker low", mutate: func(req *UpdateConfigRequest) { req.WorkerCount = 0 }, reason: "prompt_audit_invalid_worker_count"},
		{name: "worker high", mutate: func(req *UpdateConfigRequest) { req.WorkerCount = MaxWorkerCount + 1 }, reason: "prompt_audit_invalid_worker_count"},
		{name: "capacity low", mutate: func(req *UpdateConfigRequest) { req.QueueCapacity = 0 }, reason: "prompt_audit_invalid_queue_capacity"},
		{name: "capacity high", mutate: func(req *UpdateConfigRequest) { req.QueueCapacity = MaxQueueCapacity + 1 }, reason: "prompt_audit_invalid_queue_capacity"},
		{name: "unknown scanner", mutate: func(req *UpdateConfigRequest) { req.Scanners = []string{"made_up"} }, reason: "prompt_audit_invalid_scanner"},
		{name: "group required", mutate: func(req *UpdateConfigRequest) { req.AllGroups = false; req.GroupIDs = nil }, reason: "prompt_audit_groups_required"},
		{name: "group positive", mutate: func(req *UpdateConfigRequest) { req.AllGroups = false; req.GroupIDs = []int64{0} }, reason: "prompt_audit_invalid_group"},
		{name: "timeout low", mutate: func(req *UpdateConfigRequest) { req.Endpoints[0].TimeoutMS = MinTimeoutMS - 1 }, reason: "prompt_audit_invalid_timeout"},
		{name: "timeout high", mutate: func(req *UpdateConfigRequest) { req.Endpoints[0].TimeoutMS = MaxTimeoutMS + 1 }, reason: "prompt_audit_invalid_timeout"},
		{name: "input low", mutate: func(req *UpdateConfigRequest) { req.Endpoints[0].InputLimit = MinInputLimit - 1 }, reason: "prompt_audit_invalid_input_limit"},
		{name: "input high", mutate: func(req *UpdateConfigRequest) { req.Endpoints[0].InputLimit = MaxInputLimit + 1 }, reason: "prompt_audit_invalid_input_limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid
			req.Scanners = append([]string(nil), valid.Scanners...)
			req.GroupIDs = append([]int64(nil), valid.GroupIDs...)
			req.Endpoints = append([]UpdateEndpoint(nil), valid.Endpoints...)
			tt.mutate(&req)
			err := validateUpdateConfigRequest(req)
			require.Error(t, err)
			require.Equal(t, tt.reason, infraerrors.Reason(err))
		})
	}
}
