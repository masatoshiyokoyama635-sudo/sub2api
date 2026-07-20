package securityaudit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultLeaseHeartbeatInterval = 30 * time.Second
	leaseHeartbeatRefreshTimeout  = 5 * time.Second
)

type WorkerRuntime struct {
	active           atomic.Int64
	processed        atomic.Int64
	failed           atomic.Int64
	heartbeatNS      atomic.Int64
	lastProcessedNS  atomic.Int64
	lastErrorMu      sync.RWMutex
	lastErrorCode    string
	lastErrorMessage string
}

type Runner struct {
	config                 ConfigStore
	repo                   JobRepository
	payload                PayloadStore
	scanner                PromptScanner
	metrics                Metrics
	clock                  Clock
	leaseHeartbeatInterval time.Duration
	runtime                WorkerRuntime

	mu           sync.Mutex
	state        promptLifecycleState
	cancel       context.CancelFunc
	startupDone  chan struct{}
	shutdownDone chan struct{}
	shutdownErr  error
	wg           sync.WaitGroup
}

func NewRunner(config ConfigStore, repo JobRepository, payload PayloadStore, scanner PromptScanner, metrics Metrics) *Runner {
	return &Runner{
		config: config, repo: repo, payload: payload, scanner: scanner, metrics: metrics,
		clock: realClock{}, leaseHeartbeatInterval: defaultLeaseHeartbeatInterval,
	}
}

func (r *Runner) Start(ctx context.Context) error {
	if r == nil || r.config == nil || r.repo == nil || r.payload == nil || r.scanner == nil {
		return errors.New("prompt audit worker dependencies unavailable")
	}
	r.mu.Lock()
	switch r.state {
	case promptLifecycleStarting, promptLifecycleRunning:
		r.mu.Unlock()
		return nil
	case promptLifecycleStopping, promptLifecycleStopped:
		r.mu.Unlock()
		return ErrPromptAuditNotRestartable
	}
	runCtx, cancel := context.WithCancel(ctx)
	startupDone := make(chan struct{})
	r.state = promptLifecycleStarting
	r.cancel = cancel
	r.startupDone = startupDone
	r.shutdownDone = nil
	r.shutdownErr = nil
	r.mu.Unlock()

	if err := r.payload.Ping(runCtx); err != nil {
		r.setLastError("payload_store_unavailable", err.Error())
	}
	for workerID := 0; workerID < MaxWorkerCount; workerID++ {
		r.wg.Add(1)
		go r.worker(runCtx, workerID)
	}
	r.wg.Add(1)
	go r.reclaimer(runCtx)

	r.mu.Lock()
	if r.state == promptLifecycleStarting {
		r.state = promptLifecycleRunning
	}
	r.mu.Unlock()
	close(startupDone)
	return nil
}

func (r *Runner) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	switch r.state {
	case promptLifecycleNew:
		r.mu.Unlock()
		return nil
	case promptLifecycleStopped:
		shutdownErr := r.shutdownErr
		r.mu.Unlock()
		return shutdownErr
	case promptLifecycleStopping:
		shutdownDone := r.shutdownDone
		r.mu.Unlock()
		return r.waitForShutdown(ctx, shutdownDone)
	case promptLifecycleStarting, promptLifecycleRunning:
		r.state = promptLifecycleStopping
		cancel := r.cancel
		startupDone := r.startupDone
		shutdownDone := make(chan struct{})
		r.shutdownDone = shutdownDone
		r.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		go r.finishShutdown(startupDone, shutdownDone)
		return r.waitForShutdown(ctx, shutdownDone)
	default:
		r.mu.Unlock()
		return nil
	}
}

func (r *Runner) waitForShutdown(ctx context.Context, done <-chan struct{}) error {
	err := waitPromptLifecycle(ctx, done, func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.shutdownErr
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		LogWarn(EventProcessFailed, map[string]any{"status": "shutdown_timeout", "error_code": "worker_shutdown_timeout"})
	}
	return err
}

func (r *Runner) finishShutdown(startupDone, shutdownDone chan struct{}) {
	if startupDone != nil {
		<-startupDone
	}
	r.wg.Wait()
	r.mu.Lock()
	r.cancel = nil
	r.state = promptLifecycleStopped
	r.mu.Unlock()
	close(shutdownDone)
}

func (r *Runner) worker(ctx context.Context, workerID int) {
	defer r.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runtime.heartbeatNS.Store(r.clock.Now().UnixNano())
			cfg, ok := r.config.Active()
			if !ok || !cfg.RiskControlEnabled || !cfg.Enabled || workerID >= cfg.WorkerCount {
				continue
			}
			for {
				job, claimed, err := r.repo.ClaimNextJob(ctx, r.clock.Now())
				if err != nil {
					r.setLastError("claim_job_failed", err.Error())
					break
				}
				if !claimed {
					break
				}
				r.runtime.active.Add(1)
				r.processSafely(ctx, workerID, cfg, job)
				r.runtime.active.Add(-1)
			}
		}
	}
}

func (r *Runner) processSafely(ctx context.Context, workerID int, cfg ActiveConfig, job *Job) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.runtime.failed.Add(1)
			// Panic values may contain scanner response fragments or prompt data.
			// Keep only a stable generic message in runtime state and logs.
			r.setLastError("worker_panic", "worker panic recovered")
			_ = r.repo.Fail(ctx, job.ID, job.ClaimVersion, "worker_panic", "worker panic recovered")
			LogError(EventProcessFailed, mergeLogFields(jobLogFields(job), map[string]any{"worker_id": workerID, "status": "failed", "error_code": "worker_panic"}))
		}
	}()
	if err := r.processJob(ctx, workerID, cfg, job); err != nil {
		r.runtime.failed.Add(1)
	} else {
		r.runtime.processed.Add(1)
		r.runtime.lastProcessedNS.Store(r.clock.Now().UnixNano())
	}
}

type jobLeaseHeartbeat struct {
	cancelProcessing context.CancelFunc
	stopCh           chan struct{}
	done             chan struct{}
	stopOnce         sync.Once
	errMu            sync.Mutex
	err              error
}

func (h *jobLeaseHeartbeat) fail(err error) {
	h.errMu.Lock()
	h.err = err
	h.errMu.Unlock()
	h.cancelProcessing()
}

func (h *jobLeaseHeartbeat) stop() error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
	<-h.done
	return h.currentError()
}

func (h *jobLeaseHeartbeat) currentError() error {
	if h == nil {
		return nil
	}
	h.errMu.Lock()
	defer h.errMu.Unlock()
	return h.err
}

func (r *Runner) startLeaseHeartbeat(parent context.Context, job *Job) (context.Context, *jobLeaseHeartbeat) {
	processingCtx, cancelProcessing := context.WithCancel(parent)
	heartbeat := &jobLeaseHeartbeat{cancelProcessing: cancelProcessing, stopCh: make(chan struct{}), done: make(chan struct{})}
	interval := r.leaseHeartbeatInterval
	if interval <= 0 {
		interval = defaultLeaseHeartbeatInterval
	}
	go func() {
		defer close(heartbeat.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-parent.Done():
				return
			case <-heartbeat.stopCh:
				return
			case <-ticker.C:
				// Refresh is governed by the component lifetime, not the scanner's
				// failure-cancellation context. Normal quiesce waits for this bounded
				// operation to finish before any terminal transition.
				refreshCtx, refreshCancel := context.WithTimeout(parent, leaseHeartbeatRefreshTimeout)
				err := r.repo.RefreshLease(refreshCtx, job.ID, job.ClaimVersion, r.clock.Now())
				refreshCancel()
				if err == nil {
					continue
				}
				if parent.Err() != nil {
					return
				}
				r.setLastError("lease_heartbeat_failed", "")
				LogWarn(EventProcessFailed, mergeLogFields(jobLogFields(job), map[string]any{
					"status": "lease_lost", "error_code": "lease_heartbeat_failed",
				}))
				heartbeat.fail(&leaseHeartbeatError{cause: err})
				return
			}
		}
	}()
	return processingCtx, heartbeat
}

type leaseHeartbeatError struct{ cause error }

func (e *leaseHeartbeatError) Error() string { return "lease_heartbeat_failed" }
func (e *leaseHeartbeatError) Unwrap() error { return e.cause }

func (r *Runner) processJob(ctx context.Context, workerID int, cfg ActiveConfig, job *Job) (returnErr error) {
	if r == nil || r.config == nil || job == nil {
		return &GuardError{Code: ErrorCodeUnavailable}
	}
	// A job may remain queued while administrators rotate credentials or turn
	// async auditing off. Re-read the active snapshot immediately before any
	// scanner dispatch; the claim-time config is only a scheduling hint.
	current, ok := r.config.Active()
	if !ok || current.EffectiveMode() != ModeAsync {
		if job != nil {
			job.ConfigVersion = current.ConfigVersion
		}
		return r.finishFailure(ctx, job, &GuardError{Code: "audit_disabled", Retryable: false})
	}
	cfg = current
	job.ConfigVersion = cfg.ConfigVersion
	baseFields := jobLogFields(job)
	LogInfo(EventAuditStarted, mergeLogFields(baseFields, map[string]any{"worker_id": workerID, "attempts": job.Attempts, "status": "processing"}))
	scanText, err := r.payload.Get(ctx, job.ID)
	if err != nil {
		return r.finishFailure(ctx, job, &GuardError{Code: "payload_missing", Retryable: false, Cause: err})
	}
	// The job row only carries redacted metadata; the full prompt for the audit
	// event is reconstructed here from the transient scan payload.
	job.Snapshot.FullPrompt = FullPromptFromScanText(scanText)
	endpoints := cfg.EnabledEndpoints()
	if len(endpoints) == 0 {
		return r.finishFailure(ctx, job, &GuardError{Code: "no_enabled_endpoint", Retryable: true})
	}
	chunks := SplitRunes(scanText, minimumInputLimit(endpoints))
	results := make([]*NormalizedResult, 0, len(chunks))
	started := r.clock.Now()
	processingCtx, leaseHeartbeat := r.startLeaseHeartbeat(ctx, job)
	defer leaseHeartbeat.cancelProcessing()
	defer func() {
		if recovered := recover(); recovered != nil {
			heartbeatErr := leaseHeartbeat.stop()
			if heartbeatErr != nil {
				returnErr = heartbeatErr
				return
			}
			panic(recovered)
		}
		if heartbeatErr := leaseHeartbeat.stop(); heartbeatErr != nil {
			r.setLastError("lease_heartbeat_failed", "")
			returnErr = heartbeatErr
		}
	}()
	finishWithHeartbeat := func(failure error) error {
		if heartbeatErr := leaseHeartbeat.stop(); heartbeatErr != nil {
			return heartbeatErr
		}
		return r.finishFailure(ctx, job, failure)
	}
	for index, chunk := range chunks {
		if err := r.repo.RefreshLease(processingCtx, job.ID, job.ClaimVersion, r.clock.Now()); err != nil {
			return err
		}
		current, ok := r.config.Active()
		if !ok || current.EffectiveMode() != ModeAsync || !current.IncludesGroup(job.Snapshot.GroupID) || current.ConfigVersion != cfg.ConfigVersion {
			return finishWithHeartbeat(&GuardError{Code: "audit_config_changed", Retryable: false})
		}
		currentEndpoints := current.EnabledEndpoints()
		if len(currentEndpoints) == 0 {
			return finishWithHeartbeat(&GuardError{Code: "no_enabled_endpoint", Retryable: false})
		}
		chunkStarted := r.clock.Now()
		LogInfo(EventChunkStarted, mergeLogFields(baseFields, map[string]any{"worker_id": workerID, "chunk_index": index + 1, "chunk_total": len(chunks), "chunk_chars": len([]rune(chunk)), "input_chars": job.Snapshot.PromptLength, "input_limit": minimumInputLimit(currentEndpoints), "status": "started"}))
		result, scanErr := scanWithFailover(processingCtx, r.scanner, current.Scanners, currentEndpoints, chunk, r.metrics)
		if heartbeatErr := leaseHeartbeat.currentError(); heartbeatErr != nil {
			return heartbeatErr
		}
		if scanErr != nil {
			LogWarn(EventChunkFailed, mergeLogFields(baseFields, map[string]any{
				"worker_id": workerID, "chunk_index": index + 1, "chunk_total": len(chunks),
				"chunk_chars": len([]rune(chunk)), "input_chars": job.Snapshot.PromptLength,
				"input_limit": minimumInputLimit(endpoints), "latency_ms": r.clock.Now().Sub(chunkStarted).Milliseconds(),
				"error_code": guardErrorCode(scanErr), "status": "failed",
			}))
			r.observeAsyncFailure(scanErr, r.clock.Now().Sub(started))
			return finishWithHeartbeat(scanErr)
		}
		results = append(results, result)
		LogInfo(EventChunkCompleted, mergeLogFields(baseFields, map[string]any{"worker_id": workerID, "chunk_index": index + 1, "chunk_total": len(chunks), "guard_endpoint_id": result.GuardEndpointID, "action": result.Action, "latency_ms": r.clock.Now().Sub(chunkStarted).Milliseconds(), "status": "completed"}))
		if result.Action == ActionBlock {
			break
		}
	}
	aggregated, err := AggregateResults(results, r.clock.Now().Sub(started))
	if err != nil {
		if r.metrics != nil {
			r.metrics.Observe(DecisionInvalid, r.clock.Now().Sub(started))
		}
		return finishWithHeartbeat(&GuardError{Code: ErrorCodeInvalidResponse, Cause: err})
	}
	aggregated.ChunkTotal = len(chunks)
	if r.metrics != nil {
		r.metrics.Observe(decisionKindForResult(aggregated), r.clock.Now().Sub(started))
	}
	LogInfo(EventChunksAggregated, mergeLogFields(baseFields, map[string]any{
		"worker_id": workerID, "decision": aggregated.Decision, "risk_level": aggregated.RiskLevel,
		"action": aggregated.Action, "chunk_total": aggregated.ChunkTotal,
		"latency_ms": aggregated.LatencyMS, "guard_endpoint_id": aggregated.GuardEndpointID, "status": "completed",
	}))
	if heartbeatErr := leaseHeartbeat.stop(); heartbeatErr != nil {
		return heartbeatErr
	}
	event, err := r.repo.Complete(ctx, job, aggregated, cfg.StorePassEvents)
	if err != nil {
		return err
	}
	if deleteErr := r.payload.Delete(ctx, job.ID); deleteErr != nil {
		LogWarn(EventProcessFailed, mergeLogFields(baseFields, map[string]any{"worker_id": workerID, "status": "payload_delete_deferred", "error_code": "payload_delete_failed"}))
	}
	LogInfo(EventProcessed, mergeLogFields(baseFields, map[string]any{"worker_id": workerID, "event_id": eventID(event), "decision": aggregated.Decision, "risk_level": aggregated.RiskLevel, "action": aggregated.Action, "guard_endpoint_id": aggregated.GuardEndpointID, "latency_ms": aggregated.LatencyMS, "status": "done"}))
	if event != nil && aggregated.Decision != EventPass {
		LogWarn(EventFindingRecorded, mergeLogFields(baseFields, map[string]any{"worker_id": workerID, "event_id": event.ID, "decision": aggregated.Decision, "risk_level": aggregated.RiskLevel, "action": aggregated.Action, "guard_endpoint_id": aggregated.GuardEndpointID, "status": "recorded"}))
	}
	return nil
}

func (r *Runner) observeAsyncFailure(err error, latency time.Duration) {
	if r == nil || r.metrics == nil {
		return
	}
	kind := DecisionUnavailable
	if guardErrorCode(err) == ErrorCodeInvalidResponse {
		kind = DecisionInvalid
	}
	r.metrics.Observe(kind, latency)
	var guardErr *GuardError
	if errors.As(err, &guardErr) && guardErr.Timeout {
		r.metrics.IncTimeout()
	}
}

func decisionKindForResult(result *NormalizedResult) DecisionKind {
	if result == nil {
		return DecisionInvalid
	}
	switch result.Action {
	case ActionBlock:
		return DecisionBlock
	case ActionWarn:
		return DecisionFlag
	default:
		return DecisionAllow
	}
}

func (r *Runner) finishFailure(ctx context.Context, job *Job, err error) error {
	baseFields := jobLogFields(job)
	code := guardErrorCode(err)
	retryable := false
	var guardErr *GuardError
	if errors.As(err, &guardErr) {
		retryable = guardErr.Retryable
	}
	if retryable && job.Attempts < job.MaxAttempts {
		next := r.clock.Now().Add(retryBackoff(job.Attempts))
		if updateErr := r.repo.Retry(ctx, job.ID, job.ClaimVersion, next, code, "prompt guard temporarily unavailable"); updateErr != nil {
			return updateErr
		}
		LogWarn(EventProcessFailed, mergeLogFields(baseFields, map[string]any{"attempts": job.Attempts, "max_attempts": job.MaxAttempts, "status": "retry", "error_code": code, "retryable": true}))
	} else {
		if updateErr := r.repo.Fail(ctx, job.ID, job.ClaimVersion, code, "prompt guard processing failed"); updateErr != nil {
			return updateErr
		}
		_ = r.payload.Delete(ctx, job.ID)
		LogError(EventProcessFailed, mergeLogFields(baseFields, map[string]any{"attempts": job.Attempts, "max_attempts": job.MaxAttempts, "status": "failed", "error_code": code, "retryable": false}))
	}
	r.setLastError(code, err.Error())
	return err
}

func (r *Runner) reclaimer(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := r.clock.Now()
			if _, err := r.repo.CleanupTerminalJobs(ctx, now.Add(-24*time.Hour), 100); err != nil {
				r.setLastError("terminal_cleanup_failed", "")
				LogWarn(EventProcessFailed, map[string]any{"status": "cleanup_failed", "error_code": "terminal_cleanup_failed"})
			}
			count, err := r.repo.ReclaimStale(ctx, now.Add(-2*time.Minute), now.Add(-90*time.Second), 100)
			if err != nil {
				r.setLastError("reclaim_failed", err.Error())
				continue
			}
			if count > 0 {
				LogWarn(EventProcessingReclaimed, map[string]any{"reclaimed_total": count, "status": "reclaimed"})
			}
		}
	}
}

func (r *Runner) Snapshot() (active, processed, failed int64, heartbeat, lastProcessed *time.Time, code, message string) {
	if r == nil {
		return
	}
	active, processed, failed = r.runtime.active.Load(), r.runtime.processed.Load(), r.runtime.failed.Load()
	if ns := r.runtime.heartbeatNS.Load(); ns > 0 {
		value := time.Unix(0, ns).UTC()
		heartbeat = &value
	}
	if ns := r.runtime.lastProcessedNS.Load(); ns > 0 {
		value := time.Unix(0, ns).UTC()
		lastProcessed = &value
	}
	r.runtime.lastErrorMu.RLock()
	code, message = r.runtime.lastErrorCode, r.runtime.lastErrorMessage
	r.runtime.lastErrorMu.RUnlock()
	return
}

func (r *Runner) setLastError(code, _ string) {
	code, message := sanitizeStoredError(code)
	r.runtime.lastErrorMu.Lock()
	r.runtime.lastErrorCode = code
	r.runtime.lastErrorMessage = message
	r.runtime.lastErrorMu.Unlock()
}

func scanWithFailover(ctx context.Context, scanner PromptScanner, scanners []string, endpoints []ActiveEndpoint, chunk string, metrics Metrics) (*NormalizedResult, error) {
	var lastErr error
	for index, endpoint := range endpoints {
		result, err := scanner.Scan(ctx, endpoint, chunk, scanners)
		if err == nil && result != nil {
			return result, nil
		}
		if err == nil {
			err = &GuardError{Code: ErrorCodeInvalidResponse, Retryable: false}
		}
		lastErr = err
		var guardErr *GuardError
		if !errors.As(err, &guardErr) || !guardErr.Retryable {
			return nil, err
		}
		if index < len(endpoints)-1 && metrics != nil {
			metrics.IncFailover()
		}
	}
	if lastErr == nil {
		lastErr = &GuardError{Code: ErrorCodeUnavailable}
	}
	return nil, lastErr
}

func retryBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 5 * time.Second
	case 2:
		return 30 * time.Second
	default:
		return 2 * time.Minute
	}
}

func eventID(event *Event) int64 {
	if event == nil {
		return 0
	}
	return event.ID
}
