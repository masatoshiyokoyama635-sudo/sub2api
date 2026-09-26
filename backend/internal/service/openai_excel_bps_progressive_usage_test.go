package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func excelBPSProgressiveUsageWire(t *testing.T, kind string, usage OpenAIUsage, output []any) string {
	t.Helper()
	event, err := json.Marshal(map[string]any{"type": kind, "response": map[string]any{
		"id": "resp_progressive_usage", "model": "gpt-6-astra", "status": strings.TrimPrefix(kind, "response."), "output": output, "usage": usage,
	}})
	require.NoError(t, err)
	return "data: " + string(event) + string([]byte{10, 10})
}

func TestExcelBPSProgressiveUsageSurvivesZeroTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	known := OpenAIUsage{InputTokens: 1000, OutputTokens: 50, CacheCreationInputTokens: 200, CacheReadInputTokens: 100}
	measured := OpenAIUsage{InputTokens: 2000, OutputTokens: 80, CacheReadInputTokens: 300}
	for _, stream := range []bool{false, true} {
		for _, terminal := range []string{"response.completed", "response.failed", "response.incomplete"} {
			for _, tc := range []struct {
				name                     string
				progressive, final, want OpenAIUsage
				wantAttempts             int
			}{
				{name: "preserve_progressive", progressive: known, want: known},
				{name: "nonzero_terminal_authoritative", progressive: known, final: measured, want: measured, wantAttempts: 1},
				{name: "all_zero_unchanged", wantAttempts: 1},
			} {
				t.Run(fmt.Sprintf("stream=%t/%s/%s", stream, terminal, tc.name), func(t *testing.T) {
					wire := excelBPSProgressiveUsageWire(t, "response.in_progress", tc.progressive, []any{}) + excelBPSProgressiveUsageWire(t, terminal, tc.final, []any{})
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}}
					svc := openAIClientToolsTestService(upstream)
					body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": stream, "input": "known progressive usage"})
					require.NoError(t, err)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					result, err := svc.Forward(context.Background(), c, excelAccount(), body)
					if terminal == "response.completed" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					require.NotNil(t, result)
					require.Equal(t, tc.want, result.Usage, "all-zero terminal must not erase observed progressive usage")
					require.Len(t, result.BasispointsUsageAttempts, tc.wantAttempts)
					if tc.wantAttempts == 0 {
						require.Nil(t, result.BasispointsUsageAttempts, "fallback usage must not be priced as all-zero attempts")
					}
				})
			}
		}
	}
}

func TestExcelBPSProgressiveUsageClearsZeroRepairAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			for i, summary := range []string{"Run", "codex2api.custom/functions.exec"} {
				args, err := json.Marshal(map[string]any{"summary": summary, "code": "text(42);", "extended_summary": "{}", "destructive": false, "references": []any{}})
				require.NoError(t, err)
				output := []any{map[string]any{"type": "function_call", "name": "run_officejs", "id": fmt.Sprint("fc_", i), "call_id": fmt.Sprint("call_", i), "arguments": string(args), "status": "completed"}}
				wire := excelBPSProgressiveUsageWire(t, "response.completed", OpenAIUsage{}, output)
				if i == 0 {
					wire = excelBPSProgressiveUsageWire(t, "response.in_progress", OpenAIUsage{InputTokens: 10, OutputTokens: 2}, []any{}) + wire
				}
				upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))})
			}
			svc := openAIClientToolsTestService(upstream)
			body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": stream, "input": "known progressive usage during repair", "tools": []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec"}}}}})
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			result, err := svc.Forward(context.Background(), c, excelAccount(), body)
			require.NoError(t, err)
			require.Len(t, upstream.requests, 2)
			require.Equal(t, OpenAIUsage{InputTokens: 10, OutputTokens: 2}, result.Usage)
			require.Nil(t, result.BasispointsUsageAttempts, "multiple all-zero attempts must not erase the fallback billing input")
		})
	}
}
