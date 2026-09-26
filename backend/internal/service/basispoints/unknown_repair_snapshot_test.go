package basispoints

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func reviewUnknownRepairBridge(t *testing.T) (*Bridge, string) {
	t.Helper()
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "shell", "parameters": object{"type": "object"}}}
	_, bridge := mustPrepare(t, source, "review-unknown-repair", new(ReplayCache))
	original := object{"id": "resp_original", "status": "completed", "usage": object{"input_tokens": 10, "output_tokens": 2, "input_tokens_details": object{"cached_tokens": 3}}, "output": []any{nativeCall(object{"name": "missing", "arguments": object{}})}}
	return bridge, sse(object{"type": "response.completed", "response": original})
}

func reviewUnknownProgressiveUsage() string {
	return sse(object{"type": "response.in_progress", "response": object{"usage": object{"input_tokens": 7, "output_tokens": 1, "input_tokens_details": object{"cached_tokens": 2}}}}) +
		sse(object{"type": "response.in_progress", "response": object{"usage": object{"input_tokens": 7, "output_tokens": 2, "input_tokens_details": object{"cached_tokens": 2}}}})
}

func reviewAssertUsageSnapshots(t *testing.T, body io.ReadCloser, corrected bool) {
	t.Helper()
	snapshotter, ok := body.(UsageSnapshotter)
	require.True(t, ok)
	attempts := snapshotter.UsageAttempts()
	want := 1
	if corrected {
		want = 2
	}
	require.Len(t, attempts, want, "one snapshot per native attempt, not cumulative or progressive charges")
	require.Equal(t, json.Number("10"), attempts[0]["input_tokens"])
	require.Equal(t, json.Number("2"), attempts[0]["output_tokens"])
	details, ok := attempts[0]["input_tokens_details"].(object)
	require.True(t, ok)
	require.Equal(t, json.Number("3"), details["cached_tokens"])
	if corrected {
		require.Equal(t, json.Number("7"), attempts[1]["input_tokens"])
		require.Equal(t, json.Number("2"), attempts[1]["output_tokens"])
		details, ok = attempts[1]["input_tokens_details"].(object)
		require.True(t, ok)
		require.Equal(t, json.Number("2"), details["cached_tokens"])
	}
}

func TestUnknownRepairUsageSnapshotsRemainPerAttempt(t *testing.T) {
	for _, outcome := range []string{"completed", "failed", "disconnected"} {
		t.Run(outcome, func(t *testing.T) {
			bridge, original := reviewUnknownRepairBridge(t)
			correction := reviewUnknownProgressiveUsage()
			if outcome != "disconnected" {
				good := nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}})
				good["id"], good["call_id"] = "fc_corrected", "call_corrected"
				correction += sse(object{"type": "response." + outcome, "response": object{"id": "resp_corrected", "status": outcome, "usage": object{"input_tokens": 7, "output_tokens": 2, "input_tokens_details": object{"cached_tokens": 2}}, "output": []any{good}}})
			}
			calls := 0
			body := bridge.StreamWithRepair(context.Background(), io.NopCloser(strings.NewReader(original)), func(context.Context) (io.ReadCloser, error) {
				calls++
				return io.NopCloser(strings.NewReader(correction)), nil
			})
			out, err := io.ReadAll(body)
			closeErr := body.Close()
			require.NoError(t, err)
			require.NoError(t, closeErr)
			require.Equal(t, 1, calls)
			if outcome == "completed" {
				require.Contains(t, string(out), "response.completed")
				require.NotContains(t, string(out), "response.failed")
			} else {
				require.Contains(t, string(out), "response.failed")
				require.False(t, bytes.Contains(out, []byte("response.function_call_arguments")))
			}
			reviewAssertUsageSnapshots(t, body, true)
		})
	}
}

// Deliberately not context-aware: Close must interrupt the already returned
// active correction reader, not merely cancel the callback's request context.
type reviewUnknownBlockingReader struct {
	prefix    *strings.Reader
	blocked   chan struct{}
	closed    chan struct{}
	readOnce  sync.Once
	closeOnce sync.Once
}

func (r *reviewUnknownBlockingReader) Read(p []byte) (int, error) {
	if r.prefix.Len() > 0 {
		return r.prefix.Read(p)
	}
	r.readOnce.Do(func() { close(r.blocked) })
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *reviewUnknownBlockingReader) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func TestUnknownRepairCloseInterruptsActiveReader(t *testing.T) {
	for _, progressive := range []bool{false, true} {
		name := "without_reported_usage"
		prefix := ""
		if progressive {
			name, prefix = "with_progressive_usage", reviewUnknownProgressiveUsage()
		}
		t.Run(name, func(t *testing.T) {
			bridge, original := reviewUnknownRepairBridge(t)
			active := &reviewUnknownBlockingReader{prefix: strings.NewReader(prefix), blocked: make(chan struct{}), closed: make(chan struct{})}
			defer func() { _ = active.Close() }()
			body := bridge.StreamWithRepair(context.Background(), io.NopCloser(strings.NewReader(original)), func(context.Context) (io.ReadCloser, error) {
				return active, nil
			})
			select {
			case <-active.blocked:
			case <-time.After(2 * time.Second):
				_ = active.Close()
				t.Fatal("correction reader was not reached")
			}
			closed := make(chan error, 1)
			go func() { closed <- body.Close() }()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(time.Second):
				// Release a broken baseline explicitly so the failing regression
				// never leaves Close or the correction goroutine hung.
				_ = active.Close()
				select {
				case <-closed:
				case <-time.After(2 * time.Second):
					t.Fatal("Close remained stuck after explicit reader cleanup")
				}
				t.Fatal("Close did not interrupt the active unknown correction reader")
			}
			select {
			case <-active.closed:
			default:
				t.Fatal("active correction reader was not closed")
			}
			reviewAssertUsageSnapshots(t, body, progressive)
		})
	}
}
