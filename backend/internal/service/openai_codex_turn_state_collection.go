package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const codexTurnStateProbeBodyLimit = 1 << 20

type codexTurnStateCollectionBlockKey struct{}

// collectCodexTurnState uses only a detached outbound template and account
// snapshot. The shared probe may outlive a caller, so it must never retain Gin.
func (s *OpenAIGatewayService) collectCodexTurnState(req *http.Request, source, account *Account, attempt *codexTurnStateHTTPAttempt) codexTurnStateCollectionResult {
	if s == nil || req == nil || source == nil || account == nil || attempt == nil ||
		!source.GetCodexTurnStateActiveCollectionEnabled() || len(attempt.lengths) == 0 {
		return codexTurnStateCollectionResult{Reason: "disabled"}
	}
	if req.Method != http.MethodPost || req.URL == nil || req.URL.Scheme != "https" ||
		req.URL.Hostname() != "chatgpt.com" || req.URL.Path != "/backend-api/codex/responses" || req.Header.Get("Authorization") == "" {
		return codexTurnStateCollectionResult{Reason: "invalid_request"}
	}
	selected := *account
	selected.Extra = maps.Clone(account.Extra)
	selected.Credentials = maps.Clone(account.Credentials)
	if account.Proxy != nil {
		proxy := *account.Proxy
		selected.Proxy = &proxy
	}
	if account.ProxyID != nil {
		proxyID := *account.ProxyID
		selected.ProxyID = &proxyID
	}
	owner := *source
	owner.Extra = maps.Clone(source.Extra)
	owner.Credentials = maps.Clone(source.Credentials)
	scope, model := attempt.scope, attempt.model
	lengths := append([]int(nil), attempt.lengths...)
	template := req.Clone(context.WithoutCancel(req.Context()))
	template.Body, template.GetBody = nil, nil
	credentialDigest := sha256.Sum256([]byte(template.Header.Get("Authorization")))
	proxyURL := ""
	if selected.ProxyID != nil && selected.Proxy != nil {
		proxyURL = selected.Proxy.URL()
	}
	result, _, _ := s.openaiCodexTurnStateCollection.Run(req.Context(), source.ID, scope+"\x00"+model,
		hex.EncodeToString(credentialDigest[:]), func(ctx context.Context) (result codexTurnStateCollectionResult) {
			s.openaiCodexTurnStateCandidates.RecordCollectionStart(scope, model, time.Now())
			result.Reason = "collection_failed"
			defer func() {
				finished := time.Now().UTC()
				s.openaiCodexTurnStateCandidates.RecordCollectionResult(scope, model, CodexTurnStateCollectionSnapshot{
					LastFinishedAt: &finished, LastReason: result.Reason, LastHTTPStatus: result.HTTPStatus,
					LastObservedLength: result.ObservedLength, LastResponseModel: result.ResponseModel,
				})
			}()
			// Active collection deliberately performs one probe per generation
			// attempt.  The proxy URL is the account's configured endpoint; Close
			// on the probe below forces a fresh transport connection for endpoints
			// that rotate their exit IP per request without changing account routing.
			result, value := s.probeCodexTurnState(ctx, template, &selected, proxyURL, model, lengths)
			if result.Reason != "accepted" {
				return result
			}
			// Account edits made during the detached request cannot publish a
			// candidate into an obsolete identity or a newly disabled policy.
			if !s.codexTurnStateCollectionStillEnabled(ctx, &owner, &selected, scope, lengths) {
				result.Reason = "configuration_changed"
				return result
			}
			s.openaiCodexTurnStateCandidates.Observe(scope, model, value, lengths, time.Now())
			return result
		})
	return result
}

func (s *OpenAIGatewayService) codexTurnStateCollectionStillEnabled(ctx context.Context, owner, account *Account, scope string, lengths []int) bool {
	current, source := account, owner
	if s.accountRepo != nil {
		var err error
		current, err = s.accountRepo.GetByID(ctx, account.ID)
		if err != nil || current == nil || !current.IsSchedulable() {
			return false
		}
		source, err = resolveCredentialAccount(ctx, s.accountRepo, current)
		if err != nil || source == nil {
			return false
		}
		if source.ID != current.ID && !source.IsCredentialUsableForShadow() {
			return false
		}
	}
	if !source.GetCodexTurnStateActiveCollectionEnabled() || codexTurnStateCandidateScope(source, current) != scope {
		return false
	}
	allowed := source.GetCodexTurnStateCandidateLengths()
	if len(allowed) != len(lengths) {
		return false
	}
	for i := range allowed {
		if allowed[i] != lengths[i] {
			return false
		}
	}
	return ctx.Err() == nil
}

func (s *OpenAIGatewayService) probeCodexTurnState(ctx context.Context, template *http.Request, account *Account, proxyURL, model string, lengths []int) (codexTurnStateCollectionResult, string) {
	result := codexTurnStateCollectionResult{Reason: "transport_error"}
	body, err := json.Marshal(map[string]any{
		"model": model, "stream": true, "store": false,
		"instructions": "Reply with OK.",
		"input": []any{map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "Reply with OK."},
		}}},
	})
	if err != nil {
		result.Reason = "collection_failed"
		return result, ""
	}
	probe, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		return result, ""
	}
	// Preserve credential/device identity only. User prompts, cookies, state,
	// thread/session identifiers, cache keys and routing hints are not copied.
	for _, key := range []string{"Authorization", "Chatgpt-Account-Id", "Chatgpt-User-Id", "User-Agent", "Version", "Originator", "Openai-Beta", "Accept-Language", "Oai-Device-Id", "X-Codex-Installation-Id"} {
		if value := template.Header.Get(key); value != "" {
			probe.Header.Set(key, value)
		}
	}
	probe.Header.Set("Content-Type", "application/json")
	probe.Header.Set("Accept", "text/event-stream")
	probe.Header.Set("Accept-Encoding", "identity")
	probe = probe.WithContext(WithHTTPUpstreamProfile(probe.Context(), HTTPUpstreamProfileOpenAI))
	// req.Close is intentional: a rotating endpoint changes its exit on a new
	// CONNECT/HTTP connection. The plugin transport may pool connections and
	// ignore this signal, so active collection uses the native transport port.
	probe.Close = true
	if s.httpUpstream == nil {
		result.Reason = "transport_error"
		return result, ""
	}
	resp, err := s.httpUpstream.Do(probe, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Reason = "timeout"
		}
		return result, ""
	}
	if resp == nil || resp.Body == nil {
		return result, ""
	}
	defer func() { _ = resp.Body.Close() }()
	result.HTTPStatus = resp.StatusCode
	value := extractOpenAICodexTurnState(resp.Header)
	result.ObservedLength = len(value)
	result.RetryAfter = codexTurnStateProbeRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			result.Reason = "unauthorized"
		case http.StatusForbidden:
			result.Reason = "forbidden"
		case http.StatusTooManyRequests:
			result.Reason = "rate_limited"
		default:
			result.Reason = "upstream_error"
		}
		return result, ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, codexTurnStateProbeBodyLimit+1))
	completed, failure, observer := inspectCodexTurnStateProbeResponse(data)
	result.ResponseModel = codexTurnStateBoundedLabel(observer.Model(), openAICodexTurnStateCandidateMaxModelBytes)
	// A rejection already received in a partial stream remains authoritative
	// even if a later read fails, times out, or exceeds the response-size limit.
	switch failure {
	case "unauthorized":
		result.Reason, result.HTTPStatus = failure, http.StatusUnauthorized
		return result, ""
	case "forbidden":
		result.Reason, result.HTTPStatus = failure, http.StatusForbidden
		return result, ""
	case "rate_limited":
		result.Reason, result.HTTPStatus = failure, http.StatusTooManyRequests
		return result, ""
	}
	if len(data) > codexTurnStateProbeBodyLimit {
		result.Reason = "body_too_large"
		return result, ""
	}
	if err != nil || ctx.Err() != nil {
		result.Reason = "incomplete_response"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Reason = "timeout"
		}
		return result, ""
	}
	if failure != "" {
		result.Reason = failure
		return result, ""
	}
	if !completed {
		result.Reason = "incomplete_response"
		return result, ""
	}
	if result.ResponseModel == "" {
		result.Reason = "model_unobserved"
		return result, ""
	}
	if observer.Conflict() || !upstreamModelsMatchForAudit(model, result.ResponseModel) {
		result.Reason = "model_mismatch"
		return result, ""
	}
	if value == "" {
		result.Reason = "missing_state"
		return result, ""
	}
	allowed := false
	for _, length := range lengths {
		allowed = allowed || length == len(value)
	}
	if !allowed {
		result.Reason = "length_not_allowed"
		return result, ""
	}
	if _, _, reason := inspectOpenAICodexTurnStateCandidate(value, time.Now()); reason != "" {
		result.Reason = reason
		return result, ""
	}
	result.Reason = "accepted"
	return result, value
}

func inspectCodexTurnStateProbeResponse(data []byte) (bool, string, *upstreamResponseModelObserver) {
	observer := &upstreamResponseModelObserver{}
	completed, failure := false, ""
	recordFailure := func(reason string) {
		// Once an authentication/access/rate rejection has been seen, later
		// malformed frames or a completed frame cannot turn it into fallback.
		if failure == "unauthorized" || failure == "forbidden" || failure == "rate_limited" {
			return
		}
		failure = reason
	}
	inspect := func(event string, payload []byte) {
		if !gjson.ValidBytes(payload) {
			recordFailure("response_failed")
			return
		}
		observer.ObserveOpenAI(payload, event)
		status := gjson.GetBytes(payload, "response.status").String()
		if event == "response.completed" && (status == "" || status == "completed") {
			completed = true
		}
		errorValue := gjson.GetBytes(payload, "response.error")
		if !errorValue.Exists() || errorValue.Type == gjson.Null {
			errorValue = gjson.GetBytes(payload, "error")
		}
		hasError := errorValue.Exists() && errorValue.Type != gjson.Null
		if hasError || event == "error" || event == "response.failed" || event == "response.incomplete" || event == "response.cancelled" || event == "response.canceled" || status == "failed" || status == "incomplete" {
			code := firstValidTrimmedGJSONString(payload, "response.error.code", "error.code", "code")
			recordFailure(codexTurnStateProbeFailureCode(code))
		}
	}
	if gjson.ValidBytes(data) {
		observer.ObserveOpenAI(data, "")
		completed = gjson.GetBytes(data, "status").String() == "completed"
		if errorValue := gjson.GetBytes(data, "error"); errorValue.Exists() && errorValue.Type != gjson.Null {
			recordFailure(codexTurnStateProbeFailureCode(firstValidTrimmedGJSONString(data, "error.code", "code")))
		}
	} else {
		forEachOpenAISSEFrame(string(data), inspect)
	}
	return completed, failure, observer
}

func codexTurnStateProbeFailureCode(code string) string {
	switch code {
	case "rate_limit_exceeded", "insufficient_quota":
		return "rate_limited"
	case "invalid_api_key", "invalid_access_token", "token_expired", "authentication_error":
		return "unauthorized"
	case "permission_denied", "access_denied":
		return "forbidden"
	default:
		return "response_failed"
	}
}

func codexTurnStateProbeRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 && seconds <= 7*24*60*60 {
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) && deadline.Sub(now) <= 7*24*time.Hour {
		return deadline.Sub(now)
	}
	return 0
}

// A rejected probe stops this account attempt before the user's generation is
// sent. Existing upstream-error handling sees the actual rejection status.
func codexTurnStateCollectionBlockedResponse(req *http.Request) *http.Response {
	if req == nil {
		return nil
	}
	result, ok := req.Context().Value(codexTurnStateCollectionBlockKey{}).(codexTurnStateCollectionResult)
	if !ok || (!result.GenerationBlocked && result.HTTPStatus != 401 && result.HTTPStatus != 403 && result.HTTPStatus != 429) {
		return nil
	}
	status := result.HTTPStatus
	body := `{"error":{"type":"active_collection_rejected","message":"Active state collection was rejected by the upstream; the generation request was not sent to this account."}}`
	if status != 401 && status != 403 && status != 429 {
		status = http.StatusServiceUnavailable
		body = `{"error":{"type":"active_collection_pending","message":"Active state collection did not finish before the wait ended; the generation request was not sent to this account."}}`
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	if wait := time.Until(result.NextAllowedAt); wait > 0 {
		header.Set("Retry-After", strconv.FormatInt(int64(wait/time.Second)+1, 10))
	}
	return &http.Response{StatusCode: status, Header: header, Request: req,
		Body: io.NopCloser(strings.NewReader(body))}
}
