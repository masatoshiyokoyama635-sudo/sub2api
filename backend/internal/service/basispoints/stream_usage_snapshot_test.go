package basispoints

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testUsageSnapshotter interface {
	UsageAttempts() []map[string]any
}

func TestStreamUsageSnapshotRecordsOriginalBeforeRepair(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	initial := repairResponse("initial", 10, 2, repairCall("bad", "Run", "text(1)"))
	started := make(chan struct{})
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(ctx context.Context, _ object, _ error) (object, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	defer func() { _ = body.Close() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("correction did not start")
	}
	snapshotter, ok := body.(testUsageSnapshotter)
	require.True(t, ok, "stream must expose independently synchronized usage snapshots")
	usage := snapshotter.UsageAttempts()
	require.Len(t, usage, 1)
	require.Equal(t, json.Number("10"), usage[0]["input_tokens"])
	usage[0]["input_tokens"] = 999
	details, ok := usage[0]["input_tokens_details"].(object)
	require.True(t, ok)
	details["cached_tokens"] = 999
	copy := snapshotter.UsageAttempts()
	require.Equal(t, json.Number("10"), copy[0]["input_tokens"])
	copyDetails, ok := copy[0]["input_tokens_details"].(object)
	require.True(t, ok)
	require.Equal(t, json.Number("2"), copyDetails["cached_tokens"])
	require.NoError(t, body.Close())
	require.Len(t, snapshotter.UsageAttempts(), 1, "unknown in-flight correction is not a billed attempt")
}

func TestStreamUsageSnapshotPreservesDistinctAttempts(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	initial := repairResponse("initial", 10, 2, repairCall("bad", "Run", "text(1)"))
	calls := 0
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(_ context.Context, _ object, _ error) (object, error) {
		calls++
		summary := "Run"
		if calls == 2 {
			summary = "codex2api.custom/functions.exec"
		}
		return repairResponse("corrected", 10*(calls+1), calls+2, repairCall("corrected", summary, "text(1)")), nil
	})
	repairEvents(t, body)
	snapshotter, ok := body.(testUsageSnapshotter)
	require.True(t, ok)
	usage := snapshotter.UsageAttempts()
	require.Len(t, usage, 3)
	require.Equal(t, json.Number("10"), usage[0]["input_tokens"])
	require.Equal(t, json.Number("20"), usage[1]["input_tokens"])
	require.Equal(t, json.Number("30"), usage[2]["input_tokens"])
}

func TestStreamUsageSnapshotConcurrentReadAndClose(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	initial := repairResponse("initial", 10, 2, repairCall("bad", "Run", "text(1)"))
	started := make(chan struct{})
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(ctx context.Context, _ object, _ error) (object, error) {
		close(started)
		<-ctx.Done()
		return repairResponse("known_before_close", 20, 3), ctx.Err()
	})
	defer func() { _ = body.Close() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("correction did not start")
	}
	snapshotter, ok := body.(testUsageSnapshotter)
	require.True(t, ok)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				usage := snapshotter.UsageAttempts()
				for _, attempt := range usage {
					attempt["input_tokens"] = j
				}
			}
		}()
	}
	require.NoError(t, body.Close())
	wg.Wait()
	usage := snapshotter.UsageAttempts()
	require.Len(t, usage, 2)
	require.Equal(t, json.Number("10"), usage[0]["input_tokens"])
	require.Equal(t, json.Number("20"), usage[1]["input_tokens"])
}
