//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Synthetic Fernet-shaped envelope; no real state, account, or signing key.
func codexTeamHTTPFixtureState(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 249) // 1+8+16+192+32 -> 332 base64 characters.
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(time.Now().Add(-time.Minute).Unix()))
	return base64.URLEncoding.EncodeToString(raw)
}

func TestCodexTeamHTTPCollects332AndReusesOnlyOnOptIn(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "standard", true: "passthrough"}[passthrough], func(t *testing.T) {
			state := codexTeamHTTPFixtureState(t)
			resp := codexTurnStateSubmissionResponse("text/event-stream", state, io.NopCloser(strings.NewReader(codexTurnStateSubmissionSSE)))
			svc, account, c, _, upstream, body := newCodexTurnStateSubmissionRequest(t, true, resp)
			account.Extra["openai_passthrough"] = passthrough
			account.Extra["codex_turn_state_mode"] = "observe"
			account.Credentials["chatgpt_user_id"] = "member-a"
			_, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Empty(t, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
			status := svc.CodexTurnStateStatus(context.Background(), account)
			require.Len(t, status.Models, 1)
			require.NotNil(t, status.Models[0].Candidate)
			require.Equal(t, 332, status.Models[0].Candidate.Length)
			require.Equal(t, gjson.GetBytes(upstream.lastBody, "model").String(), status.Models[0].Model, "bucket must use the actual upstream model after alias mapping")
			if !passthrough {
				require.NotEqual(t, "gpt-5.1", status.Models[0].Model)
			}
			encoded, err := json.Marshal(status)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), state)
			require.NotContains(t, string(encoded), "fixture-token")

			forward := func(clientState string) string {
				next, _ := gin.CreateTestContext(httptest.NewRecorder())
				next.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				next.Request.Header.Set("Content-Type", "application/json")
				next.Request.Header.Set("Session-Id", "next-session")
				if clientState != "" {
					next.Request.Header.Set(openAICodexTurnStateHeader, clientState)
				}
				next.Set("api_key", &APIKey{ID: 7})
				upstream.resp = codexTurnStateSubmissionResponse("text/event-stream", "", io.NopCloser(strings.NewReader(codexTurnStateSubmissionSSE)))
				_, err := svc.Forward(context.Background(), next, account, body)
				require.NoError(t, err)
				return upstream.requests[len(upstream.requests)-1].Header.Get(openAICodexTurnStateHeader)
			}
			require.Empty(t, forward(""), "observe cannot inject")
			account.Extra["codex_turn_state_mode"] = "reuse"
			require.Equal(t, state, forward(""))
			require.Equal(t, "client-current-turn", forward("client-current-turn"))
			account.Extra["codex_turn_state_candidate_lengths"] = []any{292}
			require.Empty(t, forward(""), "removing 332 takes effect immediately")
			account.Extra["codex_turn_state_candidate_lengths"] = []any{332}
			account.Extra["codex_turn_state_mode"] = "off"
			require.Empty(t, forward(""))
		})
	}
}

func TestCodexTeamHTTPSelectionBoundaries(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	svc := &OpenAIGatewayService{}
	account := newTurnStateV2Account(1, "workspace-a")
	account.Credentials["chatgpt_user_id"] = "member-a"
	account.Extra["codex_turn_state_mode"] = "reuse"
	scope := codexTurnStateCandidateScope(account, account)
	svc.openaiCodexTurnStateCandidates.Observe(scope, "model-a", state, []int{332}, time.Now())
	for _, tc := range []struct {
		name, path, body, header string
		account                  *Account
		want                     string
	}{
		{"same_scope", "/v1/responses", `{"model":"model-a"}`, "", account, state},
		{"different_model", "/v1/responses", `{"model":"model-b"}`, "", account, ""},
		{"missing_model", "/v1/responses", `{}`, "", account, ""},
		{"compact", "/v1/responses/compact", `{"model":"model-a"}`, "", account, ""},
		{"compat", "/v1/messages", `{"model":"model-a"}`, "", account, ""},
		{"continuation", "/v1/responses", `{"model":"model-a","previous_response_id":"resp_existing"}`, "", account, ""},
		{"frame_state", "/v1/responses", `{"model":"model-a","client_metadata":{"X-Codex-Turn-State":"client-frame"}}`, "", account, ""},
		{"client_state", "/v1/responses", `{"model":"model-a"}`, "client", account, "client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTurnStateTestContext(t, 7, "session")
			c.Request.URL.Path = tc.path
			c.Request.Header.Set(openAICodexTurnStateHeader, tc.header)
			req := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
			req.Header.Set(openAICodexTurnStateHeader, tc.header)
			req = svc.prepareCodexTurnStateHTTP(c, tc.account, []byte(tc.body), req)
			require.Equal(t, tc.want, req.Header.Get(openAICodexTurnStateHeader))
		})
	}
	for _, mutate := range []func(*Account){
		func(a *Account) { a.Credentials["chatgpt_account_id"] = "workspace-b" },
		func(a *Account) { a.Credentials["chatgpt_user_id"] = "member-b" },
		func(a *Account) { delete(a.Credentials, "chatgpt_user_id") },
		func(a *Account) { a.Extra["codex_identity_version"] = "v1" },
		func(a *Account) { a.Extra["codex_fingerprint_mode"] = "device" },
	} {
		other := *account
		other.Credentials = maps.Clone(account.Credentials)
		other.Extra = maps.Clone(account.Extra)
		mutate(&other)
		c, _ := newTurnStateTestContext(t, 7, "session")
		req := svc.prepareCodexTurnStateHTTP(c, &other, []byte(`{"model":"model-a"}`), httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil))
		require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
	}
}

func TestCodexTeamHTTPForwardRetainsClientContinuationIntent(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		state := codexTeamHTTPFixtureState(t)
		resp := codexTurnStateSubmissionResponse("text/event-stream", "", io.NopCloser(strings.NewReader(codexTurnStateSubmissionSSE)))
		svc, account, c, _, upstream, _ := newCodexTurnStateSubmissionRequest(t, true, resp)
		account.Extra["openai_passthrough"] = passthrough
		account.Extra["codex_turn_state_mode"] = "reuse"
		svc.openaiCodexTurnStateCandidates.Observe(codexTurnStateCandidateScope(account, account), "gpt-5.4", state, []int{332}, time.Now())
		body := []byte(`{"model":"gpt-5.4","stream":true,"instructions":"fixture","previous_response_id":"resp_existing","input":[{"role":"user","content":"continue"}]}`)
		_, err := svc.Forward(context.Background(), c, account, body)
		require.NoError(t, err)
		require.Len(t, upstream.requests, 1)
		require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
		require.Empty(t, upstream.requests[0].Header.Get(openAICodexTurnStateHeader), "normalizing previous_response_id must not enable candidate injection")
	}
}

func TestCodexTeamHTTPRejectedCandidateIsInvalidated(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	account := newTurnStateV2Account(1, "workspace-a")
	account.Extra["codex_turn_state_mode"] = "reuse"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"rejected"}`))}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	scope := codexTurnStateCandidateScope(account, account)
	svc.openaiCodexTurnStateCandidates.Observe(scope, "model-a", state, []int{332}, time.Now())
	c, _ := newTurnStateTestContext(t, 7, "session")
	req := svc.prepareCodexTurnStateHTTP(c, account, []byte(`{"model":"model-a"}`), httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil))
	resp, err := svc.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	require.Equal(t, 400, resp.StatusCode)
	require.Equal(t, state, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
	status := svc.CodexTurnStateStatus(context.Background(), account)
	require.Len(t, status.Models, 1)
	require.Nil(t, status.Models[0].Candidate)
	require.EqualValues(t, 1, status.Models[0].ObservedCount)
}

func TestCodexTeamHTTPFailedUncommittedAttemptDoesNotCollect(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	response := codexTurnStateSubmissionResponse("text/event-stream", state, io.NopCloser(strings.NewReader(codexTurnStateSubmissionFailedSSE)))
	svc, account, c, _, _, body := newCodexTurnStateSubmissionRequest(t, true, response)
	account.Extra["codex_turn_state_mode"] = "observe"
	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.False(t, c.Writer.Written())
	require.Empty(t, svc.CodexTurnStateStatus(context.Background(), account).Models)
}

func TestCodexTeamStateUnknownMemberIsNotShared(t *testing.T) {
	svc := &OpenAIGatewayService{}
	first := newTurnStateV2Account(1, "workspace")
	second := newTurnStateV2Account(2, "workspace")
	svc.noteOpenAICodexTurnStateOrigin(nil, first, "opaque-332")
	require.Empty(t, svc.guardOpenAICodexTurnStateValue(nil, second, "opaque-332"))
	require.Equal(t, "opaque-332", svc.guardOpenAICodexTurnStateValue(nil, first, "opaque-332"))
}
