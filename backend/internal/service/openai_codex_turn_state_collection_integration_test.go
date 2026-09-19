//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexCollectionIntegrationModel = "gpt-6-astra"
const codexCollectionIntegrationBody = `{"model":"gpt-6-astra","stream":true,"instructions":"PRIVATE_INSTRUCTIONS","input":[{"role":"user","content":[{"type":"input_text","text":"PRIVATE_USER_PROMPT"}]}],"prompt_cache_key":"PRIVATE_CACHE_KEY","client_metadata":{"session_id":"PRIVATE_SESSION","thread_id":"PRIVATE_THREAD"}}`

// All requests traverse Forward and the production HTTP transport selection;
// only the final HTTPUpstream port is replaced. The probe and generation are
// sequential because the collection gate joins its worker before Forward sends.
type codexCollectionIntegrationUpstream struct {
	*httpUpstreamRecorder
	mu            sync.Mutex
	accountIDs    []int64
	proxyURLs     []string
	concurrency   []int
	requestErrors []error
}

type codexCollectionIntegrationPendingUpstream struct {
	*codexCollectionIntegrationUpstream
	release chan struct{}
}

func (u *codexCollectionIntegrationPendingUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	response, err := u.codexCollectionIntegrationUpstream.Do(req, proxyURL, accountID, concurrency)
	u.mu.Lock()
	first := len(u.requests) == 1
	u.mu.Unlock()
	if first {
		// Simulate a transport that has not returned when the shared deadline
		// fires. The test releases it only after checking the generation barrier.
		<-req.Context().Done()
		<-u.release
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, req.Context().Err()
	}
	return response, err
}

func (u *codexCollectionIntegrationUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.proxyURLs = append(u.proxyURLs, proxyURL)
	u.concurrency = append(u.concurrency, concurrency)
	u.err = nil
	if len(u.requestErrors) > 0 {
		u.err = u.requestErrors[0]
		u.requestErrors = u.requestErrors[1:]
	}
	return u.httpUpstreamRecorder.Do(req, proxyURL, accountID, concurrency)
}

func codexCollectionIntegrationJSON(model string) string {
	return fmt.Sprintf(`{"id":"resp_collection_fixture","object":"response","model":%q,"status":"completed","error":null,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"generation-complete"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`, model)
}

func codexCollectionIntegrationSSE(model string) string {
	return "data: {\"type\":\"response.completed\",\"response\":" + codexCollectionIntegrationJSON(model) + "}\n\ndata: [DONE]\n\n"
}

func codexCollectionIntegrationResponse(status int, contentType, state, body string) *http.Response {
	response := codexTurnStateSubmissionResponse(contentType, state, io.NopCloser(strings.NewReader(body)))
	response.StatusCode = status
	return response
}

func newCodexCollectionIntegrationRequest(t *testing.T, passthrough bool, body []byte, responses ...*http.Response) (*OpenAIGatewayService, *Account, *gin.Context, *httptest.ResponseRecorder, *codexCollectionIntegrationUpstream) {
	t.Helper()
	svc, account, c, recorder, transport, _ := newCodexTurnStateSubmissionRequest(t, true, nil)
	transport.responses = responses
	upstream := &codexCollectionIntegrationUpstream{httpUpstreamRecorder: transport}
	svc.httpUpstream = upstream
	account.Extra["openai_passthrough"] = passthrough
	account.Extra[codexTurnStateModeExtraKey] = "reuse"
	account.Extra["codex_turn_state_active_collection"] = true
	account.Extra[codexTurnStateCandidateLengthsExtraKey] = []int{332}
	account.Extra["openai_device_id"] = "11111111-1111-4111-8111-111111111111"
	account.Credentials["chatgpt_user_id"] = "fixture-member"
	proxyID := int64(81)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "fixture-proxy.invalid", Port: 8080}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Cookie", "PRIVATE_COOKIE")
	c.Request.Header.Set("Session-Id", "PRIVATE_SESSION")
	c.Request.Header.Set("Conversation-Id", "PRIVATE_CONVERSATION")
	c.Request.Header.Set("X-Codex-Installation-Id", "11111111-1111-4111-8111-111111111112")
	return svc, account, c, recorder, upstream
}

func assertCodexCollectionIntegrationProbe(t *testing.T, upstream *codexCollectionIntegrationUpstream, account *Account) {
	t.Helper()
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	require.NotEmpty(t, upstream.requests)
	probe := upstream.requests[0]
	body := upstream.bodies[0]
	require.Equal(t, http.MethodPost, probe.Method)
	require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", probe.URL.String())
	require.Equal(t, codexCollectionIntegrationModel, gjson.GetBytes(body, "model").String())
	require.Equal(t, "Reply with OK.", gjson.GetBytes(body, "instructions").String())
	require.Equal(t, "Reply with OK.", gjson.GetBytes(body, "input.0.content.0.text").String())
	require.True(t, gjson.GetBytes(body, "stream").Bool())
	require.False(t, gjson.GetBytes(body, "store").Bool())
	for _, field := range []string{"previous_response_id", "client_metadata", "prompt_cache_key", "tools", "conversation"} {
		require.False(t, gjson.GetBytes(body, field).Exists(), "probe must not inherit %s", field)
	}
	require.NotContains(t, string(body), "PRIVATE_")
	for _, header := range []string{"Cookie", "Session-Id", "Conversation-Id", "session_id", "conversation_id", openAICodexTurnStateHeader, "X-Codex-Turn-Metadata", "X-Codex-Routing-Hint"} {
		require.Empty(t, probe.Header.Get(header), "probe must not inherit %s", header)
	}
	require.Equal(t, "Bearer fixture-token", probe.Header.Get("Authorization"))
	require.Equal(t, account.ID, upstream.accountIDs[0])
	require.Equal(t, account.Proxy.URL(), upstream.proxyURLs[0])
	require.Equal(t, account.Concurrency, upstream.concurrency[0])
}

func TestCodexTurnStateCollectionIntegrationActiveProbeThenGeneration(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, jsonProbe := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/json_probe=%t", passthrough, jsonProbe), func(t *testing.T) {
				state := codexTeamHTTPFixtureState(t)
				contentType, probeBody := "text/event-stream", codexCollectionIntegrationSSE(codexCollectionIntegrationModel)
				if jsonProbe {
					contentType, probeBody = "application/json", codexCollectionIntegrationJSON(codexCollectionIntegrationModel)
				}
				body := []byte(codexCollectionIntegrationBody)
				svc, account, c, recorder, upstream := newCodexCollectionIntegrationRequest(t, passthrough, body,
					codexCollectionIntegrationResponse(200, contentType, state, probeBody),
					codexCollectionIntegrationResponse(200, "text/event-stream", "", codexCollectionIntegrationSSE(codexCollectionIntegrationModel)))
				result, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 2)
				assertCodexCollectionIntegrationProbe(t, upstream, account)
				require.Contains(t, string(upstream.bodies[1]), "PRIVATE_USER_PROMPT")
				require.Equal(t, state, upstream.requests[1].Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, codexCollectionIntegrationModel, gjson.GetBytes(upstream.bodies[1], "model").String())
				require.Equal(t, []int64{account.ID, account.ID}, upstream.accountIDs)
				require.Equal(t, []string{account.Proxy.URL(), account.Proxy.URL()}, upstream.proxyURLs)
				for _, header := range []string{"Authorization", "Chatgpt-Account-Id", "Chatgpt-User-Id", "User-Agent", "Version", "Originator", "Oai-Device-Id", "X-Codex-Installation-Id"} {
					require.Equal(t, upstream.requests[1].Header.Get(header), upstream.requests[0].Header.Get(header), "probe and generation must share %s", header)
				}
				require.Contains(t, recorder.Body.String(), "generation-complete")
				models := svc.CodexTurnStateStatus(context.Background(), account).Models
				require.Len(t, models, 1)
				require.NotNil(t, models[0].Collection)
				require.Equal(t, "accepted", models[0].Collection.LastReason)
				require.EqualValues(t, 1, models[0].Collection.AttemptCount)
				require.EqualValues(t, 1, models[0].ReuseAttemptCount)
				require.NotNil(t, models[0].LastRequest)
				require.Equal(t, "candidate", models[0].LastRequest.StateSource)
				require.Equal(t, 332, models[0].LastRequest.OutboundStateLength)
			})
		}
	}
}

func TestCodexTurnStateCollectionIntegrationSkipsIneligibleRequests(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, scenario := range []string{"passive", "observe", "cached", "client_header", "client_metadata", "original_previous_response_id", "empty_lengths"} {
			t.Run(fmt.Sprintf("passthrough=%t/%s", passthrough, scenario), func(t *testing.T) {
				body := []byte(codexCollectionIntegrationBody)
				var err error
				if scenario == "client_metadata" {
					body, err = sjson.SetBytes(body, "client_metadata.X-Codex-Turn-State", "client-metadata-state")
				} else if scenario == "original_previous_response_id" {
					body, err = sjson.SetBytes(body, "previous_response_id", "resp_client_continuation")
				}
				require.NoError(t, err)
				svc, account, c, _, upstream := newCodexCollectionIntegrationRequest(t, passthrough, body,
					codexCollectionIntegrationResponse(200, "text/event-stream", "", codexCollectionIntegrationSSE(codexCollectionIntegrationModel)))
				state := codexTeamHTTPFixtureState(t)
				switch scenario {
				case "passive":
					account.Extra["codex_turn_state_active_collection"] = false
				case "observe":
					account.Extra[codexTurnStateModeExtraKey] = "observe"
				case "cached":
					svc.openaiCodexTurnStateCandidates.Observe(codexTurnStateCandidateScope(account, account), codexCollectionIntegrationModel, state, []int{332}, time.Now())
				case "client_header":
					c.Request.Header.Set(openAICodexTurnStateHeader, "client-header-state")
				case "empty_lengths":
					account.Extra[codexTurnStateCandidateLengthsExtraKey] = []int{}
				}
				_, err = svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1, "no probe may precede generation")
				require.Contains(t, string(upstream.bodies[0]), "PRIVATE_USER_PROMPT")
				if scenario == "cached" {
					require.Equal(t, state, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
				}
				for _, model := range svc.CodexTurnStateStatus(context.Background(), account).Models {
					require.Nil(t, model.Collection, "skipping collection must not invent a probe attempt")
				}
			})
		}
	}
}

func TestCodexTurnStateCollectionIntegrationOrdinaryFailureContinuesGenerationOnce(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, scenario := range []string{"upstream_error", "missing_state", "invalid_format", "length_not_allowed", "model_mismatch", "transport_error", "timeout"} {
			t.Run(fmt.Sprintf("passthrough=%t/%s", passthrough, scenario), func(t *testing.T) {
				state, model, status := codexTeamHTTPFixtureState(t), codexCollectionIntegrationModel, 200
				switch scenario {
				case "upstream_error":
					status = 500
				case "missing_state":
					state = ""
				case "invalid_format":
					state = strings.Repeat("A", 332)
				case "length_not_allowed":
					state = testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 10, 2)
				case "model_mismatch":
					model = "gpt-5.6-luna"
				}
				body := []byte(codexCollectionIntegrationBody)
				generation := codexCollectionIntegrationResponse(200, "text/event-stream", "", codexCollectionIntegrationSSE(codexCollectionIntegrationModel))
				svc, account, c, _, upstream := newCodexCollectionIntegrationRequest(t, passthrough, body,
					codexCollectionIntegrationResponse(status, "application/json", state, codexCollectionIntegrationJSON(model)), generation)
				if scenario == "transport_error" || scenario == "timeout" {
					probeErr := errors.New("fixture transport unavailable")
					if scenario == "timeout" {
						probeErr = context.DeadlineExceeded
					}
					upstream.requestErrors = []error{probeErr, nil}
					upstream.responses = []*http.Response{generation}
				}
				_, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Len(t, upstream.requests, 2)
				assertCodexCollectionIntegrationProbe(t, upstream, account)
				require.Contains(t, string(upstream.bodies[1]), "PRIVATE_USER_PROMPT")
				require.Empty(t, upstream.requests[1].Header.Get(openAICodexTurnStateHeader))
				models := svc.CodexTurnStateStatus(context.Background(), account).Models
				require.Len(t, models, 1)
				require.Nil(t, models[0].Candidate)
				require.NotNil(t, models[0].Collection)
				require.Equal(t, scenario, models[0].Collection.LastReason)
				require.EqualValues(t, 1, models[0].Collection.AttemptCount)
				require.Zero(t, models[0].ReuseAttemptCount)
			})
		}
	}
}

func TestCodexTurnStateCollectionIntegrationRejectionStopsAccountGeneration(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, status := range []int{401, 403, 429} {
			for _, format := range []string{"http", "json", "sse", "partial_sse"} {
				t.Run(fmt.Sprintf("passthrough=%t/status=%d/%s", passthrough, status, format), func(t *testing.T) {
					code := map[int]string{401: "invalid_access_token", 403: "permission_denied", 429: "rate_limit_exceeded"}[status]
					reason := map[int]string{401: "unauthorized", 403: "forbidden", 429: "rate_limited"}[status]
					responseStatus, contentType := status, "application/json"
					errorBody := fmt.Sprintf(`{"error":{"code":%q,"message":"fixture rejection"}}`, code)
					if format == "json" {
						responseStatus = 200
					} else if format == "sse" || format == "partial_sse" {
						responseStatus, contentType = 200, "text/event-stream"
						// A later malformed or completed frame must not erase a rejection.
						errorBody = "event: error\ndata: " + errorBody + "\n\ndata: not-json\n\n" + codexCollectionIntegrationSSE(codexCollectionIntegrationModel)
					}
					body := []byte(codexCollectionIntegrationBody)
					probe := codexCollectionIntegrationResponse(responseStatus, contentType, codexTeamHTTPFixtureState(t), errorBody)
					if format == "partial_sse" {
						probe.Body = io.NopCloser(io.MultiReader(strings.NewReader(errorBody), passthroughErrReadCloser{err: io.ErrUnexpectedEOF}))
					}
					svc, account, c, recorder, upstream := newCodexCollectionIntegrationRequest(t, passthrough, body,
						probe)
					_, err := svc.Forward(context.Background(), c, account, body)
					require.Error(t, err)
					require.Len(t, upstream.requests, 1, "rejected collection must stop before generation")
					assertCodexCollectionIntegrationProbe(t, upstream, account)
					require.NotContains(t, recorder.Body.String(), "generation-complete")
					models := svc.CodexTurnStateStatus(context.Background(), account).Models
					require.Len(t, models, 1)
					require.Nil(t, models[0].Candidate)
					require.NotNil(t, models[0].Collection)
					require.Equal(t, reason, models[0].Collection.LastReason)
					require.Equal(t, status, models[0].Collection.LastHTTPStatus)
					require.EqualValues(t, 1, models[0].Collection.AttemptCount)
					require.Zero(t, models[0].ReuseAttemptCount)
					require.NotNil(t, models[0].LastRequest)
					require.Equal(t, "collection_rejected", models[0].LastRequest.SelectionReason)
					require.Equal(t, "none", models[0].LastRequest.StateSource)
					require.Zero(t, models[0].LastRequest.OutboundStateLength)
				})
			}
		}
	}
}

func TestCodexTurnStateCollectionIntegrationCredentialRejectionBlocksCachedAndClientState(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, status := range []int{401, 403, 429} {
			for _, hasClientState := range []bool{false, true} {
				t.Run(fmt.Sprintf("passthrough=%t/status=%d/client_state=%t", passthrough, status, hasClientState), func(t *testing.T) {
					body := []byte(codexCollectionIntegrationBody)
					svc, account, c, _, upstream := newCodexCollectionIntegrationRequest(t, passthrough, body,
						codexCollectionIntegrationResponse(status, "application/json", "", `{"error":{"message":"fixture rejection"}}`),
						codexCollectionIntegrationResponse(200, "text/event-stream", "", codexCollectionIntegrationSSE(codexCollectionIntegrationModel)))
					_, err := svc.Forward(context.Background(), c, account, body)
					require.Error(t, err)
					require.Len(t, upstream.requests, 1)
					// A fresh candidate or client-provided state cannot authorize sending
					// another model while this credential's rejection is retained.
					const nextModel = "gpt-5.6-sol"
					nextBody, err := sjson.SetBytes(body, "model", nextModel)
					require.NoError(t, err)
					svc.openaiCodexTurnStateCandidates.Observe(codexTurnStateCandidateScope(account, account), nextModel, codexTeamHTTPFixtureState(t), []int{332}, time.Now())
					next, _ := gin.CreateTestContext(httptest.NewRecorder())
					next.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nextBody))
					next.Request.Header.Set("Content-Type", "application/json")
					next.Set("api_key", &APIKey{ID: 7})
					if hasClientState {
						next.Request.Header.Set(openAICodexTurnStateHeader, "client-current-turn")
					}
					_, err = svc.Forward(context.Background(), next, account, nextBody)
					require.Error(t, err)
					require.Len(t, upstream.requests, 1, "credential rejection must not be bypassed by a cached or client state")
					models := svc.CodexTurnStateStatus(context.Background(), account).Models
					require.Len(t, models, 2)
					for _, model := range models {
						if model.Collection != nil {
							require.EqualValues(t, 1, model.Collection.AttemptCount)
						}
						require.Zero(t, model.ReuseAttemptCount)
						require.NotNil(t, model.LastRequest)
						wantReason := "collection_rejected"
						if model.Model == nextModel {
							wantReason = "collection_cooldown"
						}
						require.Equal(t, wantReason, model.LastRequest.SelectionReason)
						require.Equal(t, "none", model.LastRequest.StateSource)
						require.Zero(t, model.LastRequest.OutboundStateLength)
					}
				})
			}
		}
	}
}

func TestCodexTurnStateCollectionIntegrationUnresolvedDeadlineStopsGeneration(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%t", passthrough), func(t *testing.T) {
			body := []byte(codexCollectionIntegrationBody)
			svc, account, c, recorder, upstream := newCodexCollectionIntegrationRequest(t, passthrough, body,
				codexCollectionIntegrationResponse(200, "text/event-stream", "", codexCollectionIntegrationSSE(codexCollectionIntegrationModel)),
				codexCollectionIntegrationResponse(200, "text/event-stream", "", codexCollectionIntegrationSSE(codexCollectionIntegrationModel)))
			pending := &codexCollectionIntegrationPendingUpstream{codexCollectionIntegrationUpstream: upstream, release: make(chan struct{})}
			svc.httpUpstream = pending
			svc.openaiCodexTurnStateCollection.withTimeout = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
				return context.WithTimeout(ctx, 10*time.Millisecond)
			}
			t.Cleanup(func() {
				close(pending.release)
				svc.openaiCodexTurnStateCollection.mu.Lock()
				entry := svc.openaiCodexTurnStateCollection.records[account.ID]
				svc.openaiCodexTurnStateCollection.mu.Unlock()
				if entry != nil {
					select {
					case <-entry.done:
					case <-time.After(2 * time.Second):
						t.Error("collection worker did not exit after release")
					}
				}
			})
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err, "an unresolved collection must not fall through to generation")
			upstream.mu.Lock()
			requestCount := len(upstream.requests)
			upstream.mu.Unlock()
			require.Equal(t, 1, requestCount)
			assertCodexCollectionIntegrationProbe(t, upstream, account)
			require.NotContains(t, recorder.Body.String(), "generation-complete")
			svc.openaiCodexTurnStateCollection.mu.Lock()
			stillRunning := svc.openaiCodexTurnStateCollection.records[account.ID].running
			svc.openaiCodexTurnStateCollection.mu.Unlock()
			require.True(t, stillRunning, "the assertion must execute before the fake transport returns")
			nextBody, err := sjson.SetBytes(body, "model", "gpt-5.6-sol")
			require.NoError(t, err)
			next, _ := gin.CreateTestContext(httptest.NewRecorder())
			next.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nextBody))
			next.Request.Header.Set("Content-Type", "application/json")
			next.Set("api_key", &APIKey{ID: 7})
			_, err = svc.Forward(context.Background(), next, account, nextBody)
			require.Error(t, err, "another model must also wait for this credential's unresolved attempt")
			upstream.mu.Lock()
			requestCount = len(upstream.requests)
			upstream.mu.Unlock()
			require.Equal(t, 1, requestCount)
			models := svc.CodexTurnStateStatus(context.Background(), account).Models
			require.Len(t, models, 2)
			for _, model := range models {
				require.NotNil(t, model.LastRequest)
				require.Equal(t, "collection_pending", model.LastRequest.SelectionReason)
				require.Equal(t, "none", model.LastRequest.StateSource)
				require.Zero(t, model.LastRequest.OutboundStateLength)
			}
		})
	}
}
