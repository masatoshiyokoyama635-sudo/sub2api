package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const codexTurnStateHTTPDiagnosticsKey = "openai_codex_turn_state_http_diagnostics"

// CodexTurnStateRequestSnapshot correlates one completed HTTP attempt with the
// state metadata actually sent upstream. It never retains headers or state
// values. ResponseModelObserved distinguishes missing declarations from a match.
type CodexTurnStateRequestSnapshot struct {
	At                    time.Time `json:"at"`
	RequestID             string    `json:"request_id,omitempty"`
	SelectionReason       string    `json:"selection_reason"`
	StateSource           string    `json:"state_source"`
	OutboundStateLength   int       `json:"outbound_state_length"`
	UpstreamResponseModel string    `json:"upstream_response_model,omitempty"`
	ResponseModelObserved bool      `json:"response_model_observed"`
	ModelMismatch         bool      `json:"model_mismatch"`
	Failed                bool      `json:"failed"`
}

type codexTurnStateRequestAttempt struct {
	scope     string
	model     string
	accountID int64
	snapshot  CodexTurnStateRequestSnapshot
}

func (s *OpenAIGatewayService) recordCodexTurnStateHTTPSelection(c *gin.Context, attempt *codexTurnStateHTTPAttempt, body []byte, req *http.Request, reason string) {
	if s == nil || c == nil || attempt == nil || req == nil {
		return
	}
	snapshot := CodexTurnStateRequestSnapshot{SelectionReason: reason, StateSource: "none"}
	blocked := reason == "collection_cooldown" || reason == "collection_rejected" || reason == "collection_pending"
	state := req.Header.Get(openAICodexTurnStateHeader)
	if strings.TrimSpace(state) == "" {
		gjson.GetBytes(body, "client_metadata").ForEach(func(key, value gjson.Result) bool {
			if strings.EqualFold(key.String(), openAICodexTurnStateHeader) && value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
				state = value.String()
				return false
			}
			return true
		})
	}
	if state != "" && !blocked {
		snapshot.OutboundStateLength = len(state)
		snapshot.StateSource = "client"
		if attempt.injected != "" && state == attempt.injected {
			snapshot.StateSource = "candidate"
		}
	}
	c.Set(codexTurnStateHTTPDiagnosticsKey, &codexTurnStateRequestAttempt{
		scope: attempt.scope, model: attempt.model, accountID: attempt.accountID, snapshot: snapshot,
	})
	// Cache lookups already record their outcome atomically. Only earlier HTTP
	// gates need a separate selection event; none of these increments reuse.
	switch reason {
	case "observe_mode", "client_state", "client_continuation", "client_metadata_state", "collection_cooldown", "collection_rejected", "collection_pending":
		s.openaiCodexTurnStateCandidates.RecordSelection(attempt.scope, attempt.model, reason, time.Now())
	}
}

func (s *OpenAIGatewayService) finishCodexTurnStateHTTPRequest(ctx context.Context, c *gin.Context, account *Account, result *OpenAIForwardResult, forwardError error) {
	if s == nil || c == nil || account == nil {
		return
	}
	value, _ := c.Get(codexTurnStateHTTPDiagnosticsKey)
	attempt, ok := value.(*codexTurnStateRequestAttempt)
	if !ok || attempt == nil || attempt.accountID != account.ID {
		return
	}
	snapshot := attempt.snapshot
	snapshot.At = time.Now().UTC()
	snapshot.Failed = forwardError != nil
	requestID := ""
	if result != nil {
		requestID = result.RequestID
	}
	snapshot.RequestID = codexTurnStateBoundedLabel(resolveUsageBillingRequestID(ctx, requestID), 128)
	snapshot.UpstreamResponseModel = codexTurnStateBoundedLabel(observedUpstreamResponseModel(c), openAICodexTurnStateCandidateMaxModelBytes)
	snapshot.ResponseModelObserved = snapshot.UpstreamResponseModel != ""
	snapshot.ModelMismatch = snapshot.ResponseModelObserved && !upstreamModelsMatchForAudit(attempt.model, snapshot.UpstreamResponseModel)
	s.openaiCodexTurnStateCandidates.RecordRequest(attempt.scope, attempt.model, snapshot)
	fields := []zap.Field{
		zap.Int64("account_id", account.ID),
		zap.String("request_id", snapshot.RequestID),
		zap.String("sent_model", attempt.model),
		zap.String("state_selection", snapshot.SelectionReason),
		zap.String("state_source", snapshot.StateSource),
		zap.Int("outbound_state_length", snapshot.OutboundStateLength),
		zap.String("upstream_response_model", snapshot.UpstreamResponseModel),
		zap.Bool("response_model_observed", snapshot.ResponseModelObserved),
		zap.Bool("model_mismatch", snapshot.ModelMismatch),
		zap.Bool("failed", snapshot.Failed),
	}
	if snapshot.ModelMismatch {
		logger.L().Warn("codex_turn_state_request", fields...)
	} else {
		logger.L().Info("codex_turn_state_request", fields...)
	}
}
