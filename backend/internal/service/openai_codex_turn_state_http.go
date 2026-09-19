package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const codexTurnStateHTTPContextKey = "openai_codex_turn_state_http_attempt"
const codexTurnStateClientIntentKey = "openai_codex_turn_state_client_continuation"
const codexTurnStateClientIntentReasonKey = "openai_codex_turn_state_client_intent_reason"

// Forward may remove a client continuation ID when normalizing an HTTP body.
// Remember the client's intent before that transformation so candidate reuse
// cannot silently turn a continuation into a different upstream turn.
func recordCodexTurnStateClientIntent(c *gin.Context, body []byte) {
	if c == nil {
		return
	}
	continuation := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) != ""
	reason := ""
	if continuation {
		reason = "client_continuation"
	}
	gjson.GetBytes(body, "client_metadata").ForEach(func(key, value gjson.Result) bool {
		if !continuation && strings.EqualFold(key.String(), openAICodexTurnStateHeader) && strings.TrimSpace(value.String()) != "" {
			continuation = true
			reason = "client_metadata_state"
		}
		return !continuation
	})
	c.Set(codexTurnStateClientIntentKey, continuation)
	c.Set(codexTurnStateClientIntentReasonKey, reason)
}

type codexTurnStateHTTPRequestKey struct{}

// Each HTTP attempt has its own immutable attribution. Never infer a model from
// the client alias, a previous attempt, or a WebSocket handshake shared by turns.
type codexTurnStateHTTPAttempt struct {
	scope     string
	model     string
	accountID int64
	lengths   []int
	injected  string
}

// CodexTurnStateStatus contains metadata only. Raw states stay in the bounded
// process-local cache and must never be exposed through admin APIs or logs.
type CodexTurnStateStatus struct {
	Mode             string                        `json:"mode"`
	IdentityVersion  string                        `json:"identity_version"`
	CandidateLengths []int                         `json:"candidate_lengths"`
	ProcessLocal     bool                          `json:"process_local"`
	Models           []CodexTurnStateModelSnapshot `json:"models"`
}

func codexTurnStateCandidateScope(source, selected *Account) string {
	if source == nil || selected == nil || !source.IsOpenAIOAuthLike() || source.ID <= 0 {
		return ""
	}
	seed, _ := codexFingerprintSeed(selected.Extra)
	proxyURL := ""
	if selected.ProxyID != nil && selected.Proxy != nil {
		proxyURL = selected.Proxy.URL()
	}
	// Length/mode settings do not change identity. Selection checks the current
	// length allowlist again, so narrowing it immediately disables old candidates.
	identity, _ := json.Marshal([]any{
		openAICodexTurnStateOwner(nil, source), source.ID,
		selected.GetCodexFingerprintMode(), seed, selected.GetOpenAIDeviceID(),
		selected.GetOpenAIUserAgent(), selected.ProxyID, proxyURL,
	})
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:])
}

func (s *OpenAIGatewayService) CodexTurnStateStatus(ctx context.Context, account *Account) CodexTurnStateStatus {
	status := CodexTurnStateStatus{Mode: "off", IdentityVersion: "v1", CandidateLengths: []int{}, ProcessLocal: true, Models: []CodexTurnStateModelSnapshot{}}
	if s == nil || account == nil {
		return status
	}
	source, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil || source == nil {
		return status
	}
	status.Mode = source.GetCodexTurnStateMode()
	status.IdentityVersion = source.GetCodexIdentityVersion()
	status.CandidateLengths = source.GetCodexTurnStateCandidateLengths()
	if status.CandidateLengths == nil {
		status.CandidateLengths = []int{}
	}
	status.Models = s.openaiCodexTurnStateCandidates.Snapshot(codexTurnStateCandidateScope(source, account), time.Now())
	if status.Models == nil {
		status.Models = []CodexTurnStateModelSnapshot{}
	}
	return status
}

func (s *OpenAIGatewayService) ClearCodexTurnState(ctx context.Context, account *Account) {
	if s == nil || account == nil {
		return
	}
	source, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err == nil && source != nil {
		s.openaiCodexTurnStateCandidates.Clear(codexTurnStateCandidateScope(source, account))
	}
}

func (s *OpenAIGatewayService) prepareCodexTurnStateHTTP(c *gin.Context, account *Account, body []byte, req *http.Request) *http.Request {
	if c == nil || req == nil || s == nil {
		return req
	}
	// Gin contexts are reused across failover attempts. Always reset this slot,
	// including attempts whose new credential has the feature switched off.
	c.Set(codexTurnStateHTTPContextKey, (*codexTurnStateHTTPAttempt)(nil))
	c.Set(codexTurnStateHTTPDiagnosticsKey, (*codexTurnStateRequestAttempt)(nil))
	source := codexAccountIdentitySource(c, account)
	if source == nil || account == nil || source.IsOpenAIAgentIdentity() || !account.UsesOpenAICodexProtocol() || source.GetCodexTurnStateMode() == "off" {
		return req
	}
	modelValue := gjson.GetBytes(body, "model")
	model := strings.TrimSpace(modelValue.String())
	scope := codexTurnStateCandidateScope(source, account)
	if !gjson.ValidBytes(body) || modelValue.Type != gjson.String || model == "" || len(model) > 128 || scope == "" {
		return req
	}
	// Compact and compatibility bridges have their own continuation semantics.
	// Keep their existing state priority and do not mix their candidates into the
	// native Responses store. Native WS is likewise unchanged in this rollout.
	if isOpenAIResponsesCompactPath(c) || isOpenAICompatMessagesBridgeContext(c) || isOpenAICompatMessagesBridgeBody(body) ||
		(c.Request != nil && !strings.HasSuffix(strings.TrimRight(c.Request.URL.Path, "/"), "/responses")) {
		return req
	}
	attempt := &codexTurnStateHTTPAttempt{scope: scope, model: model, accountID: account.ID, lengths: source.GetCodexTurnStateCandidateLengths()}
	c.Set(codexTurnStateHTTPContextKey, attempt)
	selectionReason := "no_candidate"
	defer func() {
		s.recordCodexTurnStateHTTPSelection(c, attempt, body, req, selectionReason)
	}()
	// A client-supplied state (even one stripped by the provenance guard) and a
	// response-ID continuation are not invitations to choose another turn state.
	if source.GetCodexTurnStateMode() != "reuse" {
		selectionReason = "observe_mode"
		return req
	}
	if c.GetBool(codexTurnStateClientIntentKey) ||
		strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) != "" {
		selectionReason = "client_continuation"
		if c.GetString(codexTurnStateClientIntentReasonKey) == "client_metadata_state" {
			selectionReason = "client_metadata_state"
		}
		return req
	}
	if req.Header.Get(openAICodexTurnStateHeader) != "" ||
		(c.Request != nil && strings.TrimSpace(c.GetHeader(openAICodexTurnStateHeader)) != "") {
		selectionReason = "client_state"
		return req
	}
	hasFrameState := false
	gjson.GetBytes(body, "client_metadata").ForEach(func(key, value gjson.Result) bool {
		if strings.EqualFold(key.String(), openAICodexTurnStateHeader) && strings.TrimSpace(value.String()) != "" {
			hasFrameState = true
		}
		return !hasFrameState
	})
	if hasFrameState {
		selectionReason = "client_metadata_state"
		return req
	}
	// Check the active allowlist and count the selected candidate under one lock.
	value, ok, reason := s.openaiCodexTurnStateCandidates.SelectCandidateWithReason(scope, model, attempt.lengths, time.Now())
	selectionReason = reason
	if ok {
		req.Header.Set(openAICodexTurnStateHeader, value)
		attempt.injected = value
		return req.WithContext(context.WithValue(req.Context(), codexTurnStateHTTPRequestKey{}, *attempt))
	}
	return req
}

func (s *OpenAIGatewayService) observeCodexTurnStateHTTP(c *gin.Context, account *Account, value string) {
	if s == nil || c == nil || account == nil || value == "" {
		return
	}
	raw, _ := c.Get(codexTurnStateHTTPContextKey)
	attempt, ok := raw.(*codexTurnStateHTTPAttempt)
	if !ok || attempt == nil || attempt.accountID != account.ID {
		return
	}
	source := codexAccountIdentitySource(c, account)
	if source == nil || source.GetCodexTurnStateMode() == "off" || attempt.scope != codexTurnStateCandidateScope(source, account) {
		return
	}
	if attempt.injected != "" && value == attempt.injected {
		// A replayed value cannot refresh its own lifetime. A distinct state
		// still passes the normal length, envelope and issuance-time checks.
		s.openaiCodexTurnStateCandidates.ObserveEcho(attempt.scope, attempt.model, value, time.Now())
		return
	}
	s.openaiCodexTurnStateCandidates.Observe(attempt.scope, attempt.model, value, attempt.lengths, time.Now())
}

func (s *OpenAIGatewayService) rejectCodexTurnStateHTTP(req *http.Request, resp *http.Response) {
	if s == nil || req == nil || resp == nil || resp.StatusCode < 400 || resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return
	}
	if attempt, ok := req.Context().Value(codexTurnStateHTTPRequestKey{}).(codexTurnStateHTTPAttempt); ok && attempt.injected != "" {
		// Let the existing error/failover pipeline handle the response. No extra
		// request is sent, and a concurrently replaced candidate survives.
		s.openaiCodexTurnStateCandidates.Invalidate(attempt.scope, attempt.model, attempt.injected)
	}
}
