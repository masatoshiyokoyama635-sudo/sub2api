package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type excelBPSCancelUsageUpstream struct {
	*httpUpstreamRecorder
	completed int
	started   chan struct{}
}

func (u *excelBPSCancelUsageUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	if len(u.requests) < u.completed {
		return u.httpUpstreamRecorder.Do(req, proxy, accountID, concurrency)
	}
	close(u.started)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestExcelBPSCancelDuringRepairPreservesKnownUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, completed := range []int{1, 2} {
			for _, creationAsInput := range []bool{false, true} {
				t.Run(fmt.Sprintf("stream=%t/known=%d/creation_as_input=%t", stream, completed, creationAsInput), func(t *testing.T) {
					upstream := &httpUpstreamRecorder{}
					for i := 0; i < completed; i++ {
						body := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSRepairWire(t, fmt.Sprint(i), "Run"))}
						upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body})
					}
					blocked := &excelBPSCancelUsageUpstream{httpUpstreamRecorder: upstream, completed: completed, started: make(chan struct{})}
					svc := openAIClientToolsTestService(upstream)
					svc.httpUpstream = blocked
					account := excelAccount()
					account.Extra["openai_excel_bps_cache_creation_as_input"] = creationAsInput
					body, err := json.Marshal(map[string]any{
						"model": "gpt-5.6-sol", "stream": stream, "input": "test cancelled correction",
						"tools": []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec"}}}},
					})
					require.NoError(t, err)
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
					type outcome struct {
						result *OpenAIForwardResult
						err    error
					}
					done := make(chan outcome, 1)
					go func() { result, err := svc.Forward(ctx, c, account, body); done <- outcome{result, err} }()
					select {
					case <-blocked.started:
					case <-time.After(3 * time.Second):
						t.Fatal("correction did not begin")
					}
					cancel()
					select {
					case got := <-done:
						require.ErrorIs(t, got.err, context.Canceled)
						require.NotNil(t, got.result)
						require.True(t, got.result.ClientDisconnect)
						require.Equal(t, completed*10, got.result.Usage.InputTokens)
						require.Equal(t, completed*2, got.result.Usage.OutputTokens)
						require.Len(t, got.result.BasispointsUsageAttempts, completed)
						for _, attempt := range got.result.BasispointsUsageAttempts {
							require.Equal(t, 10, attempt.InputTokens)
							require.Equal(t, 2, attempt.OutputTokens)
						}
						require.Equal(t, creationAsInput, got.result.BasispointsCacheCreationAsInput)
					case <-time.After(3 * time.Second):
						t.Fatal("cancelled correction did not return")
					}
				})
			}
		}
	}
}
