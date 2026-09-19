package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexCollectionUnitUpstream struct {
	HTTPUpstream
	do func(*http.Request) (*http.Response, error)
}

func (u *codexCollectionUnitUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.do(req)
}

type codexCollectionUnitBody struct {
	reader io.Reader
	read   int
	closed bool
}

func (body *codexCollectionUnitBody) Read(dst []byte) (int, error) {
	n, err := body.reader.Read(dst)
	body.read += n
	return n, err
}

func (body *codexCollectionUnitBody) Close() error {
	body.closed = true
	return nil
}

type codexCollectionUnitDeadlineReader struct{ ctx context.Context }

func (reader codexCollectionUnitDeadlineReader) Read([]byte) (int, error) {
	<-reader.ctx.Done()
	return 0, reader.ctx.Err()
}

type codexCollectionUnitErrorReader struct{ err error }

func (reader codexCollectionUnitErrorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

func codexCollectionUnitJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

func codexCollectionUnitCompleted(t *testing.T, model string) string {
	t.Helper()
	return codexCollectionUnitJSON(t, map[string]any{"status": "completed", "model": model, "error": nil})
}

func codexCollectionUnitFrame(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

func codexCollectionUnitTemplate() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	req.Header.Set("Authorization", "Bearer synthetic-test-credential")
	return req
}

func TestCodexTurnStateCollectionProbeParsesCompleteResponses(t *testing.T) {
	const model = "gpt-6-astra"
	state := testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 12, 1)
	completeJSON := codexCollectionUnitCompleted(t, model)
	completeSSE := codexCollectionUnitFrame("response.completed", `{"response":{"model":"gpt-6-astra","status":"completed","error":null}}`)
	rate := codexCollectionUnitFrame("response.failed", `{"response":{"status":"failed","error":{"code":"rate_limit_exceeded"}}}`)
	malformed := codexCollectionUnitFrame("response.output_text.delta", `{"broken":`)
	for _, tc := range []struct {
		name, body, reason, responseModel string
		status, resultStatus              int
	}{
		{"json_null_error", completeJSON, "accepted", model, 200, 200},
		{"sse_completed", completeSSE + "data: [DONE]\n\n", "accepted", model, 200, 200},
		{"sse_incomplete", codexCollectionUnitFrame("response.created", `{"response":{"model":"gpt-6-astra","status":"in_progress"}}`) + "data: [DONE]\n\n", "incomplete_response", model, 200, 200},
		{"sse_explicit_incomplete", codexCollectionUnitFrame("response.incomplete", `{"response":{"model":"gpt-6-astra","status":"incomplete"}}`), "response_failed", model, 200, 200},
		{"sse_model_conflict", codexCollectionUnitFrame("response.created", `{"response":{"model":"gpt-5.6-luna"}}`) + completeSSE, "model_mismatch", model, 200, 200},
		{"json_model_mismatch", codexCollectionUnitCompleted(t, "gpt-5.6-luna"), "model_mismatch", "gpt-5.6-luna", 200, 200},
		{"missing_model", `{"status":"completed","error":null}`, "model_unobserved", "", 200, 200},
		{"unsafe_model", codexCollectionUnitCompleted(t, "private@example.invalid\nBearer secret"), "model_unobserved", "", 200, 200},
		{"overlong_model", codexCollectionUnitCompleted(t, strings.Repeat("x", 129)), "model_unobserved", "", 200, 200},
		{"rate_then_malformed_then_completed", rate + malformed + completeSSE, "rate_limited", model, 200, 429},
		{"malformed_then_rate_then_completed", malformed + rate + completeSSE, "rate_limited", model, 200, 429},
		{"unauthorized_then_completed", codexCollectionUnitFrame("error", `{"error":{"code":"invalid_api_key"}}`) + malformed + completeSSE, "unauthorized", model, 200, 401},
		{"forbidden_then_completed", codexCollectionUnitFrame("error", `{"error":{"code":"permission_denied"}}`) + malformed + completeSSE, "forbidden", model, 200, 403},
		{"json_error_overrides_completed", `{"status":"completed","model":"gpt-6-astra","error":{"code":"rate_limit_exceeded"}}`, "rate_limited", model, 200, 429},
		{"sse_error_overrides_completed", codexCollectionUnitFrame("response.completed", `{"response":{"status":"completed","model":"gpt-6-astra","error":{"code":"rate_limit_exceeded"}}}`), "rate_limited", model, 200, 429},
		{"http_429_ignores_completed_body", completeJSON, "rate_limited", "", 429, 429},
		{"http_401_ignores_malformed_body", "malformed", "unauthorized", "", 401, 401},
		{"http_403", completeJSON, "forbidden", "", 403, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &codexCollectionUnitBody{reader: strings.NewReader(tc.body)}
			svc := &OpenAIGatewayService{httpUpstream: &codexCollectionUnitUpstream{do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"X-Codex-Turn-State": []string{state}}, Body: body}, nil
			}}}
			result, value := svc.probeCodexTurnState(context.Background(), codexCollectionUnitTemplate(), &Account{ID: 1}, "", model, []int{332})
			require.Equal(t, tc.reason, result.Reason)
			require.Equal(t, tc.resultStatus, result.HTTPStatus)
			require.Equal(t, 332, result.ObservedLength)
			require.Equal(t, tc.responseModel, result.ResponseModel)
			require.True(t, body.closed)
			if tc.reason == "accepted" {
				require.True(t, value == state, "accepted response should return the synthetic state")
			} else {
				require.True(t, value == "", "rejected response must not return a candidate")
			}
			if tc.status != 200 {
				require.Zero(t, body.read, "transport rejection takes precedence without reading the payload")
			}
			if tc.resultStatus == 401 || tc.resultStatus == 403 || tc.resultStatus == 429 {
				req := codexCollectionUnitTemplate()
				req = req.WithContext(context.WithValue(req.Context(), codexTurnStateCollectionBlockKey{}, result))
				blocked := codexTurnStateCollectionBlockedResponse(req)
				require.NotNil(t, blocked)
				require.Equal(t, tc.resultStatus, blocked.StatusCode)
				require.NoError(t, blocked.Body.Close())
			}
		})
	}
}

func TestCodexTurnStateCollectionProbeValidatesCandidateWithoutLosingMetadata(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct{ name, state, reason string }{
		{"missing", "", "missing_state"},
		{"disallowed", testCodexTurnStateEnvelope(now, 13, 1), "length_not_allowed"},
		{"malformed", strings.Repeat("!", 332), "invalid_format"},
		{"future", testCodexTurnStateEnvelope(now.Add(time.Minute), 12, 1), "future_timestamp"},
		{"expired", testCodexTurnStateEnvelope(now.Add(-2*time.Hour), 12, 1), "expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{httpUpstream: &codexCollectionUnitUpstream{do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Codex-Turn-State": []string{tc.state}}, Body: io.NopCloser(strings.NewReader(codexCollectionUnitCompleted(t, "gpt-6-astra")))}, nil
			}}}
			result, value := svc.probeCodexTurnState(context.Background(), codexCollectionUnitTemplate(), &Account{ID: 1}, "", "gpt-6-astra", []int{332})
			require.Equal(t, tc.reason, result.Reason)
			require.Equal(t, 200, result.HTTPStatus)
			require.Equal(t, len(tc.state), result.ObservedLength)
			require.Equal(t, "gpt-6-astra", result.ResponseModel)
			require.True(t, value == "")
		})
	}
}

func TestCodexTurnStateCollectionProbeEnforcesBodyLimitAndTimeout(t *testing.T) {
	state := testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 12, 1)
	completed := codexCollectionUnitCompleted(t, "gpt-6-astra")
	for _, size := range []int{codexTurnStateProbeBodyLimit, codexTurnStateProbeBodyLimit + 1} {
		name := "exact_limit"
		if size > codexTurnStateProbeBodyLimit {
			name = "over_limit"
		}
		t.Run(name, func(t *testing.T) {
			body := &codexCollectionUnitBody{reader: strings.NewReader(completed + strings.Repeat(" ", size-len(completed)))}
			svc := &OpenAIGatewayService{httpUpstream: &codexCollectionUnitUpstream{do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Codex-Turn-State": []string{state}}, Body: body}, nil
			}}}
			result, value := svc.probeCodexTurnState(context.Background(), codexCollectionUnitTemplate(), &Account{ID: 1}, "", "gpt-6-astra", []int{332})
			if size == codexTurnStateProbeBodyLimit {
				require.Equal(t, "accepted", result.Reason)
				require.True(t, value == state)
			} else {
				require.Equal(t, "body_too_large", result.Reason)
				require.True(t, value == "")
			}
			require.LessOrEqual(t, body.read, codexTurnStateProbeBodyLimit+1)
			require.True(t, body.closed)
		})
	}
	for _, inBody := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		body := &codexCollectionUnitBody{reader: codexCollectionUnitDeadlineReader{ctx: ctx}}
		svc := &OpenAIGatewayService{httpUpstream: &codexCollectionUnitUpstream{do: func(req *http.Request) (*http.Response, error) {
			if !inBody {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"X-Codex-Turn-State": []string{state}}, Body: body}, nil
		}}}
		result, value := svc.probeCodexTurnState(ctx, codexCollectionUnitTemplate(), &Account{ID: 1}, "", "gpt-6-astra", []int{332})
		cancel()
		require.Equal(t, "timeout", result.Reason)
		require.True(t, value == "")
		if inBody {
			require.True(t, body.closed)
			require.Equal(t, 332, result.ObservedLength)
		}
	}
	svc := &OpenAIGatewayService{httpUpstream: &codexCollectionUnitUpstream{do: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("synthetic transport failure: Bearer secret")
	}}}
	result, value := svc.probeCodexTurnState(context.Background(), codexCollectionUnitTemplate(), &Account{ID: 1}, "", "gpt-6-astra", []int{332})
	require.Equal(t, "transport_error", result.Reason)
	require.True(t, value == "")
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "Bearer")
}

func TestCodexTurnStateCollectionProbeKeepsBlockingFailureAfterReadProblems(t *testing.T) {
	state := testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 12, 1)
	rate := codexCollectionUnitFrame("response.failed", `{"response":{"model":"gpt-6-astra","status":"failed","error":{"code":"rate_limit_exceeded"}}}`)
	for _, kind := range []string{"read_error", "body_limit", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			var tail io.Reader
			switch kind {
			case "read_error":
				tail = codexCollectionUnitErrorReader{err: io.ErrUnexpectedEOF}
			case "body_limit":
				tail = strings.NewReader(strings.Repeat(" ", codexTurnStateProbeBodyLimit))
			case "timeout":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Millisecond)
				defer cancel()
				tail = codexCollectionUnitDeadlineReader{ctx: ctx}
			}
			body := &codexCollectionUnitBody{reader: io.MultiReader(strings.NewReader(rate), tail)}
			svc := &OpenAIGatewayService{httpUpstream: &codexCollectionUnitUpstream{do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Codex-Turn-State": []string{state}}, Body: body}, nil
			}}}
			result, value := svc.probeCodexTurnState(ctx, codexCollectionUnitTemplate(), &Account{ID: 1}, "", "gpt-6-astra", []int{332})
			require.Equal(t, "rate_limited", result.Reason)
			require.Equal(t, 429, result.HTTPStatus)
			require.Equal(t, 332, result.ObservedLength)
			require.True(t, value == "")
			require.True(t, body.closed)
			require.LessOrEqual(t, body.read, codexTurnStateProbeBodyLimit+1)
		})
	}
}

func TestCodexTurnStateCollectionRevalidatesCurrentConfigurationBeforePublishing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Account)
	}{
		{"unchanged", nil},
		{"active_disabled", func(a *Account) { a.Extra[codexTurnStateActiveCollectionExtraKey] = false }},
		{"mode_changed", func(a *Account) { a.Extra[codexTurnStateModeExtraKey] = "observe" }},
		{"identity_changed", func(a *Account) { a.Extra[codexIdentityVersionExtraKey] = "v1" }},
		{"workspace_changed", func(a *Account) { a.Credentials["chatgpt_account_id"] = "workspace-b" }},
		{"member_changed", func(a *Account) { a.Credentials["chatgpt_user_id"] = "member-b" }},
		{"fingerprint_changed", func(a *Account) { a.Extra[codexFingerprintModeExtraKey] = "device" }},
		{"proxy_changed", func(a *Account) { a.Proxy.Host = "changed.invalid" }},
		{"lengths_changed", func(a *Account) { a.Extra[codexTurnStateCandidateLengthsExtraKey] = []int{292} }},
		{"account_disabled", func(a *Account) { a.Status = StatusDisabled }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := newTurnStateV2Account(41, "workspace-a")
			account.Status, account.Schedulable = StatusActive, true
			account.Extra[codexTurnStateModeExtraKey] = "reuse"
			account.Extra[codexTurnStateActiveCollectionExtraKey] = true
			account.Extra[codexTurnStateCandidateLengthsExtraKey] = []int{332}
			account.Credentials["chatgpt_user_id"] = "member-a"
			proxyID := int64(1)
			account.ProxyID, account.Proxy = &proxyID, &Proxy{ID: proxyID, Protocol: "http", Host: "initial.invalid", Port: 8080}
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{41: account}}
			state := testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 12, 1)
			scope := codexTurnStateCandidateScope(account, account)
			completed := codexCollectionUnitCompleted(t, "gpt-6-astra")
			var calls atomic.Int32
			svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: &codexCollectionUnitUpstream{do: func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				if tc.mutate != nil {
					repo.mu.Lock()
					tc.mutate(repo.accounts[41])
					repo.mu.Unlock()
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Codex-Turn-State": []string{state}}, Body: io.NopCloser(strings.NewReader(completed))}, nil
			}}}
			result := svc.collectCodexTurnState(codexCollectionUnitTemplate(), account, account,
				&codexTurnStateHTTPAttempt{scope: scope, model: "gpt-6-astra", accountID: 41, lengths: []int{332}})
			require.Equal(t, int32(1), calls.Load())
			snapshots := svc.openaiCodexTurnStateCandidates.Snapshot(scope, time.Now())
			require.Len(t, snapshots, 1)
			if tc.mutate == nil {
				require.Equal(t, "accepted", result.Reason)
				require.NotNil(t, snapshots[0].Candidate)
			} else {
				require.Equal(t, "configuration_changed", result.Reason)
				require.Nil(t, snapshots[0].Candidate)
			}
			require.Equal(t, 332, result.ObservedLength)
			require.Equal(t, uint64(1), snapshots[0].Collection.AttemptCount)
			require.False(t, snapshots[0].Collection.InFlight)
			require.Equal(t, result.Reason, snapshots[0].Collection.LastReason)
			require.Equal(t, 200, snapshots[0].Collection.LastHTTPStatus)
			require.Equal(t, 332, snapshots[0].Collection.LastObservedLength)
			require.Equal(t, "gpt-6-astra", snapshots[0].Collection.LastResponseModel)
		})
	}
}

func TestCodexTurnStateCollectionRevalidatesShadowCredentialHealth(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*Account)
		wantValid bool
	}{
		{"healthy", func(*Account) {}, true},
		{"parent_paused_for_scheduling", func(a *Account) { a.Schedulable = false }, true},
		{"parent_disabled", func(a *Account) { a.Status = StatusDisabled }, false},
		{"parent_credential_cooldown", func(a *Account) {
			until := time.Now().Add(time.Minute)
			a.TempUnschedulableUntil = &until
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := newTurnStateV2Account(11, "workspace-a")
			owner.Status, owner.Schedulable = StatusActive, true
			owner.Credentials["chatgpt_user_id"] = "member-a"
			owner.Extra[codexTurnStateModeExtraKey] = "reuse"
			owner.Extra[codexTurnStateActiveCollectionExtraKey] = true
			owner.Extra[codexTurnStateCandidateLengthsExtraKey] = []int{332}
			selected := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
				Schedulable: true, ParentAccountID: &owner.ID}
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{11: owner, 21: selected}}
			state := testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 12, 1)
			completed := codexCollectionUnitCompleted(t, "gpt-6-astra")
			scope := codexTurnStateCandidateScope(owner, selected)
			svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: &codexCollectionUnitUpstream{do: func(*http.Request) (*http.Response, error) {
				repo.mu.Lock()
				tc.mutate(repo.accounts[11])
				repo.mu.Unlock()
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Codex-Turn-State": []string{state}}, Body: io.NopCloser(strings.NewReader(completed))}, nil
			}}}
			result := svc.collectCodexTurnState(codexCollectionUnitTemplate(), owner, selected,
				&codexTurnStateHTTPAttempt{scope: scope, model: "gpt-6-astra", accountID: selected.ID, lengths: []int{332}})
			snapshots := svc.openaiCodexTurnStateCandidates.Snapshot(scope, time.Now())
			require.Len(t, snapshots, 1)
			if tc.wantValid {
				require.Equal(t, "accepted", result.Reason)
				require.NotNil(t, snapshots[0].Candidate)
			} else {
				require.Equal(t, "configuration_changed", result.Reason)
				require.Nil(t, snapshots[0].Candidate)
			}
		})
	}
}
