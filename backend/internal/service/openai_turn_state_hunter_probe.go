package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (s *OpenAIGatewayService) probeCodexHunter(ctx context.Context, account *Account, model string, cfg openAITurnStateHunterConfig, proxy *Proxy) openAITurnStateHuntAttempt {
	attempt := openAITurnStateHuntAttempt{At: time.Now().UTC(), Model: model}
	if proxy != nil {
		attempt.ProxyID = proxy.ID
		attempt.Proxy = "#" + strconv.FormatInt(proxy.ID, 10)
	}
	if !codexHunterProbeEligible(s, account, proxy) || strings.TrimSpace(model) == "" || s.httpUpstream == nil {
		attempt.Error = "invalid_configuration"
		attempt.preflight = true
		return attempt
	}
	ctx, cancel := context.WithTimeout(ctx, openAITurnStateHuntProbeTimeout)
	defer cancel()
	// The caller's account snapshot and normal proxy binding remain untouched.
	selected := *account
	selected.Extra = maps.Clone(account.Extra)
	selected.Credentials = maps.Clone(account.Credentials)
	probeProxy := proxy
	if probeProxy == nil {
		probeProxy = account.Proxy
	}
	proxyURL := ""
	if proxy != nil || account.ProxyID != nil {
		if probeProxy == nil || !probeProxy.IsActive() || probeProxy.IsExpired(time.Now()) {
			attempt.Error = "proxy_unavailable"
			attempt.preflight = true
			return attempt
		}
		proxyURL = probeProxy.URL()
		if _, _, err := proxyurl.Parse(proxyURL); err != nil {
			attempt.Error = "invalid_proxy"
			attempt.preflight = true
			return attempt
		}
	}
	token, _, err := s.GetAccessToken(ctx, &selected)
	if err != nil || token == "" {
		attempt.Error = "credential_unavailable"
		attempt.preflight = true
		return attempt
	}
	session := uuid.NewString()
	body := map[string]any{"model": model, "stream": true, "store": false, "instructions": "Reply with OK.", "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Reply with OK."}}}}, "reasoning": map[string]any{"effort": cfg.ReasoningEffort}, "client_metadata": map[string]any{"session_id": session, "thread_id": session, "turn_id": uuid.NewString()}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	c.Set(ctxKeyTurnStateProbe, true)
	c.Request.Header.Set("Session-Id", session)
	c.Request.Header.Set("User-Agent", CodexCanonicalUserAgent())
	if _, err = s.prepareCodexAccountIdentitySource(ctx, c, &selected); err != nil {
		attempt.Error = "identity_unavailable"
		attempt.preflight = true
		return attempt
	}
	applyCodexIdentityV2Map(c, &selected, body)
	encoded, err := json.Marshal(body)
	if err != nil {
		attempt.Error = "invalid_request"
		attempt.preflight = true
		return attempt
	}
	req, err := s.buildUpstreamRequest(ctx, c, &selected, encoded, token, true, "", true)
	if err != nil {
		attempt.Error = "build_failed"
		attempt.preflight = true
		return attempt
	}
	if req.URL == nil || req.URL.Scheme != "https" || req.URL.Hostname() != "chatgpt.com" || req.URL.Path != "/backend-api/codex/responses" {
		attempt.Error = "unsupported_upstream"
		attempt.preflight = true
		return attempt
	}
	req.Header.Del(openAICodexTurnStateHeader)
	req.Header.Set("Accept-Encoding", "identity")
	req.Close = true
	req = req.WithContext(WithHTTPUpstreamFreshConnection(WithHTTPUpstreamRedirectsDisabled(req.Context())))
	started := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	attempt.LatencyMs = time.Since(started).Milliseconds()
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		attempt.transport = true
		attempt.Error = "transport_error"
		return attempt
	}
	if resp == nil || resp.Body == nil {
		attempt.transport = true
		attempt.Error = "empty_response"
		return attempt
	}
	defer resp.Body.Close()
	attempt.Status = resp.StatusCode
	attempt.RetryAfter = codexTurnStateProbeRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	if resp.StatusCode != http.StatusOK {
		attempt.Error = "http_" + http.StatusText(resp.StatusCode)
		return attempt
	}
	value := extractOpenAICodexTurnState(resp.Header)
	attempt.Chars = len(value)
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, codexTurnStateProbeBodyLimit+1))
	_ = resp.Body.Close()
	completed, failure, observer := inspectCodexTurnStateProbeResponse(data)
	attempt.ResponseModel = codexTurnStateBoundedLabel(observer.Model(), 128)
	usage, hasUsage := extractOpenAIUsageFromJSONBytes(data)
	if !hasUsage {
		forEachOpenAISSEFrame(string(data), func(_ string, payload []byte) {
			if u, ok := extractOpenAIUsageFromJSONBytes(payload); ok {
				usage = u
			}
		})
	}
	if s.openaiTurnStateHunter != nil {
		defer func() {
			result := &OpenAIForwardResult{RequestID: "turn_state_probe:" + uuid.NewString(), Usage: usage, Model: model, UpstreamResponseModel: attempt.ResponseModel, UpstreamResponseModelConflict: observer.Conflict(), UpstreamResponseServiceTier: observer.ServiceTier(), UpstreamHeaders: resp.Header, Stream: true, Duration: time.Since(started), ReasoningEffort: &cfg.ReasoningEffort, UpstreamEndpoint: req.URL.Path}
			// A response timeout can follow an already received usage event.
			// Keep billing bounded while preserving those reported tokens after
			// the probe context has been canceled.
			billingCtx, billingCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer billingCancel()
			if err := s.openaiTurnStateHunter.recordCodexHunterUsage(billingCtx, account, cfg, result); err != nil {
				slog.Warn("openai_turn_state_probe_usage_record_failed", "account_id", account.ID, "api_key_id", cfg.UsageAPIKeyID, "reason", "billing_unavailable")
			}
		}()
	}
	if failure != "" {
		attempt.Error = failure
		switch failure {
		case "unauthorized":
			attempt.Status = 401
		case "forbidden":
			attempt.Status = 403
		case "rate_limited":
			attempt.Status = 429
		}
		return attempt
	}
	if readErr != nil || ctx.Err() != nil || len(data) > codexTurnStateProbeBodyLimit || !completed {
		attempt.Error = "incomplete_response"
		return attempt
	}
	if observer.Model() == "" || observer.Conflict() || !upstreamModelsMatchForAudit(model, observer.Model()) {
		attempt.Error = "model_mismatch"
		return attempt
	}
	if !codexHunterValidState(account, value, time.Now()) {
		if _, _, ok := parseOpenAICodexTurnStateCandidate(value, time.Now()); !ok {
			attempt.Error = "invalid_state"
		}
		return attempt
	}
	if !s.codexHunterProbeConfigurationCurrent(ctx, account, proxy) {
		attempt.Error = "configuration_changed"
		return attempt
	}
	// Recovery reports whether the account's own exit works; it is independent
	// of publishing a candidate from an administrator-selected hunt proxy.
	if proxy != nil {
		issued, expires, _ := parseOpenAICodexTurnStateCandidate(value, time.Now())
		candidate := CodexHunterCandidate{Value: value, ProbeProxyID: proxy.ID, ProxyIdentity: codexHunterProxyIdentity(proxy), IssuedUnix: issued.Unix(), ExpiresUnix: expires.Unix()}
		if err := s.hunterCandidateStore().PutCodexHunterCandidate(ctx, codexHunterStoreKey(account, model), candidate); err != nil {
			attempt.Error = "candidate_store_failed"
			return attempt
		}
		// A compare-and-set store may retain a newer value or refuse a late
		// write after a rejection tombstone. Only report success if a candidate
		// remains usable, and mirror the value the store actually retained.
		retained := s.codexHunterCandidate(ctx, account, model)
		if retained == nil {
			attempt.Error = "candidate_not_retained"
			return attempt
		}
		s.openaiCodexTurnStateCandidates.Observe(codexHunterScope(account), model, retained.Value, account.GetCodexTurnStateCandidateLengths(), time.Now())
	}
	attempt.Healthy = true
	return attempt
}

func (s *OpenAIGatewayService) codexHunterProbeConfigurationCurrent(ctx context.Context, account *Account, proxy *Proxy) bool {
	if s.accountRepo == nil {
		return true
	}
	latest, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || latest == nil || latest.Status != StatusActive || !codexHunterProbeEligible(s, latest, proxy) || codexHunterScope(latest) != codexHunterScope(account) {
		return false
	}
	if proxy == nil {
		a, _ := json.Marshal(account.Extra[openAITurnStateRecoveryExtraKey])
		b, _ := json.Marshal(latest.Extra[openAITurnStateRecoveryExtraKey])
		cfg, _ := readOpenAITurnStateRecoveryConfig(latest)
		return cfg.Enabled && bytes.Equal(a, b)
	}
	if !latest.IsOpenAITurnStateHunterEnabled() {
		return false
	}
	if s.openaiTurnStateHunter != nil && s.openaiTurnStateHunter.proxyRepo != nil {
		rows, err := s.openaiTurnStateHunter.proxyRepo.ListByIDs(ctx, []int64{proxy.ID})
		return err == nil && len(rows) == 1 && rows[0].IsActive() && !rows[0].IsExpired(time.Now()) && codexHunterProxyIdentity(&rows[0]) == codexHunterProxyIdentity(proxy)
	}
	return true
}

func codexHunterProbeEligible(s *OpenAIGatewayService, a *Account, proxy *Proxy) bool {
	if proxy != nil {
		return s.codexHunterReusable(a)
	}
	return a != nil && a.IsOpenAIOAuthLike() && !a.IsOpenAIAgentIdentity() && !a.IsCredentialShadow() && a.GetCodexIdentityVersion() == "v2" && a.GetCodexTurnStateMode() != "off"
}
