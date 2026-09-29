//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestExcelBPSImagePolicyUsageAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, creationAsInput := range []bool{false, true} {
			for _, scenario := range []string{"direct", "repair", "cancel_repair", "progressive", "generation_reject", "compact_failed", "cancel_generation"} {
				t.Run(fmt.Sprintf("stream=%t/cache_as_input=%t/%s", stream, creationAsInput, scenario), func(t *testing.T) {
					svc := openAIClientToolsTestService(nil)
					svc.cache = newImagePolicyMemoryCache()
					repo := &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSImageRelayEnabled: "true", SettingKeyExcelBPSImageMode: "native", SettingKeyExcelBPSImageLimitPolicy: "auto_compact"}}
					svc.settingService = NewSettingService(repo, svc.cfg)
					_, pixels := nativeGatewayBody(t)
					body := bytes.ReplaceAll(imagePolicyRequest(t, 19, 2), []byte("data:image/png;base64,synthetic"), []byte("data:image/png;base64,"+base64.StdEncoding.EncodeToString(pixels)))
					var err error
					body, err = sjson.SetBytes(body, "model", "gpt-6-astra")
					require.NoError(t, err)
					body, err = sjson.SetBytes(body, "stream", stream)
					require.NoError(t, err)
					body, err = sjson.SetBytes(body, "tools", []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec"}}}})
					require.NoError(t, err)
					compactUsage := OpenAIUsage{InputTokens: 7, OutputTokens: 3, CacheCreationInputTokens: 2, CacheReadInputTokens: 1}
					nativeUsage := OpenAIUsage{InputTokens: 10, OutputTokens: 2}
					compactKind := "response.completed"
					if scenario == "compact_failed" {
						compactKind = "response.failed"
					}
					compactWire := excelBPSProgressiveUsageWire(t, compactKind, compactUsage, []any{map[string]any{"type": "compaction", "id": "cmp-attempts", "encrypted_content": "opaque-attempts-state"}})
					directWire := excelBPSProgressiveUsageWire(t, "response.completed", nativeUsage, []any{})
					badRepairWire := excelBPSRepairWire(t, "initial", "Run")
					goodRepairWire := excelBPSRepairWire(t, "repair", "codex2api.custom/functions.exec")
					progressiveWire := excelBPSProgressiveUsageWire(t, "response.in_progress", nativeUsage, []any{}) + excelBPSProgressiveUsageWire(t, "response.completed", OpenAIUsage{}, []any{})
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					started := make(chan struct{})
					var phase atomic.Int32
					var previous *excelBPSRepairBody
					svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, concurrency int) (*http.Response, error) {
						if req.URL.String() == basispoints.AttachmentsURL {
							return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"openai_file_id\":\"file-attempts\"}"))}, nil
						}
						if id != 300 || concurrency != 1 {
							return nil, fmt.Errorf("changed account scheduling: %d/%d", id, concurrency)
						}
						if previous != nil && !previous.closed.Load() {
							return nil, fmt.Errorf("previous attempt retained its concurrency lease")
						}
						n := phase.Add(1)
						if (scenario == "cancel_repair" && n == 3) || (scenario == "cancel_generation" && n == 2) {
							close(started)
							<-req.Context().Done()
							return nil, req.Context().Err()
						}
						wire, status := compactWire, http.StatusOK
						if n > 1 {
							wire = directWire
							switch scenario {
							case "repair", "cancel_repair":
								if n == 2 {
									wire = badRepairWire
								} else {
									wire = goodRepairWire
								}
							case "progressive":
								wire = progressiveWire
							case "generation_reject":
								status, wire = http.StatusTooManyRequests, "{}"
							}
						}
						previous = &excelBPSRepairBody{Reader: strings.NewReader(wire)}
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: previous}, nil
					}}
					account := excelAccount()
					account.Concurrency = 1
					account.Extra["openai_excel_bps_cache_creation_as_input"] = creationAsInput
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
					c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
					c.Request.Header.Set("session_id", "attempts-session")
					type outcome struct {
						result *OpenAIForwardResult
						err    error
					}
					done := make(chan outcome, 1)
					go func() {
						result, err := svc.forwardExcelBPS(ctx, c, account, body, time.Now())
						done <- outcome{result, err}
					}()
					cancelling := scenario == "cancel_repair" || scenario == "cancel_generation"
					if cancelling {
						select {
						case <-started:
							cancel()
						case <-time.After(3 * time.Second):
							cancel()
							t.Fatal("synthetic cancellation stage did not start")
						}
					}
					var got outcome
					select {
					case got = <-done:
					case <-time.After(3 * time.Second):
						cancel()
						t.Fatal("forwarder or Close did not join")
					}
					require.NotNil(t, got.result)
					if cancelling {
						require.ErrorIs(t, got.err, context.Canceled)
					} else if scenario == "generation_reject" || scenario == "compact_failed" {
						require.Error(t, got.err)
					} else {
						require.NoError(t, got.err)
					}
					wantNative := 1
					wantAttempts := 2
					switch scenario {
					case "repair":
						wantNative, wantAttempts = 2, 3
					case "progressive":
						wantAttempts = 0
					case "generation_reject", "compact_failed", "cancel_generation":
						wantNative, wantAttempts = 0, 0
					}
					require.Equal(t, 7+10*wantNative, got.result.Usage.InputTokens)
					require.Equal(t, 3+2*wantNative, got.result.Usage.OutputTokens)
					require.Equal(t, creationAsInput, got.result.BasispointsCacheCreationAsInput, "compact-only fallback must retain account cache pricing policy")
					require.Len(t, got.result.BasispointsUsageAttempts, wantAttempts, "compaction is one independently observed attempt; unknown continuation split stays aggregate")
					if wantAttempts == 0 {
						require.Nil(t, got.result.BasispointsUsageAttempts)
					} else {
						require.Equal(t, compactUsage, got.result.BasispointsUsageAttempts[wantAttempts-1])
						for _, attempt := range got.result.BasispointsUsageAttempts[:wantAttempts-1] {
							require.Equal(t, nativeUsage, attempt)
						}
					}
					if scenario == "repair" {
						require.Equal(t, int32(3), phase.Load())
						require.Contains(t, rec.Body.String(), "text(42);")
					}
				})
			}
		}
	}
}
