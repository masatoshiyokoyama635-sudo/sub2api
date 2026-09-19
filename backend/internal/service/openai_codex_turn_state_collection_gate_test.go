package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexCollectionTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *codexCollectionTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *codexCollectionTestClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

func newCodexCollectionTestGate() (*openAICodexTurnStateCollectionGate, *codexCollectionTestClock) {
	clock := &codexCollectionTestClock{now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	return &openAICodexTurnStateCollectionGate{now: clock.Now}, clock
}

type codexCollectionTestOutcome struct {
	result            codexTurnStateCollectionResult
	attempted, joined bool
}

func startCodexCollectionTestRun(gate *openAICodexTurnStateCollectionGate, ctx context.Context, id int64, key, hash string, run func(context.Context) codexTurnStateCollectionResult) <-chan codexCollectionTestOutcome {
	out := make(chan codexCollectionTestOutcome, 1)
	go func() {
		result, attempted, joined := gate.Run(ctx, id, key, hash, run)
		out <- codexCollectionTestOutcome{result: result, attempted: attempted, joined: joined}
	}()
	return out
}

func receiveCodexCollectionTestOutcome(t *testing.T, out <-chan codexCollectionTestOutcome) codexCollectionTestOutcome {
	t.Helper()
	select {
	case result := <-out:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("collection waiter did not finish")
		return codexCollectionTestOutcome{}
	}
}

func waitCodexCollectionTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("collection signal was not received")
	}
}

// Done is first consulted after Run chooses the in-flight entry. This provides
// a deterministic waiter barrier without sleeps or observing scheduler timing.
type codexCollectionWaitingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (ctx *codexCollectionWaitingContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestCodexTurnStateCollectionGateZeroValueAndCompletionCooldown(t *testing.T) {
	var zero openAICodexTurnStateCollectionGate
	var deadlineDuration time.Duration
	result, attempted, joined := zero.Run(context.Background(), 1, "model/proxy", "auth-a", func(ctx context.Context) codexTurnStateCollectionResult {
		deadline, _ := ctx.Deadline()
		deadlineDuration = time.Until(deadline)
		return codexTurnStateCollectionResult{Reason: "accepted", HTTPStatus: 200, ObservedLength: 332}
	})
	require.True(t, attempted)
	require.False(t, joined)
	require.Equal(t, "accepted", result.Reason)
	require.InDelta(t, 20, deadlineDuration.Seconds(), 1)
	require.WithinDuration(t, time.Now().Add(180*time.Second), result.NextAllowedAt, time.Second)

	gate, clock := newCodexCollectionTestGate()
	start := clock.Now()
	var calls int
	run := func(context.Context) codexTurnStateCollectionResult {
		calls++
		clock.Advance(10 * time.Second)
		return codexTurnStateCollectionResult{Reason: "accepted", HTTPStatus: 200}
	}
	result, attempted, joined = gate.Run(context.Background(), 7, "scope/model-a", "auth-a", run)
	require.True(t, attempted)
	require.False(t, joined)
	require.Equal(t, start.Add(190*time.Second), result.NextAllowedAt)
	require.Equal(t, 180*time.Second, result.RetryAfter)
	require.Equal(t, result.NextAllowedAt, gate.NextAllowedAt(7))
	clock.Advance(179 * time.Second)
	result, attempted, joined = gate.Run(context.Background(), 7, "other-proxy/model-b", "rotated-auth", run)
	require.False(t, attempted)
	require.False(t, joined)
	require.Equal(t, "cooldown", result.Reason)
	require.Zero(t, result.HTTPStatus, "a previous 200 is not a blocking cooldown response")
	require.Equal(t, time.Second, result.RetryAfter)
	require.Equal(t, 1, calls)
	clock.Advance(time.Second)
	require.True(t, gate.NextAllowedAt(7).IsZero())
	_, attempted, _ = gate.Run(context.Background(), 7, "other-proxy/model-b", "rotated-auth", run)
	require.True(t, attempted)
	require.Equal(t, 2, calls)
}

func TestCodexTurnStateCollectionGateBlockingStatusesRemainDuringCredentialCooldown(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			gate, clock := newCodexCollectionTestGate()
			var calls int
			result, attempted, _ := gate.Run(context.Background(), 7, "scope/model-a", "auth-a", func(context.Context) codexTurnStateCollectionResult {
				calls++
				return codexTurnStateCollectionResult{Reason: "upstream_rejected", HTTPStatus: status, RetryAfter: 10 * time.Minute}
			})
			require.True(t, attempted)
			wantCooldown := 180 * time.Second
			if status == 429 {
				wantCooldown = 10 * time.Minute
			}
			require.Equal(t, clock.Now().Add(wantCooldown), result.NextAllowedAt)
			clock.Advance(wantCooldown - time.Second)
			result, attempted, joined := gate.Run(context.Background(), 7, "scope/model-b", "new-token", func(context.Context) codexTurnStateCollectionResult {
				calls++
				return codexTurnStateCollectionResult{Reason: "unexpected"}
			})
			require.False(t, attempted)
			require.False(t, joined)
			require.Equal(t, "cooldown", result.Reason)
			if status == 500 {
				require.Zero(t, result.HTTPStatus)
			} else {
				require.Equal(t, status, result.HTTPStatus)
			}
			require.Equal(t, time.Second, result.RetryAfter)
			require.Equal(t, 1, calls, "no retry and token changes cannot bypass cooldown")
		})
	}
}

func TestCodexTurnStateCollectionGateJoinsAndCancelsEachWaiterIndependently(t *testing.T) {
	for _, cancelOwner := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelOwner), func(t *testing.T) {
			gate, _ := newCodexCollectionTestGate()
			ownerContext, cancelOwnerContext := context.WithCancel(context.Background())
			defer cancelOwnerContext()
			joinContext, cancelJoinContext := context.WithCancel(context.Background())
			defer cancelJoinContext()
			joinedContext := &codexCollectionWaitingContext{Context: joinContext, waiting: make(chan struct{})}
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			run := func(ctx context.Context) codexTurnStateCollectionResult {
				calls.Add(1)
				close(started)
				<-release
				if ctx.Err() != nil {
					return codexTurnStateCollectionResult{Reason: "unexpected_cancellation"}
				}
				return codexTurnStateCollectionResult{Reason: "accepted", HTTPStatus: 200}
			}
			owner := startCodexCollectionTestRun(gate, ownerContext, 7, "scope/model-a", "auth-a", run)
			waitCodexCollectionTestSignal(t, started)
			joiner := startCodexCollectionTestRun(gate, joinedContext, 7, "scope/model-a", "auth-a", run)
			waitCodexCollectionTestSignal(t, joinedContext.waiting)
			if cancelOwner {
				cancelOwnerContext()
				outcome := receiveCodexCollectionTestOutcome(t, owner)
				require.Equal(t, "context_cancelled", outcome.result.Reason)
				require.True(t, outcome.result.GenerationBlocked)
				require.True(t, outcome.attempted)
				require.False(t, outcome.joined)
				close(release)
				outcome = receiveCodexCollectionTestOutcome(t, joiner)
				require.Equal(t, "accepted", outcome.result.Reason)
				require.False(t, outcome.attempted)
				require.True(t, outcome.joined)
			} else {
				cancelJoinContext()
				outcome := receiveCodexCollectionTestOutcome(t, joiner)
				require.Equal(t, "context_cancelled", outcome.result.Reason)
				require.True(t, outcome.result.GenerationBlocked)
				require.False(t, outcome.attempted)
				require.True(t, outcome.joined)
				close(release)
				outcome = receiveCodexCollectionTestOutcome(t, owner)
				require.Equal(t, "accepted", outcome.result.Reason)
				require.True(t, outcome.attempted)
				require.False(t, outcome.joined)
			}
			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestCodexTurnStateCollectionGateCredentialAndGlobalConcurrency(t *testing.T) {
	gate, _ := newCodexCollectionTestGate()
	release := make(chan struct{})
	start := func(id int64) (<-chan codexCollectionTestOutcome, <-chan struct{}) {
		started := make(chan struct{})
		out := startCodexCollectionTestRun(gate, context.Background(), id, "scope/model-a", "auth-a", func(context.Context) codexTurnStateCollectionResult {
			close(started)
			<-release
			return codexTurnStateCollectionResult{Reason: "accepted"}
		})
		return out, started
	}
	one, firstStarted := start(7)
	waitCodexCollectionTestSignal(t, firstStarted)
	_, blocked := gate.BlockedResult(7)
	require.False(t, blocked, "running entries must still be joinable through Run")
	run := func(context.Context) codexTurnStateCollectionResult {
		return codexTurnStateCollectionResult{Reason: "accepted"}
	}
	for _, keyHash := range [][2]string{{"scope/model-b", "auth-a"}, {"scope/model-a", "auth-b"}} {
		result, attempted, joined := gate.Run(context.Background(), 7, keyHash[0], keyHash[1], run)
		require.Equal(t, "credential_busy", result.Reason)
		require.True(t, result.GenerationBlocked)
		require.False(t, attempted)
		require.False(t, joined)
	}
	two, secondStarted := start(8)
	waitCodexCollectionTestSignal(t, secondStarted)
	result, attempted, joined := gate.Run(context.Background(), 9, "scope/model-a", "auth-a", run)
	require.Equal(t, "global_busy", result.Reason)
	require.False(t, attempted)
	require.False(t, joined)
	require.True(t, gate.NextAllowedAt(9).IsZero(), "busy denial does not allocate a cooldown")
	close(release)
	receiveCodexCollectionTestOutcome(t, one)
	receiveCodexCollectionTestOutcome(t, two)
	_, attempted, _ = gate.Run(context.Background(), 9, "scope/model-a", "auth-a", run)
	require.True(t, attempted)
}

func TestCodexTurnStateCollectionGateBlockedResultOnlyReturnsUnexpiredRejections(t *testing.T) {
	for _, status := range []int{200, 401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			gate, clock := newCodexCollectionTestGate()
			_, blocked := gate.BlockedResult(7)
			require.False(t, blocked)
			require.Empty(t, gate.records, "read-only lookup must not allocate")
			_, attempted, _ := gate.Run(context.Background(), 7, "scope/model-a", "auth-a", func(context.Context) codexTurnStateCollectionResult {
				return codexTurnStateCollectionResult{Reason: "probe_result", HTTPStatus: status}
			})
			require.True(t, attempted)
			result, blocked := gate.BlockedResult(7)
			if status == 401 || status == 403 || status == 429 {
				require.True(t, blocked)
				require.Equal(t, status, result.HTTPStatus)
				require.Equal(t, "cooldown", result.Reason)
				require.Equal(t, clock.Now().Add(180*time.Second), result.NextAllowedAt)
				require.Equal(t, 180*time.Second, result.RetryAfter)
			} else {
				require.False(t, blocked, "ordinary completed failures retain generation fallback")
				require.Zero(t, result.HTTPStatus)
			}
			clock.Advance(180 * time.Second)
			_, blocked = gate.BlockedResult(7)
			require.False(t, blocked, "the exact deadline is expired")
			_, blocked = gate.BlockedResult(8)
			require.False(t, blocked)
			require.Len(t, gate.records, 1, "read-only lookup does not clean or allocate entries")
		})
	}
}

func TestCodexTurnStateCollectionGateTimeoutRetainsSlotUntilCallbackExits(t *testing.T) {
	gate, _ := newCodexCollectionTestGate()
	var cancelAttempt context.CancelFunc
	var timeout time.Duration
	gate.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		timeout = duration
		ctx, cancel := context.WithCancel(parent)
		cancelAttempt = cancel
		return ctx, cancel
	}
	started, release := make(chan struct{}), make(chan struct{})
	out := startCodexCollectionTestRun(gate, context.Background(), 7, "scope/model-a", "auth-a", func(context.Context) codexTurnStateCollectionResult {
		close(started)
		<-release // Simulates delayed cleanup after transport cancellation.
		return codexTurnStateCollectionResult{Reason: "transport_error"}
	})
	waitCodexCollectionTestSignal(t, started)
	require.Equal(t, 20*time.Second, timeout)
	cancelAttempt()
	outcome := receiveCodexCollectionTestOutcome(t, out)
	require.Equal(t, "context_cancelled", outcome.result.Reason)
	require.True(t, outcome.result.GenerationBlocked)
	require.True(t, outcome.attempted)
	gate.mu.Lock()
	require.Equal(t, 1, gate.active)
	entryDone := gate.records[7].done
	gate.mu.Unlock()
	close(release)
	waitCodexCollectionTestSignal(t, entryDone)
	gate.mu.Lock()
	require.Zero(t, gate.active)
	gate.mu.Unlock()
}

type codexCollectionDeadlineContext struct{ context.Context }

func (ctx codexCollectionDeadlineContext) Err() error {
	if ctx.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func TestCodexTurnStateCollectionGatePendingDeadlineBlocksGenerationUntilRejectionPublished(t *testing.T) {
	gate, _ := newCodexCollectionTestGate()
	var cancelAttempt context.CancelFunc
	gate.withTimeout = func(parent context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancelAttempt = cancel
		return codexCollectionDeadlineContext{ctx}, cancel
	}
	started, release := make(chan struct{}), make(chan struct{})
	out := startCodexCollectionTestRun(gate, context.Background(), 7, "scope/model-a", "auth-a", func(context.Context) codexTurnStateCollectionResult {
		close(started)
		<-release // The probe has a rejection, but its cleanup has not finished.
		return codexTurnStateCollectionResult{Reason: "forbidden", HTTPStatus: 403}
	})
	waitCodexCollectionTestSignal(t, started)
	cancelAttempt()
	result := receiveCodexCollectionTestOutcome(t, out).result
	require.Equal(t, "timeout", result.Reason)
	require.True(t, result.GenerationBlocked)
	require.Zero(t, result.HTTPStatus, "a local deadline must not invent an upstream status")
	gate.mu.Lock()
	entry := gate.records[7]
	require.Equal(t, 1, gate.active)
	gate.mu.Unlock()
	close(release)
	waitCodexCollectionTestSignal(t, entry.done)
	// The same pending-result path must prefer a completed known rejection.
	result = gate.pendingResult(entry, "timeout")
	require.Equal(t, "forbidden", result.Reason)
	require.Equal(t, 403, result.HTTPStatus)
	require.False(t, result.GenerationBlocked)
	result, attempted, joined := gate.Run(context.Background(), 7, "another-model/proxy", "rotated-token", func(context.Context) codexTurnStateCollectionResult {
		return codexTurnStateCollectionResult{Reason: "unexpected"}
	})
	require.False(t, attempted)
	require.False(t, joined)
	require.Equal(t, "cooldown", result.Reason)
	require.Equal(t, 403, result.HTTPStatus)
}

func TestCodexTurnStateCollectionGateCapacityDoesNotEvictCooldowns(t *testing.T) {
	gate, clock := newCodexCollectionTestGate()
	var calls int
	run := func(context.Context) codexTurnStateCollectionResult {
		calls++
		return codexTurnStateCollectionResult{Reason: "accepted"}
	}
	for id := int64(1); id <= codexTurnStateCollectionMaxRecords; id++ {
		_, attempted, _ := gate.Run(context.Background(), id, "scope/model-a", "auth-a", run)
		require.True(t, attempted)
	}
	result, attempted, _ := gate.Run(context.Background(), 2048, "scope/model-a", "auth-a", run)
	require.Equal(t, "capacity", result.Reason)
	require.False(t, attempted)
	require.Len(t, gate.records, 1024)
	result, attempted, _ = gate.Run(context.Background(), 1, "new-proxy/new-model", "new-token", run)
	require.Equal(t, "cooldown", result.Reason)
	require.False(t, attempted)
	require.Equal(t, 1024, calls)
	clock.Advance(180 * time.Second)
	_, attempted, _ = gate.Run(context.Background(), 2048, "scope/model-a", "auth-a", run)
	require.True(t, attempted)
	require.Len(t, gate.records, 1)
}

func TestCodexTurnStateCollectionGateRejectsInvalidAndCancelledInputs(t *testing.T) {
	gate, _ := newCodexCollectionTestGate()
	run := func(context.Context) codexTurnStateCollectionResult {
		return codexTurnStateCollectionResult{Reason: "unexpected"}
	}
	for _, input := range []struct {
		id        int64
		key, hash string
	}{
		{0, "scope", "auth"}, {1, "", "auth"}, {1, "scope", ""},
		{1, strings.Repeat("x", 513), "auth"}, {1, "scope", strings.Repeat("x", 129)},
	} {
		result, attempted, joined := gate.Run(context.Background(), input.id, input.key, input.hash, run)
		require.Equal(t, "invalid_request", result.Reason)
		require.False(t, attempted)
		require.False(t, joined)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, attempted, joined := gate.Run(ctx, 1, "scope", "auth", run)
	require.Equal(t, "context_cancelled", result.Reason)
	require.False(t, attempted)
	require.False(t, joined)
	require.Empty(t, gate.records)
	require.True(t, gate.NextAllowedAt(1).IsZero())
}

func TestCodexTurnStateCollectionGatePanicAndResultMetadataAreBounded(t *testing.T) {
	gate, _ := newCodexCollectionTestGate()
	result, attempted, _ := gate.Run(context.Background(), 7, "scope", "auth", func(context.Context) codexTurnStateCollectionResult {
		panic("private token must never enter the result")
	})
	require.True(t, attempted)
	require.Equal(t, "collection_failed", result.Reason)
	require.Empty(t, result.ResponseModel)
	require.Zero(t, gate.active)
	result, _, _ = gate.Run(context.Background(), 8, "scope", "auth", func(context.Context) codexTurnStateCollectionResult {
		return codexTurnStateCollectionResult{Reason: strings.Repeat("x", 1024), ResponseModel: "private@example.invalid\nBearer secret", HTTPStatus: -1, ObservedLength: -1}
	})
	require.Equal(t, "collection_failed", result.Reason)
	require.Empty(t, result.ResponseModel)
	require.Zero(t, result.HTTPStatus)
	require.Zero(t, result.ObservedLength)
}
