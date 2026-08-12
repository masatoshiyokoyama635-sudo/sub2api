package securityaudit

import (
	"context"
	"errors"
	"time"
)

const enqueueCleanupTimeout = 2 * time.Second

type enqueueFailureError struct {
	code  string
	cause error
}

func (e *enqueueFailureError) Error() string {
	if e == nil {
		return "prompt_audit_enqueue_failed"
	}
	return stableErrorCode(e.code)
}

func (e *enqueueFailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type Enqueuer struct {
	config  ConfigStore
	repo    JobRepository
	payload PayloadStore
	metrics Metrics
}

type enqueuePreparation struct {
	config    ActiveConfig
	logFields map[string]any
}

func NewEnqueuer(config ConfigStore, repo JobRepository, payload PayloadStore, metrics ...Metrics) *Enqueuer {
	var metric Metrics
	if len(metrics) > 0 {
		metric = metrics[0]
	}
	return &Enqueuer{config: config, repo: repo, payload: payload, metrics: metric}
}

func (e *Enqueuer) Enqueue(ctx context.Context, req Request) error {
	preparation, ready, err := e.prepare(req)
	if err != nil || !ready {
		return err
	}
	snapshot, err := ExtractPromptSnapshot(req)
	if err != nil {
		e.recordSnapshotDrop(preparation.logFields, err)
		return nil
	}
	return e.enqueuePrepared(ctx, preparation, snapshot)
}

func (e *Enqueuer) prepare(req Request) (enqueuePreparation, bool, error) {
	if e == nil || e.config == nil || e.repo == nil || e.payload == nil {
		return enqueuePreparation{}, false, errors.New("prompt audit enqueuer unavailable")
	}
	cfg, ok := e.config.Active()
	baseFields := requestLogFields(req)
	if !ok || cfg.EffectiveMode() != ModeAsync {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "mode_not_async"}))
		return enqueuePreparation{}, false, nil
	}
	baseFields["config_version"] = cfg.ConfigVersion
	if !cfg.IncludesGroup(req.GroupID) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "group_out_of_scope"}))
		return enqueuePreparation{}, false, nil
	}
	if len(cfg.EnabledEndpoints()) == 0 {
		e.recordDropped()
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{"status": "dropped", "error_code": "no_enabled_endpoint"}))
		return enqueuePreparation{}, false, nil
	}
	return enqueuePreparation{config: cfg, logFields: baseFields}, true, nil
}

func (e *Enqueuer) recordSnapshotDrop(baseFields map[string]any, err error) {
	if errors.Is(err, ErrNoPromptText) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "no_user_text"}))
		return
	}
	e.recordDropped()
	code := "snapshot_invalid"
	if errors.Is(err, ErrPromptAuditPayloadTooLarge) {
		code = ErrorCodePayloadTooLarge
	}
	LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{"status": "dropped", "error_code": code}))
}

func (e *Enqueuer) enqueuePrepared(ctx context.Context, preparation enqueuePreparation, snapshot PromptSnapshot) error {
	if len(snapshot.ScanText) > MaxPromptAuditPayloadBytes {
		e.recordSnapshotDrop(preparation.logFields, newPromptAuditPayloadTooLargeError())
		return nil
	}
	cfg := preparation.config
	baseFields := preparation.logFields
	job, err := e.repo.CreateStagingWithCapacity(ctx, snapshot.Redacted(), cfg.ConfigVersion, 3, cfg.QueueCapacity)
	if err != nil {
		code := "database_unavailable"
		if errors.Is(err, ErrQueueFull) {
			code = "queue_full"
		}
		if errors.Is(err, ErrQueueAdmissionBusy) {
			code = "queue_admission_busy"
		}
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{
			"queue_capacity": cfg.QueueCapacity, "status": "dropped", "error_code": code,
		}))
		e.recordDropped()
		return err
	}
	if err := e.payload.Set(ctx, job.ID, snapshot.ScanText, DefaultPayloadTTL); err != nil {
		deleteErr := e.deletePayload(ctx, job.ID)
		markErr := e.markStagingFailed(ctx, job.ID, "payload_store_failed")
		e.recordEnqueueFailure(baseFields, job.ID, "payload_store_failed", deleteErr != nil || markErr != nil)
		return newEnqueueFailure("payload_store_failed", err, deleteErr, markErr)
	}
	if publishErr := e.repo.PublishQueued(ctx, job.ID); publishErr != nil {
		accepted, failed, reconcileErr := e.reconcilePublishOutcome(ctx, job.ID)
		if accepted {
			e.recordEnqueued(baseFields, job.ID, cfg.QueueCapacity, true)
			return nil
		}
		if !failed {
			e.recordEnqueueFailure(baseFields, job.ID, "queue_publish_unconfirmed", reconcileErr != nil)
			return newEnqueueFailure("queue_publish_unconfirmed", publishErr, reconcileErr)
		}
		deleteErr := e.deletePayload(ctx, job.ID)
		e.recordEnqueueFailure(baseFields, job.ID, "queue_publish_failed", deleteErr != nil || reconcileErr != nil)
		return newEnqueueFailure("queue_publish_failed", publishErr, reconcileErr, deleteErr)
	}
	e.recordEnqueued(baseFields, job.ID, cfg.QueueCapacity, false)
	return nil
}

func (e *Enqueuer) recordEnqueued(baseFields map[string]any, jobID int64, capacity int, reconciled bool) {
	fields := map[string]any{
		"job_id": jobID, "queue_capacity": capacity, "status": "queued",
	}
	if reconciled {
		fields["outcome"] = "reconciled"
	}
	LogInfo(EventJobEnqueued, mergeLogFields(baseFields, fields))
	if e.metrics != nil {
		e.metrics.IncEnqueued()
	}
}

func (e *Enqueuer) reconcilePublishOutcome(parent context.Context, jobID int64) (accepted, failed bool, reconcileErr error) {
	reconcileCtx, cancel := enqueueCleanupContext(parent)
	defer cancel()
	status, err := e.repo.JobStatus(reconcileCtx, jobID)
	if err != nil {
		return false, false, err
	}
	if publishStatusAccepted(status) {
		return true, false, nil
	}
	if status == "failed" {
		return false, true, nil
	}
	if status != "staging" {
		return false, false, errors.New("prompt audit publish outcome unknown")
	}
	markErr := e.repo.MarkStagingFailed(reconcileCtx, jobID, "queue_publish_failed", stableErrorMessage("queue_publish_failed"))
	if markErr == nil {
		return false, true, nil
	}
	// MarkStagingFailed can itself have an ambiguous transport outcome. Re-read
	// the row before deciding whether the payload is safe to remove.
	status, statusErr := e.repo.JobStatus(reconcileCtx, jobID)
	if statusErr != nil {
		return false, false, errors.Join(markErr, statusErr)
	}
	if publishStatusAccepted(status) {
		return true, false, markErr
	}
	if status == "failed" {
		return false, true, markErr
	}
	return false, false, errors.Join(markErr, errors.New("prompt audit publish outcome unknown"))
}

func publishStatusAccepted(status string) bool {
	switch status {
	case "queued", "processing", "retry", "done":
		return true
	default:
		return false
	}
}

func (e *Enqueuer) deletePayload(parent context.Context, jobID int64) error {
	cleanupCtx, cancel := enqueueCleanupContext(parent)
	defer cancel()
	return e.payload.Delete(cleanupCtx, jobID)
}

func (e *Enqueuer) markStagingFailed(parent context.Context, jobID int64, code string) error {
	cleanupCtx, cancel := enqueueCleanupContext(parent)
	defer cancel()
	return e.repo.MarkStagingFailed(cleanupCtx, jobID, code, stableErrorMessage(code))
}

func enqueueCleanupContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), enqueueCleanupTimeout)
}

func (e *Enqueuer) recordEnqueueFailure(baseFields map[string]any, jobID int64, code string, cleanupFailed bool) {
	fields := map[string]any{
		"job_id": jobID, "status": "dropped", "error_code": code,
	}
	if cleanupFailed {
		fields["error_kind"] = code + "_cleanup_failed"
	}
	LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, fields))
	e.recordDropped()
}

func newEnqueueFailure(code string, errs ...error) error {
	return &enqueueFailureError{code: code, cause: errors.Join(errs...)}
}

func (e *Enqueuer) recordDropped() {
	if e != nil && e.metrics != nil {
		e.metrics.IncDropped()
	}
}
