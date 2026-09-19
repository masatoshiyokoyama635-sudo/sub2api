//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexTurnStateRequestDiagnosticsCorrelateActualOutboundState(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, tc := range []struct {
			name, mode, client, reason, source string
			seed, declared                     bool
		}{
			{"candidate_with_different_model", "reuse", "", "reused", "candidate", true, true},
			{"client_header", "reuse", "header", "client_state", "client", true, true},
			{"client_metadata", "reuse", "metadata", "client_metadata_state", "client", true, true},
			{"continuation", "reuse", "continuation", "client_continuation", "none", true, true},
			{"observe_only", "observe", "", "observe_mode", "none", true, false},
			{"no_candidate", "reuse", "", "no_candidate", "none", false, false},
			{"stripped_foreign_client_state", "reuse", "foreign", "client_state", "none", true, true},
		} {
			pathName := map[bool]string{false: "standard", true: "passthrough"}[passthrough]
			t.Run(pathName+"/"+tc.name, func(t *testing.T) {
				state := codexTeamHTTPFixtureState(t)
				responseBody := codexTurnStateSubmissionSSE
				if tc.declared {
					responseBody = strings.ReplaceAll(responseBody, `"object":"response"`, `"object":"response","model":"gpt-5.6-luna"`)
				}
				response := codexTurnStateSubmissionResponse("text/event-stream", "", io.NopCloser(strings.NewReader(responseBody)))
				svc, account, c, _, upstream, _ := newCodexTurnStateSubmissionRequest(t, true, response)
				account.Extra["openai_passthrough"] = passthrough
				account.Extra["codex_turn_state_mode"] = tc.mode
				account.Credentials["chatgpt_user_id"] = "member-a"
				bodyObject := map[string]any{"model": "gpt-6-astra", "stream": true, "instructions": "fixture", "input": "hello"}
				if tc.client == "metadata" {
					bodyObject["client_metadata"] = map[string]string{openAICodexTurnStateHeader: state}
				}
				if tc.client == "continuation" {
					bodyObject["previous_response_id"] = "resp_original"
				}
				body, err := json.Marshal(bodyObject)
				require.NoError(t, err)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				if tc.client == "header" || tc.client == "foreign" {
					c.Request.Header.Set(openAICodexTurnStateHeader, state)
				}
				if tc.client == "foreign" {
					svc.noteOpenAICodexTurnStateBlobOrigin(nil, newTurnStateV2Account(42, "other-owner"), state)
				}
				if tc.seed {
					svc.openaiCodexTurnStateCandidates.Observe(codexTurnStateCandidateScope(account, account), "gpt-6-astra", state, []int{332}, time.Now())
				}
				_, err = svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1)
				status := svc.CodexTurnStateStatus(context.Background(), account)
				require.Len(t, status.Models, 1)
				model := status.Models[0]
				require.NotNil(t, model.LastRequest)
				actual := model.LastRequest
				require.Equal(t, tc.reason, actual.SelectionReason)
				require.Equal(t, tc.source, actual.StateSource)
				outbound := upstream.requests[0].Header.Get(openAICodexTurnStateHeader)
				if outbound == "" {
					outbound = gjson.GetBytes(upstream.lastBody, "client_metadata."+openAICodexTurnStateHeader).String()
				}
				require.Equal(t, len(outbound), actual.OutboundStateLength)
				if tc.source != "none" {
					require.Equal(t, 332, actual.OutboundStateLength)
				}
				require.Equal(t, tc.declared, actual.ResponseModelObserved)
				require.Equal(t, tc.declared, actual.ModelMismatch)
				if tc.declared {
					require.Equal(t, "gpt-5.6-luna", actual.UpstreamResponseModel)
				}
				require.False(t, actual.Failed)
				if tc.source == "candidate" {
					require.EqualValues(t, 1, model.ReuseAttemptCount)
				} else {
					require.Zero(t, model.ReuseAttemptCount)
				}
				encoded, err := json.Marshal(status)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), state)
				require.NotContains(t, string(encoded), "fixture-token")
			})
		}
	}
}

func TestCodexTurnStateRequestDiagnosticsDoNotLeakAcrossAttempts(t *testing.T) {
	svc, account, c, _, upstream, body := newCodexTurnStateSubmissionRequest(t, true,
		codexTurnStateSubmissionResponse("text/event-stream", "", io.NopCloser(strings.NewReader(codexTurnStateSubmissionSSE))))
	account.Extra["codex_turn_state_mode"] = "reuse"
	_, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	before := svc.CodexTurnStateStatus(context.Background(), account).Models
	require.Len(t, before, 1)
	require.NotNil(t, before[0].LastRequest)
	account.Extra["codex_turn_state_mode"] = "off"
	upstream.resp = codexTurnStateSubmissionResponse("text/event-stream", "", io.NopCloser(strings.NewReader(codexTurnStateSubmissionSSE)))
	_, err = svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	after := svc.CodexTurnStateStatus(context.Background(), account).Models
	require.Equal(t, before[0].LastRequest.At, after[0].LastRequest.At, "disabled attempt must not reuse an earlier Gin diagnostic")
}
