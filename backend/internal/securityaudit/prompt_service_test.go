package securityaudit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type staticSettingRepository struct {
	values map[string]string
}

func (r staticSettingRepository) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (r staticSettingRepository) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}
func (r staticSettingRepository) Set(context.Context, string, string) error { return nil }
func (r staticSettingRepository) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = r.values[key]
	}
	return result, nil
}
func (r staticSettingRepository) SetMultiple(context.Context, map[string]string) error { return nil }
func (r staticSettingRepository) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}
func (r staticSettingRepository) Delete(context.Context, string) error { return nil }

func TestPromptServiceHasExplicitIdempotentLifecycle(t *testing.T) {
	config := NewConfigManager(nil, staticSettingRepository{values: map[string]string{
		SettingKeyPromptAuditConfig: "",
		SettingKeyRiskControl:       "false",
	}}, nil, prefixEncryptor{}, testTotpKeyConfig())
	service := NewPromptService(
		config,
		NewPostgreSQLRepository(nil),
		NewRedisPayloadStore(nil),
		NewOpenAICompatibleScanner(),
		NewAtomicMetrics(),
	)

	require.Nil(t, service.cancel, "construction must not start background work")
	require.NoError(t, service.Start(context.Background()))
	require.NotNil(t, service.cancel)
	require.NoError(t, service.Start(context.Background()), "Start must be idempotent")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, service.Shutdown(ctx))
	require.Nil(t, service.cancel)
	require.NoError(t, service.Shutdown(ctx), "Shutdown must be idempotent")
}

func TestPromptServiceStartAndShutdownAreSerialized(t *testing.T) {
	startEntered := make(chan struct{})
	startRelease := make(chan struct{})
	config := &fakeConfigStore{
		startEntered: startEntered,
		startRelease: startRelease,
	}
	runner := NewRunner(
		config,
		&fakeJobRepository{},
		&fakePayloadStore{},
		PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			return integrationResult(EventPass), nil
		}),
		NewAtomicMetrics(),
	)
	service := &PromptService{
		config:       config,
		runner:       runner,
		enqueueSlots: make(chan struct{}, 1),
	}
	startDone := make(chan error, 1)
	go func() { startDone <- service.Start(context.Background()) }()
	<-startEntered

	shutdownDone := make(chan error, 1)
	shutdownAttempted := make(chan struct{})
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	go func() {
		close(shutdownAttempted)
		shutdownDone <- service.Shutdown(shutdownCtx)
	}()
	<-shutdownAttempted

	shutdownReturnedBeforeStart := false
	var shutdownErr error
	select {
	case shutdownErr = <-shutdownDone:
		shutdownReturnedBeforeStart = true
	case <-time.After(50 * time.Millisecond):
	}
	close(startRelease)
	startErr := <-startDone
	if !shutdownReturnedBeforeStart {
		shutdownErr = <-shutdownDone
	}

	require.NoError(t, startErr)
	require.NoError(t, shutdownErr)
	require.False(t, shutdownReturnedBeforeStart, "Shutdown must not overtake a Start still installing dependencies")

	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	require.NoError(t, service.Shutdown(secondCtx))
}

func TestPromptServiceShutdownTimeoutIsTerminalAndSecondShutdownCanFinish(t *testing.T) {
	config := &fakeConfigStore{}
	runner := NewRunner(
		config,
		&fakeJobRepository{},
		&fakePayloadStore{},
		PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			return integrationResult(EventPass), nil
		}),
		NewAtomicMetrics(),
	)
	_, cancelRun := context.WithCancel(context.Background())
	service := &PromptService{
		config:         config,
		runner:         runner,
		lifecycleState: promptLifecycleRunning,
		cancel:         cancelRun,
		startupDone:    closedLifecycleChannel(),
		enqueueOpen:    true,
		enqueueSlots:   make(chan struct{}, 1),
	}
	service.enqueueWG.Add(1)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShutdown()
	require.ErrorIs(t, service.Shutdown(shutdownCtx), context.DeadlineExceeded)

	startErr := service.Start(context.Background())
	service.enqueueWG.Done()
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	secondErr := service.Shutdown(secondCtx)

	require.Error(t, startErr, "Start must remain disabled once shutdown has begun, even after a timeout")
	require.NoError(t, secondErr, "a later Shutdown must be able to finish draining the original run")
}

func TestPromptServiceStoppedShutdownReturnsSavedError(t *testing.T) {
	shutdownErr := errors.New("prompt audit shutdown failed")
	service := &PromptService{
		lifecycleState: promptLifecycleStopped,
		shutdownDone:   closedLifecycleChannel(),
		shutdownErr:    shutdownErr,
	}

	require.ErrorIs(t, service.Shutdown(context.Background()), shutdownErr)
}

func TestPromptServiceShutdownCompletionWinsOverCanceledWaitContext(t *testing.T) {
	shutdownErr := errors.New("completed shutdown failed")
	done := closedLifecycleChannel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &PromptService{
		shutdownDone: done,
		shutdownErr:  shutdownErr,
	}

	for range 100 {
		require.ErrorIs(t, service.waitForShutdown(ctx, done), shutdownErr)
	}
}

func TestPromptServiceStartReportsDependencyFailureWithoutPanic(t *testing.T) {
	service := &PromptService{}
	require.Error(t, service.Start(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, service.Shutdown(ctx))
}

func TestPromptServiceAsyncRejectsOversizedPayloadBeforeLocalEnqueue(t *testing.T) {
	trace := []string{}
	metrics := NewAtomicMetrics()
	config := &fakeConfigStore{cfg: asyncConfig(), active: true}
	repo := &fakeJobRepository{trace: &trace, createJob: &Job{ID: 46}}
	payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}
	service := &PromptService{
		config:       config,
		enqueuer:     NewEnqueuer(config, repo, payload, metrics),
		metrics:      metrics,
		background:   context.Background(),
		enqueueSlots: make(chan struct{}, 1),
	}
	request := Request{
		Protocol: "openai_chat_completions",
		Body: []byte(`{"messages":[{"role":"user","content":` +
			string(mustJSON(t, strings.Repeat("x", MaxPromptAuditPayloadBytes+1))) + `}]}`),
	}

	require.NoError(t, service.Enqueue(context.Background(), request))
	require.Empty(t, trace)
	require.Empty(t, payload.values)
	require.Empty(t, service.enqueueSlots, "oversized input must not occupy a local enqueue slot")
	require.Equal(t, AuditMetricsSnapshot{Dropped: 1}, metrics.AuditSnapshot())
}

func TestPromptServiceBlockingOversizedPayloadFailsClosedWithTypedBoundedError(t *testing.T) {
	const canary = "BLOCKING_OVERSIZE_CANARY_MUST_NOT_LEAK"
	blockingConfig := guardConfig(ActiveEndpoint{
		ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: MaxInputLimit,
	})
	blockingConfig.AllGroups = true
	blockingConfig.BlockingLatestTurnOnly = true
	config := &fakeConfigStore{active: true, cfg: blockingConfig}
	scannerCalls := 0
	service := &PromptService{
		config: config,
		evaluator: newGuardEvaluator(PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			scannerCalls++
			return &NormalizedResult{Decision: EventPass, RiskLevel: RiskLow, Action: ActionAllow}, nil
		}), nil, NewAtomicMetrics(), 2, 2),
	}
	request := Request{
		Protocol: "openai_chat_completions",
		Body: []byte(`{"messages":[{"role":"user","content":` +
			string(mustJSON(t, strings.Repeat("x", MaxPromptAuditPayloadBytes)+canary)) + `}]}`),
	}

	decision, err := service.Evaluate(context.Background(), request)

	require.Nil(t, decision)
	var guardErr *GuardError
	require.ErrorAs(t, err, &guardErr)
	require.Equal(t, ErrorCodeInvalidResponse, guardErr.Code)
	require.ErrorIs(t, err, ErrPromptAuditPayloadTooLarge)
	require.LessOrEqual(t, len(err.Error()), 64)
	require.NotContains(t, err.Error(), canary)
	require.Zero(t, scannerCalls)
}

func TestPromptServiceBlockingLatestTurnOnlyUsesNarrowSnapshot(t *testing.T) {
	seen := make([]string, 0, 2)
	evaluator := newGuardEvaluator(PromptScannerFunc(func(_ context.Context, _ ActiveEndpoint, chunk string, _ []string) (*NormalizedResult, error) {
		seen = append(seen, chunk)
		return &NormalizedResult{Decision: EventPass, RiskLevel: RiskLow, Action: ActionAllow, ScannerScores: map[string]float64{}, ScannerEvidence: map[string]string{}}, nil
	}), nil, NewAtomicMetrics(), 2, 2)
	service := &PromptService{
		config: &fakeConfigStore{active: true, cfg: ActiveConfig{
			RiskControlEnabled: true, Enabled: true, BlockingEnabled: true, BlockingLatestTurnOnly: true, AllGroups: true,
			Scanners: AllScannerIDs, Endpoints: []ActiveEndpoint{{ID: "guard-1", Enabled: true, TimeoutMS: 1000, InputLimit: 4096}},
		}},
		evaluator: evaluator,
	}
	decision, err := service.Evaluate(context.Background(), Request{Protocol: "openai_chat_completions", Body: []byte(`{"messages":[{"role":"system","content":"system instruction"},{"role":"user","content":"older user input"},{"role":"assistant","content":"previous output"},{"role":"user","content":"latest user input"}]}`)})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, decision.Kind)
	require.Equal(t, []string{"latest user input", "previous output"}, seen)
}

func TestPromptServiceRejectsInvalidDeleteConfirmationClaims(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	filter := EventFilter{Decision: string(EventCritical), StartAt: &start, EndAt: &end}
	const snapshotMaxID int64 = 10
	filterHash := FilterHash(filter, snapshotMaxID)
	validClaims := deleteClaims{
		FilterHash: filterHash, SnapshotMaxID: snapshotMaxID, AdminID: 7,
		IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
	claimsToken := func(claims deleteClaims) string {
		raw, err := json.Marshal(claims)
		require.NoError(t, err)
		return string(raw)
	}
	validRequest := DeleteByFilterRequest{
		Filter: filter, SnapshotMaxID: snapshotMaxID, FilterHash: filterHash,
		ConfirmationToken: claimsToken(validClaims), Confirm: true,
	}

	tests := []struct {
		name    string
		request DeleteByFilterRequest
		adminID int64
	}{
		{name: "confirm false", request: func() DeleteByFilterRequest { value := validRequest; value.Confirm = false; return value }(), adminID: 7},
		{name: "malformed token", request: func() DeleteByFilterRequest {
			value := validRequest
			value.ConfirmationToken = "not-json"
			return value
		}(), adminID: 7},
		{name: "different administrator", request: validRequest, adminID: 8},
		{name: "filter hash mismatch", request: func() DeleteByFilterRequest {
			value := validRequest
			value.FilterHash = strings.Repeat("b", 64)
			return value
		}(), adminID: 7},
		{name: "snapshot mismatch", request: func() DeleteByFilterRequest { value := validRequest; value.SnapshotMaxID++; return value }(), adminID: 7},
		{name: "expired", request: func() DeleteByFilterRequest {
			value := validRequest
			claims := validClaims
			claims.ExpiresAt = now
			value.ConfirmationToken = claimsToken(claims)
			return value
		}(), adminID: 7},
	}

	service := &PromptService{config: &fakeConfigStore{}, clock: fixedClock{now: now}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := service.DeleteByFilter(context.Background(), test.request, test.adminID)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}
