package service

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	codexTurnStateCollectionTimeout       = 20 * time.Second
	codexTurnStateCollectionCooldown      = 180 * time.Second
	codexTurnStateCollectionMaxConcurrent = 2
	codexTurnStateCollectionMaxRecords    = 1024
	codexTurnStateCollectionMaxKeyBytes   = 512
	codexTurnStateCollectionMaxHashBytes  = 128
)

// codexTurnStateCollectionResult contains only bounded diagnostics. The caller
// writes any harvested state directly into the credential-scoped candidate cache.
type codexTurnStateCollectionResult struct {
	Reason         string
	HTTPStatus     int
	ObservedLength int
	ResponseModel  string
	RetryAfter     time.Duration
	NextAllowedAt  time.Time
	// Pending collection cannot authorize a generation fallback: its eventual
	// response may already contain an access/rate rejection not yet published.
	GenerationBlocked bool
}

type codexTurnStateCollectionEntry struct {
	key            string
	credentialHash string
	nextAllowedAt  time.Time
	running        bool
	context        context.Context
	done           chan struct{}
	result         codexTurnStateCollectionResult
}

// openAICodexTurnStateCollectionGate is zero-value-ready. Budgets belong to the
// parent credential ID, so changing model, proxy scope, or token cannot bypass
// the minimum collection interval. Only an exact in-flight key/hash can join.
type openAICodexTurnStateCollectionGate struct {
	mu      sync.Mutex
	records map[int64]*codexTurnStateCollectionEntry
	active  int

	// Tests inject a clock and timeout factory without changing production limits.
	now         func() time.Time
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

func (gate *openAICodexTurnStateCollectionGate) currentTime() time.Time {
	if gate.now != nil {
		return gate.now()
	}
	return time.Now()
}

// Run starts at most one callback, without retrying it. attempted identifies the
// caller that started it; joined identifies an exact-key in-flight waiter. Each
// waiter may cancel independently. Cancellation does not abandon a concurrency
// slot while the callback still runs: callbacks must honor the supplied context.
func (gate *openAICodexTurnStateCollectionGate) Run(ctx context.Context, credentialID int64, scopeModelKey, credentialHash string, run func(context.Context) codexTurnStateCollectionResult) (result codexTurnStateCollectionResult, attempted, joined bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return codexTurnStateCollectionResult{Reason: "context_cancelled"}, false, false
	}
	if gate == nil || credentialID <= 0 || strings.TrimSpace(scopeModelKey) == "" || len(scopeModelKey) > codexTurnStateCollectionMaxKeyBytes ||
		strings.TrimSpace(credentialHash) == "" || len(credentialHash) > codexTurnStateCollectionMaxHashBytes || run == nil {
		return codexTurnStateCollectionResult{Reason: "invalid_request"}, false, false
	}
	gate.mu.Lock()
	if ctx.Err() != nil {
		gate.mu.Unlock()
		return codexTurnStateCollectionResult{Reason: "context_cancelled"}, false, false
	}
	now := gate.currentTime()
	if entry := gate.records[credentialID]; entry != nil {
		if entry.running {
			if entry.key == scopeModelKey && entry.credentialHash == credentialHash {
				gate.mu.Unlock()
				return gate.wait(ctx, entry), false, true
			}
			retryAfter := remainingCollectionCooldown(entry.nextAllowedAt, now)
			nextAllowedAt := entry.nextAllowedAt
			gate.mu.Unlock()
			return codexTurnStateCollectionResult{Reason: "credential_busy", RetryAfter: retryAfter, NextAllowedAt: nextAllowedAt, GenerationBlocked: true}, false, false
		}
		if now.Before(entry.nextAllowedAt) {
			result = entry.result
			result.Reason = "cooldown"
			result.RetryAfter = entry.nextAllowedAt.Sub(now)
			result.NextAllowedAt = entry.nextAllowedAt
			if result.HTTPStatus != 401 && result.HTTPStatus != 403 && result.HTTPStatus != 429 {
				result.HTTPStatus = 0
			}
			gate.mu.Unlock()
			return result, false, false
		}
	}
	// Never evict a live or unexpired record merely to make room. Otherwise a
	// stream of new keys could erase the credential's rate limit.
	for id, entry := range gate.records {
		if !entry.running && !now.Before(entry.nextAllowedAt) {
			delete(gate.records, id)
		}
	}
	if len(gate.records) >= codexTurnStateCollectionMaxRecords {
		gate.mu.Unlock()
		return codexTurnStateCollectionResult{Reason: "capacity"}, false, false
	}
	if gate.active >= codexTurnStateCollectionMaxConcurrent {
		gate.mu.Unlock()
		return codexTurnStateCollectionResult{Reason: "global_busy"}, false, false
	}
	if gate.records == nil {
		gate.records = make(map[int64]*codexTurnStateCollectionEntry)
	}
	withTimeout := gate.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	// Preserve context values, but do not let the first waiter cancel the shared
	// collection for other callers. The independent attempt has its own deadline.
	attemptContext, cancel := withTimeout(context.WithoutCancel(ctx), codexTurnStateCollectionTimeout)
	entry := &codexTurnStateCollectionEntry{
		key: scopeModelKey, credentialHash: credentialHash,
		nextAllowedAt: now.Add(codexTurnStateCollectionCooldown), running: true,
		context: attemptContext, done: make(chan struct{}),
	}
	gate.records[credentialID] = entry
	gate.active++
	gate.mu.Unlock()
	go gate.execute(entry, cancel, run)
	return gate.wait(ctx, entry), true, false
}

func remainingCollectionCooldown(deadline, now time.Time) time.Duration {
	if now.Before(deadline) {
		return deadline.Sub(now)
	}
	return 0
}

// NextAllowedAt reports the credential's current unexpired cooldown without
// creating a record. The eventual completion of a running attempt extends it.
func (gate *openAICodexTurnStateCollectionGate) NextAllowedAt(credentialID int64) time.Time {
	if gate == nil {
		return time.Time{}
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if entry := gate.records[credentialID]; entry != nil && gate.currentTime().Before(entry.nextAllowedAt) {
		return entry.nextAllowedAt
	}
	return time.Time{}
}

// BlockedResult protects generation even when a different model already has a
// cached candidate. Running collections remain joinable through Run; only a
// completed, unexpired authentication/access/rate rejection blocks here.
func (gate *openAICodexTurnStateCollectionGate) BlockedResult(credentialID int64) (codexTurnStateCollectionResult, bool) {
	if gate == nil {
		return codexTurnStateCollectionResult{}, false
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	entry := gate.records[credentialID]
	now := gate.currentTime()
	if entry == nil || entry.running || !now.Before(entry.nextAllowedAt) {
		return codexTurnStateCollectionResult{}, false
	}
	result := entry.result
	if result.HTTPStatus != 401 && result.HTTPStatus != 403 && result.HTTPStatus != 429 {
		return codexTurnStateCollectionResult{}, false
	}
	result.Reason = "cooldown"
	result.NextAllowedAt = entry.nextAllowedAt
	result.RetryAfter = entry.nextAllowedAt.Sub(now)
	return result, true
}

func (gate *openAICodexTurnStateCollectionGate) resultAtCurrentCooldown(entry *codexTurnStateCollectionEntry, result codexTurnStateCollectionResult) codexTurnStateCollectionResult {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	result.NextAllowedAt = entry.nextAllowedAt
	result.RetryAfter = remainingCollectionCooldown(entry.nextAllowedAt, gate.currentTime())
	return result
}

func (gate *openAICodexTurnStateCollectionGate) pendingResult(entry *codexTurnStateCollectionEntry, reason string) codexTurnStateCollectionResult {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	result := codexTurnStateCollectionResult{Reason: reason, GenerationBlocked: true}
	// Completion may race the select. Preserve its known rejection instead of
	// replacing it with an ambiguous cancellation or timeout.
	if !entry.running {
		result = entry.result
	}
	result.NextAllowedAt = entry.nextAllowedAt
	result.RetryAfter = remainingCollectionCooldown(entry.nextAllowedAt, gate.currentTime())
	return result
}

func (gate *openAICodexTurnStateCollectionGate) wait(ctx context.Context, entry *codexTurnStateCollectionEntry) codexTurnStateCollectionResult {
	select {
	case <-ctx.Done():
		return gate.pendingResult(entry, "context_cancelled")
	case <-entry.done:
		return gate.resultAtCurrentCooldown(entry, entry.result)
	case <-entry.context.Done():
		reason := "context_cancelled"
		if entry.context.Err() == context.DeadlineExceeded {
			reason = "timeout"
		}
		return gate.pendingResult(entry, reason)
	}
}

func (gate *openAICodexTurnStateCollectionGate) execute(entry *codexTurnStateCollectionEntry, cancel context.CancelFunc, run func(context.Context) codexTurnStateCollectionResult) {
	defer cancel()
	result := codexTurnStateCollectionResult{Reason: "collection_failed"}
	// A callback panic must not escape this goroutine or leak a concurrency slot.
	// Keep the diagnostic fixed instead of exposing panic contents.
	func() {
		defer func() { _ = recover() }()
		result = normalizeCodexTurnStateCollectionResult(run(entry.context))
	}()
	gate.mu.Lock()
	now := gate.currentTime()
	cooldown := codexTurnStateCollectionCooldown
	if result.HTTPStatus == 429 && result.RetryAfter > cooldown {
		cooldown = result.RetryAfter
	}
	if deadline := now.Add(cooldown); deadline.After(entry.nextAllowedAt) {
		entry.nextAllowedAt = deadline
	}
	result.RetryAfter = remainingCollectionCooldown(entry.nextAllowedAt, now)
	result.NextAllowedAt = entry.nextAllowedAt
	entry.result = result
	entry.running = false
	gate.active--
	close(entry.done)
	gate.mu.Unlock()
}

func normalizeCodexTurnStateCollectionResult(result codexTurnStateCollectionResult) codexTurnStateCollectionResult {
	validReason := result.Reason != "" && len(result.Reason) <= 64
	for _, char := range result.Reason {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			validReason = false
			break
		}
	}
	if !validReason {
		result.Reason = "collection_failed"
	}
	validModel := len(result.ResponseModel) <= 128
	for _, char := range result.ResponseModel {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.:/", char) {
			continue
		}
		validModel = false
		break
	}
	if !validModel {
		result.ResponseModel = ""
	}
	if result.HTTPStatus < 100 || result.HTTPStatus > 599 {
		result.HTTPStatus = 0
	}
	if result.ObservedLength < 0 {
		result.ObservedLength = 0
	}
	if result.RetryAfter < 0 {
		result.RetryAfter = 0
	}
	return result
}
