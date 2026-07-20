package securityaudit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type advancingClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(c.step)
	return c.now
}

type fakeConfigStore struct {
	mu     sync.RWMutex
	cfg    ActiveConfig
	active bool

	runtimeStateSet            bool
	runtimeExpected            int64
	runtimeActive              int64
	runtimeLoadErr             string
	blockingActivationDegraded bool

	startEntered    chan struct{}
	startRelease    <-chan struct{}
	shutdownEntered chan struct{}
	startOnce       sync.Once
	shutdownOnce    sync.Once
	startErr        error
	shutdownErr     error
}

func (s *fakeConfigStore) Start(context.Context) error {
	if s.startEntered != nil {
		s.startOnce.Do(func() { close(s.startEntered) })
	}
	if s.startRelease != nil {
		<-s.startRelease
	}
	return s.startErr
}
func (s *fakeConfigStore) Shutdown(context.Context) error {
	if s.shutdownEntered != nil {
		s.shutdownOnce.Do(func() { close(s.shutdownEntered) })
	}
	return s.shutdownErr
}
func (s *fakeConfigStore) Active() (ActiveConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneActiveConfig(s.cfg), s.active
}
func (s *fakeConfigStore) Set(cfg ActiveConfig, active bool) {
	s.mu.Lock()
	s.cfg, s.active = cloneActiveConfig(cfg), active
	if !s.runtimeStateSet {
		s.runtimeExpected = cfg.ConfigVersion
		s.runtimeActive = cfg.ConfigVersion
	}
	s.mu.Unlock()
}
func (s *fakeConfigStore) SetRuntimeState(expected, active int64, loadError string) {
	s.mu.Lock()
	s.runtimeStateSet = true
	s.runtimeExpected = expected
	s.runtimeActive = active
	s.runtimeLoadErr = loadError
	s.mu.Unlock()
}
func (s *fakeConfigStore) EffectiveMode() Mode {
	if s.BlockingActivationDegraded() {
		return ModeBlocking
	}
	cfg, active := s.Active()
	if !active {
		return ModeOff
	}
	return cfg.EffectiveMode()
}
func (s *fakeConfigStore) BlockingActivationDegraded() bool { return s.blockingActivationDegraded }
func (s *fakeConfigStore) Public() PublicConfig             { return PublicConfig{} }
func (s *fakeConfigStore) Save(context.Context, UpdateConfigRequest, int64) (PublicConfig, error) {
	return PublicConfig{}, nil
}
func (s *fakeConfigStore) RuntimeState() (int64, int64, *time.Time, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.runtimeStateSet {
		return s.runtimeExpected, s.runtimeActive, nil, s.runtimeLoadErr
	}
	return s.cfg.ConfigVersion, s.cfg.ConfigVersion, nil, ""
}
func (s *fakeConfigStore) Encrypt(value string) (string, error) { return value, nil }
func (s *fakeConfigStore) Decrypt(value string) (string, error) { return value, nil }

type fakeJobRepository struct {
	mu sync.Mutex

	trace             *[]string
	createJob         *Job
	createErr         error
	publishErr        error
	publishCancel     context.CancelFunc
	jobStatuses       []string
	jobStatusErrs     []error
	markErr           error
	refreshErr        error
	refreshErrAfter   int
	refreshBlockAfter int
	refreshEntered    chan struct{}
	refreshRelease    <-chan struct{}
	refreshOnce       sync.Once
	terminalCalled    chan string
	completeErr       error
	retryErr          error
	failErr           error

	createdSnapshot      PromptSnapshot
	markedCode           string
	markContextErr       error
	markHasDeadline      bool
	completedResult      *NormalizedResult
	completedStore       bool
	completeCount        int
	completeClaimVersion int64
	eventCount           int
	retryAt              time.Time
	retryCode            string
	retryClaimVersion    int64
	retried              int
	failedCode           string
	failClaimVersion     int64
	failed               int
	terminalContextErr   error
	refreshes            int

	claimQueue []*Job

	recordBlockingCalls    int
	recordBlockingSnapshot PromptSnapshot
	recordBlockingResult   *NormalizedResult
	recordBlockingErr      error
}

func (r *fakeJobRepository) record(value string) {
	if r.trace != nil {
		*r.trace = append(*r.trace, value)
	}
}

func (r *fakeJobRepository) CreateStagingWithCapacity(_ context.Context, snapshot PromptSnapshot, _ int64, _, _ int) (*Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("create_staging")
	r.createdSnapshot = snapshot
	if r.createErr != nil {
		return nil, r.createErr
	}
	if r.createJob == nil {
		r.createJob = &Job{ID: 1, Snapshot: snapshot}
	}
	return r.createJob, nil
}
func (r *fakeJobRepository) PublishQueued(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("publish_queued")
	if r.publishCancel != nil {
		r.publishCancel()
	}
	return r.publishErr
}
func (r *fakeJobRepository) JobStatus(ctx context.Context, _ int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("job_status")
	r.markContextErr = ctx.Err()
	_, r.markHasDeadline = ctx.Deadline()
	if len(r.jobStatusErrs) > 0 {
		err := r.jobStatusErrs[0]
		r.jobStatusErrs = r.jobStatusErrs[1:]
		return "", err
	}
	if len(r.jobStatuses) == 0 {
		return "staging", nil
	}
	status := r.jobStatuses[0]
	r.jobStatuses = r.jobStatuses[1:]
	return status, nil
}
func (r *fakeJobRepository) MarkStagingFailed(ctx context.Context, _ int64, code, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("mark_staging_failed")
	r.markedCode = code
	r.markContextErr = ctx.Err()
	_, r.markHasDeadline = ctx.Deadline()
	return r.markErr
}
func (r *fakeJobRepository) ClaimNextJob(context.Context, time.Time) (*Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.claimQueue) == 0 {
		return nil, false, nil
	}
	job := r.claimQueue[0]
	r.claimQueue = r.claimQueue[1:]
	return job, true, nil
}
func (r *fakeJobRepository) RefreshLease(ctx context.Context, _ int64, _ int64, _ time.Time) error {
	r.mu.Lock()
	r.refreshes++
	refreshes := r.refreshes
	blockAfter := r.refreshBlockAfter
	entered := r.refreshEntered
	release := r.refreshRelease
	if blockAfter > 0 && refreshes >= blockAfter && entered != nil {
		r.refreshOnce.Do(func() { close(entered) })
	}
	if r.refreshErrAfter > 0 && refreshes < r.refreshErrAfter {
		r.mu.Unlock()
		return nil
	}
	err := r.refreshErr
	r.mu.Unlock()
	if blockAfter > 0 && refreshes >= blockAfter && release != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
	}
	return err
}
func (r *fakeJobRepository) Complete(ctx context.Context, job *Job, result *NormalizedResult, storePass bool) (*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminalCalled != nil {
		r.terminalCalled <- "complete"
	}
	r.completeCount++
	r.completeClaimVersion = job.ClaimVersion
	r.terminalContextErr = ctx.Err()
	r.completedResult, r.completedStore = result, storePass
	if r.completeErr != nil {
		return nil, r.completeErr
	}
	if result.Decision == EventPass && !storePass {
		return nil, nil
	}
	r.eventCount++
	return &Event{ID: 99, Decision: result.Decision}, nil
}
func (r *fakeJobRepository) Retry(ctx context.Context, _ int64, claimVersion int64, next time.Time, code, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminalCalled != nil {
		r.terminalCalled <- "retry"
	}
	r.retried++
	r.retryClaimVersion = claimVersion
	r.terminalContextErr = ctx.Err()
	r.retryAt, r.retryCode = next, code
	return r.retryErr
}
func (r *fakeJobRepository) Fail(ctx context.Context, _ int64, claimVersion int64, code, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminalCalled != nil {
		r.terminalCalled <- "fail"
	}
	r.failed++
	r.failClaimVersion = claimVersion
	r.terminalContextErr = ctx.Err()
	r.failedCode = code
	return r.failErr
}
func (r *fakeJobRepository) ReclaimStale(context.Context, time.Time, time.Time, int) (int64, error) {
	return 0, nil
}
func (r *fakeJobRepository) QueueStats(context.Context) (QueueStats, error) { return QueueStats{}, nil }
func (r *fakeJobRepository) CleanupTerminalJobs(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}
func (r *fakeJobRepository) RecordBlocking(_ context.Context, snapshot PromptSnapshot, _ int64, result *NormalizedResult, _ bool) (*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordBlockingCalls++
	r.recordBlockingSnapshot, r.recordBlockingResult = snapshot, result
	return nil, r.recordBlockingErr
}

type fakePayloadStore struct {
	mu sync.Mutex

	trace             *[]string
	values            map[int64]string
	setErr            error
	getErr            error
	deleteErr         error
	pingErr           error
	setTTL            time.Duration
	deleted           []int64
	deleteContextErr  error
	deleteHasDeadline bool
	pingEntered       chan struct{}
	pingRelease       <-chan struct{}
	pingOnce          sync.Once
}

func (s *fakePayloadStore) Set(_ context.Context, jobID int64, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trace != nil {
		*s.trace = append(*s.trace, "payload_set")
	}
	if s.setErr != nil {
		return s.setErr
	}
	if s.values == nil {
		s.values = map[int64]string{}
	}
	s.values[jobID], s.setTTL = value, ttl
	return nil
}
func (s *fakePayloadStore) Get(_ context.Context, jobID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.values[jobID]
	if !ok {
		return "", errors.New("missing")
	}
	return value, nil
}
func (s *fakePayloadStore) Delete(ctx context.Context, jobID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trace != nil {
		*s.trace = append(*s.trace, "payload_delete")
	}
	s.deleted = append(s.deleted, jobID)
	s.deleteContextErr = ctx.Err()
	_, s.deleteHasDeadline = ctx.Deadline()
	delete(s.values, jobID)
	return s.deleteErr
}
func (s *fakePayloadStore) Ping(context.Context) error {
	if s.pingEntered != nil {
		s.pingOnce.Do(func() { close(s.pingEntered) })
	}
	if s.pingRelease != nil {
		<-s.pingRelease
	}
	return s.pingErr
}

func asyncConfig() ActiveConfig {
	return ActiveConfig{
		RiskControlEnabled: true, Enabled: true, BlockingEnabled: false, Strategy: "priority",
		WorkerCount: 1, QueueCapacity: 8, Scanners: []string{"pii"}, AllGroups: true, ConfigVersion: 7,
		Endpoints: []ActiveEndpoint{{ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: 3}},
	}
}

func asyncRequest() Request {
	return Request{RequestID: "request-async", Protocol: "openai_chat_completions", Body: []byte(`{"messages":[{"role":"user","content":"payload canary text"}]}`)}
}

func TestEnqueuerStagingPayloadPublishProtocolAndFailureCleanup(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		trace := []string{}
		repo := &fakeJobRepository{trace: &trace, createJob: &Job{ID: 41}}
		payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}
		enqueuer := NewEnqueuer(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload)
		require.NoError(t, enqueuer.Enqueue(context.Background(), asyncRequest()))
		require.Equal(t, []string{"create_staging", "payload_set", "publish_queued"}, trace)
		require.Empty(t, repo.createdSnapshot.ScanText)
		require.Equal(t, "payload canary text", payload.values[41])
		require.Equal(t, DefaultPayloadTTL, payload.setTTL)
	})

	t.Run("queue admission failures never touch payload", func(t *testing.T) {
		for _, createErr := range []error{ErrQueueFull, ErrQueueAdmissionBusy, errors.New("database down")} {
			trace := []string{}
			repo := &fakeJobRepository{trace: &trace, createErr: createErr}
			payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}
			err := NewEnqueuer(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload).Enqueue(context.Background(), asyncRequest())
			require.ErrorIs(t, err, createErr)
			require.Equal(t, []string{"create_staging"}, trace)
		}
	})

	t.Run("payload failure marks staging failed", func(t *testing.T) {
		trace := []string{}
		repo := &fakeJobRepository{trace: &trace, createJob: &Job{ID: 42}}
		payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}, setErr: errors.New("redis down")}
		err := NewEnqueuer(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload).Enqueue(context.Background(), asyncRequest())
		require.Error(t, err)
		require.Equal(t, []string{"create_staging", "payload_set", "payload_delete", "mark_staging_failed"}, trace)
		require.Equal(t, "payload_store_failed", repo.markedCode)
	})

	t.Run("publish failure removes payload only after marking staging failed", func(t *testing.T) {
		trace := []string{}
		repo := &fakeJobRepository{trace: &trace, createJob: &Job{ID: 43}, publishErr: errors.New("publish down")}
		payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}
		err := NewEnqueuer(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload).Enqueue(context.Background(), asyncRequest())
		require.Error(t, err)
		require.Equal(t, []string{"create_staging", "payload_set", "publish_queued", "job_status", "mark_staging_failed", "payload_delete"}, trace)
		require.Equal(t, "queue_publish_failed", repo.markedCode)
		require.NotContains(t, payload.values, int64(43))
	})
}

func TestEnqueuerPublishErrorReconcilesCommittedQueueWithoutDeletingPayload(t *testing.T) {
	publishErr := errors.New("commit acknowledgement lost")
	trace := []string{}
	repo := &fakeJobRepository{
		trace:       &trace,
		createJob:   &Job{ID: 49},
		publishErr:  publishErr,
		jobStatuses: []string{"queued"},
	}
	payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}
	metrics := NewAtomicMetrics()

	err := NewEnqueuer(
		&fakeConfigStore{cfg: asyncConfig(), active: true},
		repo,
		payload,
		metrics,
	).Enqueue(context.Background(), asyncRequest())

	require.NoError(t, err, "a queued row proves that the ambiguous publish committed")
	require.Equal(t, []string{"create_staging", "payload_set", "publish_queued", "job_status"}, trace)
	require.Equal(t, "payload canary text", payload.values[49])
	require.Empty(t, payload.deleted)
	require.Equal(t, AuditMetricsSnapshot{Enqueued: 1}, metrics.AuditSnapshot())
}

func TestEnqueuerPublishErrorKeepsPayloadUntilFailedStateIsConfirmed(t *testing.T) {
	publishErr := errors.New("publish outcome unknown")
	statusErr := errors.New("status unavailable")
	trace := []string{}
	repo := &fakeJobRepository{
		trace:         &trace,
		createJob:     &Job{ID: 50},
		publishErr:    publishErr,
		jobStatusErrs: []error{statusErr},
	}
	payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}

	err := NewEnqueuer(
		&fakeConfigStore{cfg: asyncConfig(), active: true},
		repo,
		payload,
	).Enqueue(context.Background(), asyncRequest())

	require.ErrorIs(t, err, publishErr)
	require.ErrorIs(t, err, statusErr)
	require.Equal(t, []string{"create_staging", "payload_set", "publish_queued", "job_status"}, trace)
	require.Equal(t, "payload canary text", payload.values[50], "an unconfirmed row may be queued and still needs its payload")
	require.Empty(t, payload.deleted)
	require.Empty(t, repo.markedCode)
}

func TestEnqueuerPublishFailureUsesBoundedCleanupAfterCallerCancellation(t *testing.T) {
	publishErr := errors.New("publish unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	trace := []string{}
	repo := &fakeJobRepository{
		trace:         &trace,
		createJob:     &Job{ID: 47},
		publishErr:    publishErr,
		publishCancel: cancel,
	}
	payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}

	err := NewEnqueuer(
		&fakeConfigStore{cfg: asyncConfig(), active: true},
		repo,
		payload,
	).Enqueue(ctx, asyncRequest())

	require.ErrorIs(t, err, publishErr)
	require.Equal(t, []string{"create_staging", "payload_set", "publish_queued", "job_status", "mark_staging_failed", "payload_delete"}, trace)
	require.NoError(t, payload.deleteContextErr, "payload cleanup must not reuse the canceled caller context")
	require.True(t, payload.deleteHasDeadline, "payload cleanup must have a bounded independent deadline")
	require.NoError(t, repo.markContextErr, "staging cleanup must not reuse the canceled caller context")
	require.True(t, repo.markHasDeadline, "staging cleanup must have a bounded independent deadline")
}

func TestEnqueuerPublishFailureSurfacesBothCleanupErrorsWithoutPromptLeak(t *testing.T) {
	const promptCanary = "ENQUEUE_BODY_CANARY_MUST_NOT_LEAK"
	publishErr := errors.New("publish unavailable")
	deleteErr := errors.New("payload cleanup unavailable")
	markErr := errors.New("staging cleanup unavailable")
	trace := []string{}
	repo := &fakeJobRepository{
		trace:       &trace,
		createJob:   &Job{ID: 48},
		publishErr:  publishErr,
		jobStatuses: []string{"staging", "failed"},
		markErr:     markErr,
	}
	payload := &fakePayloadStore{
		trace:     &trace,
		values:    map[int64]string{},
		deleteErr: deleteErr,
	}
	request := Request{
		RequestID: "cleanup-errors",
		Protocol:  "openai_chat_completions",
		Body:      []byte(`{"messages":[{"role":"user","content":"` + promptCanary + `"}]}`),
	}
	var output strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	err := NewEnqueuer(
		&fakeConfigStore{cfg: asyncConfig(), active: true},
		repo,
		payload,
	).Enqueue(context.Background(), request)

	require.ErrorIs(t, err, publishErr)
	require.ErrorIs(t, err, deleteErr, "payload cleanup failure must remain observable")
	require.ErrorIs(t, err, markErr, "staging cleanup failure must remain observable")
	require.NotContains(t, err.Error(), promptCanary)
	require.NotContains(t, output.String(), promptCanary)
	require.Equal(t, []string{"create_staging", "payload_set", "publish_queued", "job_status", "mark_staging_failed", "job_status", "payload_delete"}, trace)
}

func TestEnqueuerDropsOversizedPayloadBeforeQueueOrRedisPersistence(t *testing.T) {
	const canary = "PROMPT_OVERSIZE_CANARY_MUST_NOT_BE_OBSERVED"
	trace := []string{}
	repo := &fakeJobRepository{trace: &trace, createJob: &Job{ID: 45}}
	payload := &fakePayloadStore{trace: &trace, values: map[int64]string{}}
	metrics := NewAtomicMetrics()
	request := Request{
		RequestID: "oversized-async",
		Protocol:  "openai_chat_completions",
		Body: []byte(`{"messages":[{"role":"user","content":` +
			string(mustJSON(t, strings.Repeat("x", MaxPromptAuditPayloadBytes)+canary)) + `}]}`),
	}

	var output strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	err := NewEnqueuer(
		&fakeConfigStore{cfg: asyncConfig(), active: true},
		repo,
		payload,
		metrics,
	).Enqueue(context.Background(), request)

	require.NoError(t, err, "async audit must safely skip oversized input")
	require.Empty(t, trace, "capacity admission and payload persistence must not run")
	require.Empty(t, payload.values)
	require.Equal(t, AuditMetricsSnapshot{Dropped: 1}, metrics.AuditSnapshot())
	require.Contains(t, output.String(), EventEnqueueDropped)
	require.Contains(t, output.String(), ErrorCodePayloadTooLarge)
	require.NotContains(t, output.String(), canary)
	require.Less(t, output.Len(), 2048)
}

func TestEnqueuerSkipsOffOutOfScopeAndNoText(t *testing.T) {
	tests := []struct {
		name string
		cfg  ActiveConfig
		req  Request
	}{
		{name: "off", cfg: ActiveConfig{}, req: asyncRequest()},
		{name: "out of scope", cfg: func() ActiveConfig {
			cfg := asyncConfig()
			cfg.AllGroups = false
			cfg.GroupIDs = []int64{9}
			return cfg
		}(), req: asyncRequest()},
		{name: "no user text", cfg: asyncConfig(), req: Request{Protocol: "openai_chat_completions", Body: []byte(`{"messages":[{"role":"function","content":"not audited"}]}`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeJobRepository{}
			err := NewEnqueuer(&fakeConfigStore{cfg: tt.cfg, active: true}, repo, &fakePayloadStore{}).Enqueue(context.Background(), tt.req)
			require.NoError(t, err)
			require.Zero(t, repo.createdSnapshot.MessageCount)
		})
	}
}

func TestEnqueuerRecordsAcceptedDroppedAndSkippedMetrics(t *testing.T) {
	t.Run("accepted increments enqueued", func(t *testing.T) {
		metrics := NewAtomicMetrics()
		repo := &fakeJobRepository{createJob: &Job{ID: 44}}
		payload := &fakePayloadStore{values: map[int64]string{}}

		require.NoError(t, NewEnqueuer(
			&fakeConfigStore{cfg: asyncConfig(), active: true},
			repo,
			payload,
			metrics,
		).Enqueue(context.Background(), asyncRequest()))

		require.Equal(t, AuditMetricsSnapshot{Enqueued: 1}, metrics.AuditSnapshot())
	})

	t.Run("queue full increments dropped", func(t *testing.T) {
		metrics := NewAtomicMetrics()
		repo := &fakeJobRepository{createErr: ErrQueueFull}

		err := NewEnqueuer(
			&fakeConfigStore{cfg: asyncConfig(), active: true},
			repo,
			&fakePayloadStore{},
			metrics,
		).Enqueue(context.Background(), asyncRequest())

		require.ErrorIs(t, err, ErrQueueFull)
		require.Equal(t, AuditMetricsSnapshot{Dropped: 1}, metrics.AuditSnapshot())
	})

	t.Run("skipped request does not increment dropped", func(t *testing.T) {
		metrics := NewAtomicMetrics()

		require.NoError(t, NewEnqueuer(
			&fakeConfigStore{cfg: ActiveConfig{}, active: true},
			&fakeJobRepository{},
			&fakePayloadStore{},
			metrics,
		).Enqueue(context.Background(), asyncRequest()))

		require.Equal(t, AuditMetricsSnapshot{}, metrics.AuditSnapshot())
	})
}

func workerJob(attempts, maxAttempts int) *Job {
	return &Job{ID: 51, ClaimVersion: 3, Attempts: attempts, MaxAttempts: maxAttempts, ConfigVersion: 7,
		Snapshot: PromptSnapshot{RequestID: "worker-request", PromptLength: 6, RedactedPreview: "red***"}}
}

func TestWorkerCompletesPassWithoutEventRefreshesEveryChunkAndDeletesPayload(t *testing.T) {
	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: map[int64]string{51: "abcdef"}}
	scannerCalls := 0
	scanner := PromptScannerFunc(func(_ context.Context, endpoint ActiveEndpoint, chunk string, _ []string) (*NormalizedResult, error) {
		scannerCalls++
		return &NormalizedResult{Decision: EventPass, RiskLevel: RiskLow, Action: ActionAllow, Safety: "Safe", Categories: []string{}, MatchedScanners: []string{}, ScannerScores: map[string]float64{}, ScannerEvidence: map[string]string{}, GuardEndpointID: endpoint.ID}, nil
	})
	metrics := NewAtomicMetrics()
	runner := NewRunner(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload, scanner, metrics)
	runner.clock = fixedClock{now: time.Unix(100, 0).UTC()}
	require.NoError(t, runner.processJob(context.Background(), 0, asyncConfig(), workerJob(1, 3)))
	require.Equal(t, 2, scannerCalls)
	require.Equal(t, 2, repo.refreshes)
	require.NotNil(t, repo.completedResult)
	require.Equal(t, EventPass, repo.completedResult.Decision)
	require.False(t, repo.completedStore)
	require.Equal(t, []int64{51}, payload.deleted)
	require.Equal(t, int64(1), metrics.Snapshot().Total)
	require.Equal(t, int64(1), metrics.Snapshot().Allowed)
}

func TestWorkerPendingJobUsesCurrentRotatedEndpointAndToken(t *testing.T) {
	oldConfig := asyncConfig()
	oldConfig.Endpoints = []ActiveEndpoint{{
		ID: "old", BaseURL: "https://old-guard.example.test", Token: "old-token", Enabled: true, TimeoutMS: 1000, InputLimit: 1000,
	}}
	currentConfig := cloneActiveConfig(oldConfig)
	currentConfig.ConfigVersion++
	currentConfig.Endpoints = []ActiveEndpoint{{
		ID: "new", BaseURL: "https://new-guard.example.test", Token: "new-token", Enabled: true, TimeoutMS: 1000, InputLimit: 1000,
	}}
	configStore := &fakeConfigStore{cfg: currentConfig, active: true}
	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: map[int64]string{51: "rotate me"}}
	var scanned ActiveEndpoint
	runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(_ context.Context, endpoint ActiveEndpoint, _ string, _ []string) (*NormalizedResult, error) {
		scanned = endpoint
		return integrationResult(EventPass), nil
	}), NewAtomicMetrics())
	job := workerJob(1, 3)

	require.NoError(t, runner.processJob(context.Background(), 0, oldConfig, job))
	require.Equal(t, "new", scanned.ID)
	require.Equal(t, "https://new-guard.example.test", scanned.BaseURL)
	require.Equal(t, "new-token", scanned.Token)
	require.Equal(t, currentConfig.ConfigVersion, job.ConfigVersion)
}

func TestWorkerRejectsUntrustedRuntimeStateBeforeFirstDispatch(t *testing.T) {
	tests := []struct {
		name      string
		expected  int64
		active    int64
		loadError string
	}{
		{name: "expected version is newer than active", expected: 8, active: 7},
		{name: "active version has a load error", expected: 7, active: 7, loadError: "config_load_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := asyncConfig()
			configStore := &fakeConfigStore{cfg: cfg, active: true}
			configStore.SetRuntimeState(tt.expected, tt.active, tt.loadError)
			repo := &fakeJobRepository{}
			payload := &fakePayloadStore{values: map[int64]string{51: "do not dispatch with stale credentials"}}
			scannerCalls := 0
			runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
				scannerCalls++
				return integrationResult(EventPass), nil
			}), NewAtomicMetrics())

			err := runner.processJob(context.Background(), 0, cfg, workerJob(1, 3))

			require.Error(t, err)
			require.Zero(t, scannerCalls)
			require.Equal(t, 1, repo.failed)
			require.Equal(t, "audit_config_changed", repo.failedCode)
		})
	}
}

func TestWorkerRejectsUntrustedModeOrGroupBeforeFirstDispatch(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*fakeConfigStore, *ActiveConfig)
	}{
		{
			name: "store effective mode is not async",
			prepare: func(store *fakeConfigStore, _ *ActiveConfig) {
				store.blockingActivationDegraded = true
			},
		},
		{
			name: "job group is no longer included",
			prepare: func(_ *fakeConfigStore, cfg *ActiveConfig) {
				cfg.AllGroups = false
				cfg.GroupIDs = []int64{9}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := asyncConfig()
			configStore := &fakeConfigStore{}
			tt.prepare(configStore, &cfg)
			configStore.Set(cfg, true)
			repo := &fakeJobRepository{}
			payload := &fakePayloadStore{values: map[int64]string{51: "do not dispatch outside trusted async scope"}}
			scannerCalls := 0
			runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
				scannerCalls++
				return integrationResult(EventPass), nil
			}), NewAtomicMetrics())

			err := runner.processJob(context.Background(), 0, cfg, workerJob(1, 3))

			require.Error(t, err)
			require.Zero(t, scannerCalls)
			require.Equal(t, 1, repo.failed)
			require.Equal(t, "audit_config_changed", repo.failedCode)
		})
	}
}

func TestWorkerStopsDispatchingWhenRuntimeStateDriftsBetweenChunks(t *testing.T) {
	tests := []struct {
		name      string
		expected  int64
		active    int64
		loadError string
	}{
		{name: "expected version advances", expected: 8, active: 7},
		{name: "runtime active version diverges", expected: 7, active: 8},
		{name: "load becomes untrusted", expected: 7, active: 7, loadError: "config_load_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := asyncConfig()
			cfg.Endpoints[0].InputLimit = 3
			configStore := &fakeConfigStore{cfg: cfg, active: true}
			repo := &fakeJobRepository{}
			payload := &fakePayloadStore{values: map[int64]string{51: "abcdef"}}
			scannerCalls := 0
			runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
				scannerCalls++
				if scannerCalls == 1 {
					configStore.SetRuntimeState(tt.expected, tt.active, tt.loadError)
				}
				return integrationResult(EventPass), nil
			}), NewAtomicMetrics())

			err := runner.processJob(context.Background(), 0, cfg, workerJob(1, 3))

			require.Error(t, err)
			require.Equal(t, 1, scannerCalls)
			require.Equal(t, 1, repo.failed)
			require.Equal(t, "audit_config_changed", repo.failedCode)
		})
	}
}

func TestWorkerTrustedRuntimeStateExecutesAllChunks(t *testing.T) {
	cfg := asyncConfig()
	cfg.Endpoints[0].InputLimit = 3
	configStore := &fakeConfigStore{cfg: cfg, active: true}
	configStore.SetRuntimeState(cfg.ConfigVersion, cfg.ConfigVersion, "")
	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: map[int64]string{51: "abcdef"}}
	scannerCalls := 0
	runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
		scannerCalls++
		return integrationResult(EventPass), nil
	}), NewAtomicMetrics())

	require.NoError(t, runner.processJob(context.Background(), 0, cfg, workerJob(1, 3)))
	require.Equal(t, 2, scannerCalls)
	require.Equal(t, 1, repo.completeCount)
	require.Zero(t, repo.failed)
}

func TestWorkerStopsDispatchingWhenConfigChangesBetweenChunks(t *testing.T) {
	initial := asyncConfig()
	initial.Endpoints[0].InputLimit = 3
	configStore := &fakeConfigStore{cfg: initial, active: true}
	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: map[int64]string{51: "abcdef"}}
	scannerCalls := 0
	runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
		scannerCalls++
		if scannerCalls == 1 {
			rotated := cloneActiveConfig(initial)
			rotated.ConfigVersion++
			rotated.Endpoints[0].Token = "rotated"
			configStore.Set(rotated, true)
		}
		return integrationResult(EventPass), nil
	}), NewAtomicMetrics())
	job := workerJob(1, 3)

	err := runner.processJob(context.Background(), 0, initial, job)
	require.Error(t, err)
	require.Equal(t, 1, scannerCalls)
	require.Equal(t, 1, repo.failed)
	require.Equal(t, "audit_config_changed", repo.failedCode)
}

func TestWorkerClaimedJobDoesNotDispatchAfterAuditDisabled(t *testing.T) {
	oldConfig := asyncConfig()
	disabledConfig := cloneActiveConfig(oldConfig)
	disabledConfig.ConfigVersion++
	disabledConfig.Enabled = false
	configStore := &fakeConfigStore{cfg: disabledConfig, active: true}
	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: map[int64]string{51: "do not dispatch"}}
	scannerCalls := 0
	runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
		scannerCalls++
		return integrationResult(EventPass), nil
	}), NewAtomicMetrics())
	job := workerJob(1, 3)

	err := runner.processJob(context.Background(), 0, oldConfig, job)
	require.Error(t, err)
	require.Zero(t, scannerCalls)
	require.Equal(t, 1, repo.failed)
	require.Equal(t, "audit_disabled", repo.failedCode)
	require.Equal(t, []int64{job.ID}, payload.deleted)
	require.Equal(t, disabledConfig.ConfigVersion, job.ConfigVersion)
}

func TestWorkerRetryBackoffTerminalFailureAndFailover(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	for _, tt := range []struct {
		name        string
		attempts    int
		maxAttempts int
		err         *GuardError
		wantRetry   bool
		wantBackoff time.Duration
	}{
		{name: "first retry", attempts: 1, maxAttempts: 3, err: &GuardError{Code: ErrorCodeUnavailable, Retryable: true}, wantRetry: true, wantBackoff: 5 * time.Second},
		{name: "second retry", attempts: 2, maxAttempts: 3, err: &GuardError{Code: ErrorCodeUnavailable, Retryable: true}, wantRetry: true, wantBackoff: 30 * time.Second},
		{name: "third retry", attempts: 3, maxAttempts: 4, err: &GuardError{Code: ErrorCodeUnavailable, Retryable: true}, wantRetry: true, wantBackoff: 2 * time.Minute},
		{name: "max attempts", attempts: 3, maxAttempts: 3, err: &GuardError{Code: ErrorCodeUnavailable, Retryable: true}},
		{name: "invalid terminal", attempts: 1, maxAttempts: 3, err: &GuardError{Code: ErrorCodeInvalidResponse, Retryable: false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeJobRepository{}
			payload := &fakePayloadStore{values: map[int64]string{51: "abc"}}
			metrics := NewAtomicMetrics()
			runner := NewRunner(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
				return nil, tt.err
			}), metrics)
			runner.clock = fixedClock{now: now}
			err := runner.processJob(context.Background(), 0, asyncConfig(), workerJob(tt.attempts, tt.maxAttempts))
			require.Error(t, err)
			if tt.wantRetry {
				require.Equal(t, 1, repo.retried)
				require.Equal(t, now.Add(tt.wantBackoff), repo.retryAt)
				require.Empty(t, payload.deleted)
			} else {
				require.Equal(t, 1, repo.failed)
				require.Equal(t, tt.err.Code, repo.failedCode)
				require.Equal(t, []int64{51}, payload.deleted)
			}
			snapshot := metrics.Snapshot()
			require.Equal(t, int64(1), snapshot.Total)
			if tt.err.Code == ErrorCodeInvalidResponse {
				require.Equal(t, int64(1), snapshot.Invalid)
			} else {
				require.Equal(t, int64(1), snapshot.Unavailable)
			}
		})
	}

	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: map[int64]string{51: "abc"}}
	metrics := NewAtomicMetrics()
	scanner := PromptScannerFunc(func(_ context.Context, endpoint ActiveEndpoint, _ string, _ []string) (*NormalizedResult, error) {
		if endpoint.ID == "first" {
			return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true}
		}
		return integrationResult(EventPass), nil
	})
	cfg := asyncConfig()
	cfg.Endpoints = []ActiveEndpoint{{ID: "first", Enabled: true, InputLimit: 10}, {ID: "second", Enabled: true, InputLimit: 10}}
	runner := NewRunner(&fakeConfigStore{cfg: cfg, active: true}, repo, payload, scanner, metrics)
	require.NoError(t, runner.processJob(context.Background(), 0, cfg, workerJob(1, 3)))
	require.Equal(t, int64(1), metrics.Snapshot().Failovers)
}

func TestWorkerPanicLeaseLossAndLifecycleAreContained(t *testing.T) {
	t.Run("panic", func(t *testing.T) {
		repo := &fakeJobRepository{}
		payload := &fakePayloadStore{values: map[int64]string{51: "abc"}}
		runner := NewRunner(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			panic("scanner panic canary")
		}), NewAtomicMetrics())
		require.NotPanics(t, func() { runner.processSafely(context.Background(), 0, asyncConfig(), workerJob(1, 3)) })
		_, _, failed, _, _, code, message := runner.Snapshot()
		require.Equal(t, int64(1), failed)
		require.Equal(t, "worker_panic", code)
		require.NotContains(t, message, "canary")
		require.Equal(t, 1, repo.failed)
	})

	t.Run("lease loss", func(t *testing.T) {
		repo := &fakeJobRepository{refreshErr: ErrLeaseLost}
		payload := &fakePayloadStore{values: map[int64]string{51: "abc"}}
		calls := 0
		runner := NewRunner(&fakeConfigStore{cfg: asyncConfig(), active: true}, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			calls++
			return integrationResult(EventPass), nil
		}), NewAtomicMetrics())
		require.ErrorIs(t, runner.processJob(context.Background(), 0, asyncConfig(), workerJob(1, 3)), ErrLeaseLost)
		require.Zero(t, calls)
		require.Zero(t, repo.retried)
		require.Zero(t, repo.failed)
	})

	t.Run("start and shutdown", func(t *testing.T) {
		cfg := asyncConfig()
		cfg.Enabled = false
		configStore := &fakeConfigStore{cfg: cfg, active: true}
		repo := &fakeJobRepository{}
		payload := &fakePayloadStore{pingErr: errors.New("redis unavailable")}
		runner := NewRunner(configStore, repo, payload, PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			return integrationResult(EventPass), nil
		}), NewAtomicMetrics())
		require.NoError(t, runner.Start(context.Background()))
		require.NoError(t, runner.Start(context.Background()))
		_, _, _, _, _, code, _ := runner.Snapshot()
		require.Equal(t, "payload_store_unavailable", code)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, runner.Shutdown(ctx))
		require.NoError(t, runner.Shutdown(ctx))
	})

	t.Run("shutdown timeout is bounded", func(t *testing.T) {
		runner := &Runner{state: promptLifecycleRunning, startupDone: closedLifecycleChannel()}
		release := make(chan struct{})
		runner.wg.Add(1)
		go func() {
			defer runner.wg.Done()
			<-release
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, runner.Shutdown(ctx), context.DeadlineExceeded)
		close(release)
		ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
		defer cancel2()
		require.NoError(t, runner.Shutdown(ctx2))
	})
}

func TestRunnerStoppedShutdownReturnsSavedError(t *testing.T) {
	shutdownErr := errors.New("worker shutdown failed")
	runner := &Runner{state: promptLifecycleStopped, shutdownErr: shutdownErr}

	require.ErrorIs(t, runner.Shutdown(context.Background()), shutdownErr)
}

func TestRunnerStartAndShutdownAreSerialized(t *testing.T) {
	pingEntered := make(chan struct{})
	pingRelease := make(chan struct{})
	cfg := asyncConfig()
	cfg.Enabled = false
	runner := NewRunner(
		&fakeConfigStore{cfg: cfg, active: true},
		&fakeJobRepository{},
		&fakePayloadStore{pingEntered: pingEntered, pingRelease: pingRelease},
		PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			return integrationResult(EventPass), nil
		}),
		NewAtomicMetrics(),
	)
	startDone := make(chan error, 1)
	go func() { startDone <- runner.Start(context.Background()) }()
	<-pingEntered

	shutdownDone := make(chan error, 1)
	shutdownAttempted := make(chan struct{})
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	go func() {
		close(shutdownAttempted)
		shutdownDone <- runner.Shutdown(shutdownCtx)
	}()
	<-shutdownAttempted

	shutdownReturnedBeforeStart := false
	select {
	case <-shutdownDone:
		shutdownReturnedBeforeStart = true
	case <-time.After(50 * time.Millisecond):
	}
	close(pingRelease)
	startErr := <-startDone
	if !shutdownReturnedBeforeStart {
		require.NoError(t, <-shutdownDone)
	}

	require.NoError(t, startErr)
	require.False(t, shutdownReturnedBeforeStart, "Shutdown must not complete while Start is still installing workers")
}

func TestRunnerShutdownTimeoutIsTerminalAndSecondShutdownCanFinish(t *testing.T) {
	cfg := asyncConfig()
	cfg.Enabled = false
	runner := NewRunner(
		&fakeConfigStore{cfg: cfg, active: true},
		&fakeJobRepository{},
		&fakePayloadStore{},
		PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			return integrationResult(EventPass), nil
		}),
		NewAtomicMetrics(),
	)
	require.NoError(t, runner.Start(context.Background()))

	release := make(chan struct{})
	runner.wg.Add(1)
	go func() {
		defer runner.wg.Done()
		<-release
	}()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShutdown()
	require.ErrorIs(t, runner.Shutdown(shutdownCtx), context.DeadlineExceeded)

	startCtx, cancelStart := context.WithCancel(context.Background())
	cancelStart()
	startErr := runner.Start(startCtx)
	runner.mu.Lock()
	restartInstalled := runner.state == promptLifecycleStarting || runner.state == promptLifecycleRunning
	runner.mu.Unlock()
	close(release)
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	secondErr := runner.Shutdown(secondCtx)

	require.False(t, restartInstalled, "Start must not install a new run once shutdown has begun")
	require.Error(t, startErr, "Start must remain disabled once shutdown has begun")
	require.NoError(t, secondErr, "a later Shutdown must finish draining the original run")
}

func TestWorkerQuiescesHeartbeatBeforeFencedTerminalTransition(t *testing.T) {
	tests := []struct {
		name        string
		attempts    int
		maxAttempts int
		scan        func() (*NormalizedResult, error)
		want        string
	}{
		{name: "complete", attempts: 1, maxAttempts: 3, scan: func() (*NormalizedResult, error) { return integrationResult(EventPass), nil }, want: "complete"},
		{name: "retry", attempts: 1, maxAttempts: 3, scan: func() (*NormalizedResult, error) {
			return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true}
		}, want: "retry"},
		{name: "fail", attempts: 3, maxAttempts: 3, scan: func() (*NormalizedResult, error) { return nil, &GuardError{Code: ErrorCodeInvalidResponse} }, want: "fail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refreshEntered := make(chan struct{})
			refreshRelease := make(chan struct{})
			terminalCalled := make(chan string, 1)
			repo := &fakeJobRepository{
				refreshBlockAfter: 2,
				refreshEntered:    refreshEntered,
				refreshRelease:    refreshRelease,
				terminalCalled:    terminalCalled,
			}
			cfg := asyncConfig()
			cfg.Endpoints[0].InputLimit = MaxInputLimit
			payload := &fakePayloadStore{values: map[int64]string{51: "heartbeat quiesce input"}}
			runner := NewRunner(
				&fakeConfigStore{cfg: cfg, active: true},
				repo,
				payload,
				PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
					<-refreshEntered
					return tt.scan()
				}),
				NewAtomicMetrics(),
			)
			runner.leaseHeartbeatInterval = 5 * time.Millisecond

			processDone := make(chan error, 1)
			go func() {
				processDone <- runner.processJob(context.Background(), 0, cfg, workerJob(tt.attempts, tt.maxAttempts))
			}()
			<-refreshEntered

			select {
			case terminal := <-terminalCalled:
				t.Fatalf("%s ran before the in-flight heartbeat joined", terminal)
			case <-time.After(30 * time.Millisecond):
			}
			close(refreshRelease)

			require.Equal(t, tt.want, <-terminalCalled)
			err := <-processDone
			if tt.want == "complete" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, repo.terminalContextErr, "normal heartbeat quiesce must not cancel the terminal-write context")
			switch tt.want {
			case "complete":
				require.Equal(t, int64(3), repo.completeClaimVersion)
			case "retry":
				require.Equal(t, int64(3), repo.retryClaimVersion)
			case "fail":
				require.Equal(t, int64(3), repo.failClaimVersion)
			}
		})
	}
}

func TestWorkerHeartbeatFailureDuringQuiesceNeverWritesTerminalState(t *testing.T) {
	refreshEntered := make(chan struct{})
	refreshRelease := make(chan struct{})
	terminalCalled := make(chan string, 1)
	repo := &fakeJobRepository{
		refreshErr:        ErrLeaseLost,
		refreshErrAfter:   2,
		refreshBlockAfter: 2,
		refreshEntered:    refreshEntered,
		refreshRelease:    refreshRelease,
		terminalCalled:    terminalCalled,
	}
	cfg := asyncConfig()
	cfg.Endpoints[0].InputLimit = MaxInputLimit
	runner := NewRunner(
		&fakeConfigStore{cfg: cfg, active: true},
		repo,
		&fakePayloadStore{values: map[int64]string{51: "heartbeat failure at completion boundary"}},
		PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			<-refreshEntered
			return integrationResult(EventPass), nil
		}),
		NewAtomicMetrics(),
	)
	runner.leaseHeartbeatInterval = 5 * time.Millisecond

	processDone := make(chan error, 1)
	go func() { processDone <- runner.processJob(context.Background(), 0, cfg, workerJob(1, 3)) }()
	<-refreshEntered
	close(refreshRelease)

	require.ErrorIs(t, <-processDone, ErrLeaseLost)
	select {
	case terminal := <-terminalCalled:
		t.Fatalf("heartbeat failure must not write terminal state, got %s", terminal)
	default:
	}
	require.Zero(t, repo.completeCount)
	require.Zero(t, repo.retried)
	require.Zero(t, repo.failed)
}

func TestWorkerHeartbeatFailureWithScannerPanicNeverWritesTerminalState(t *testing.T) {
	repo := &fakeJobRepository{
		refreshErr:      ErrLeaseLost,
		refreshErrAfter: 2,
	}
	cfg := asyncConfig()
	cfg.Endpoints[0].InputLimit = MaxInputLimit
	runner := NewRunner(
		&fakeConfigStore{cfg: cfg, active: true},
		repo,
		&fakePayloadStore{values: map[int64]string{51: "panic after lease loss"}},
		PromptScannerFunc(func(ctx context.Context, _ ActiveEndpoint, _ string, _ []string) (*NormalizedResult, error) {
			<-ctx.Done()
			panic("scanner panic after heartbeat failure")
		}),
		NewAtomicMetrics(),
	)
	runner.leaseHeartbeatInterval = 5 * time.Millisecond

	require.NotPanics(t, func() {
		runner.processSafely(context.Background(), 0, cfg, workerJob(1, 3))
	})
	require.Zero(t, repo.completeCount)
	require.Zero(t, repo.retried)
	require.Zero(t, repo.failed)
	_, _, failed, _, _, code, _ := runner.Snapshot()
	require.Equal(t, int64(1), failed)
	require.Equal(t, "lease_heartbeat_failed", code)
}

func TestWorkerLeaseHeartbeatFailureCancelsLongScannerWithoutTerminalTransition(t *testing.T) {
	scannerStarted := make(chan struct{})
	scannerCanceled := make(chan struct{})
	forceScannerReturn := make(chan struct{})
	repo := &fakeJobRepository{
		refreshErr:      ErrLeaseLost,
		refreshErrAfter: 2,
	}
	payload := &fakePayloadStore{values: map[int64]string{51: "long-running scanner input"}}
	runner := NewRunner(
		&fakeConfigStore{cfg: asyncConfig(), active: true},
		repo,
		payload,
		PromptScannerFunc(func(ctx context.Context, _ ActiveEndpoint, _ string, _ []string) (*NormalizedResult, error) {
			close(scannerStarted)
			select {
			case <-ctx.Done():
				close(scannerCanceled)
				return nil, ctx.Err()
			case <-forceScannerReturn:
				return integrationResult(EventPass), nil
			}
		}),
		NewAtomicMetrics(),
	)
	cfg := asyncConfig()
	cfg.Endpoints[0].InputLimit = MaxInputLimit
	configStore, ok := runner.config.(*fakeConfigStore)
	require.True(t, ok)
	configStore.Set(cfg, true)
	runner.leaseHeartbeatInterval = 10 * time.Millisecond

	processDone := make(chan error, 1)
	go func() { processDone <- runner.processJob(context.Background(), 0, cfg, workerJob(1, 3)) }()
	<-scannerStarted

	canceledByHeartbeat := false
	select {
	case <-scannerCanceled:
		canceledByHeartbeat = true
	case <-time.After(2 * time.Second):
		close(forceScannerReturn)
	}
	err := <-processDone

	require.True(t, canceledByHeartbeat, "an independent lease heartbeat must run while Scan is blocked")
	require.ErrorIs(t, err, ErrLeaseLost)
	require.GreaterOrEqual(t, repo.refreshes, 2)
	require.Zero(t, repo.completeCount, "a worker that lost its lease must not complete the job")
	require.Zero(t, repo.retried, "a worker that lost its lease must not retry the job")
	require.Zero(t, repo.failed, "a worker that lost its lease must not fail the job")
}

func TestPromptAuditSyntheticAsyncBaseline(t *testing.T) {
	const totalRequests = 100
	cfg := asyncConfig()
	cfg.Endpoints[0].InputLimit = 256
	cfg.StorePassEvents = false
	repo := &fakeJobRepository{}
	payload := &fakePayloadStore{values: make(map[int64]string, totalRequests)}
	metrics := NewAtomicMetrics()
	knownBenignFindings := 0
	knownMaliciousBlocked := 0
	scanner := PromptScannerFunc(func(_ context.Context, endpoint ActiveEndpoint, chunk string, _ []string) (*NormalizedResult, error) {
		switch {
		case strings.HasPrefix(chunk, "benign"):
			return &NormalizedResult{Decision: EventPass, RiskLevel: RiskLow, Action: ActionAllow, Safety: "Safe", GuardEndpointID: endpoint.ID}, nil
		case strings.HasPrefix(chunk, "flag"):
			return &NormalizedResult{Decision: EventFlag, RiskLevel: RiskMedium, Action: ActionWarn, Safety: "Controversial", Categories: []string{"politically_sensitive_topics"}, GuardEndpointID: endpoint.ID}, nil
		case strings.HasPrefix(chunk, "block"):
			knownMaliciousBlocked++
			return &NormalizedResult{Decision: EventCritical, RiskLevel: RiskCritical, Action: ActionBlock, Safety: "Unsafe", Categories: []string{"jailbreak"}, GuardEndpointID: endpoint.ID}, nil
		case strings.HasPrefix(chunk, "invalid"):
			return nil, &GuardError{Code: ErrorCodeInvalidResponse}
		default:
			return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true, Timeout: true}
		}
	})
	runner := NewRunner(&fakeConfigStore{cfg: cfg, active: true}, repo, payload, scanner, metrics)
	runner.clock = &advancingClock{now: time.Unix(1_000, 0).UTC(), step: time.Millisecond}

	for index := 1; index <= totalRequests; index++ {
		text := fmt.Sprintf("benign-%03d", index)
		switch {
		case index > 90 && index <= 95:
			text = fmt.Sprintf("flag-%03d", index)
		case index > 95 && index <= 98:
			text = fmt.Sprintf("block-%03d", index)
		case index == 99:
			text = "invalid-099"
		case index == 100:
			text = "timeout-100"
		}
		jobID := int64(index)
		payload.values[jobID] = text
		job := &Job{ID: jobID, ClaimVersion: 1, Attempts: 1, MaxAttempts: 1, ConfigVersion: cfg.ConfigVersion,
			Snapshot: PromptSnapshot{RequestID: fmt.Sprintf("baseline-%03d", index), PromptLength: len([]rune(text)), RedactedPreview: "synthetic"}}
		err := runner.processJob(context.Background(), 0, cfg, job)
		if index <= 98 {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}

	snapshot := metrics.Snapshot()
	require.Equal(t, int64(totalRequests), snapshot.Total)
	require.Equal(t, int64(90), snapshot.Allowed)
	require.Equal(t, int64(5), snapshot.Flagged)
	require.Equal(t, int64(3), snapshot.Blocked)
	require.Equal(t, int64(1), snapshot.Invalid)
	require.Equal(t, int64(1), snapshot.Unavailable)
	require.Equal(t, int64(1), snapshot.Timeouts)
	require.Zero(t, knownBenignFindings)
	require.Equal(t, 3, knownMaliciousBlocked)
	require.Equal(t, 98, repo.completeCount)
	require.Equal(t, 8, repo.eventCount, "store_pass_events=false only grows events for flag/block fixtures")
	require.Positive(t, snapshot.LatencyP50MS)
	require.LessOrEqual(t, snapshot.LatencyP50MS, snapshot.LatencyP95MS)
	require.LessOrEqual(t, snapshot.LatencyP95MS, snapshot.LatencyP99MS)
	t.Logf("synthetic async baseline: p50=%dms p95=%dms p99=%dms failure_rate=2%% false_positive_rate=0%% event_growth=8/100", snapshot.LatencyP50MS, snapshot.LatencyP95MS, snapshot.LatencyP99MS)
}

func TestRequestCloneOwnsMutableInputs(t *testing.T) {
	groupID := int64(7)
	req := Request{Body: []byte("original"), GroupID: &groupID}
	clone := req.Clone()
	clone.Body[0] = 'X'
	*clone.GroupID = 8
	require.Equal(t, []byte("original"), req.Body)
	require.Equal(t, int64(7), *req.GroupID)
	require.False(t, reflect.ValueOf(req.Body).Pointer() == reflect.ValueOf(clone.Body).Pointer())
}
