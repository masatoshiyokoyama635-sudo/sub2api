package admin

import (
	"context"
	"net/http"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexTurnStateObserver interface {
	CodexTurnStateStatus(context.Context, *service.Account) service.CodexTurnStateStatus
	ClearCodexTurnState(context.Context, *service.Account)
}

// SetCodexTurnStateService shares the gateway's process-local observations with
// authenticated account administration. Neither operation contacts the upstream.
func (h *AccountHandler) SetCodexTurnStateService(gateway *service.OpenAIGatewayService) {
	if gateway != nil {
		h.codexTurnState = gateway
	}
}

// GetCodexTurnState returns only aggregate observations and candidate metadata.
// Raw state values and account credentials must never enter this response.
func (h *AccountHandler) GetCodexTurnState(c *gin.Context) {
	account, err := h.codexTurnStateAccount(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.codexTurnState == nil {
		response.Error(c, http.StatusServiceUnavailable, "Turn-state observations are unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, h.codexTurnState.CodexTurnStateStatus(c.Request.Context(), account))
}

// ClearCodexTurnState clears candidates and observations for the selected
// credential's current identity configuration in this process. Account settings
// and candidates isolated under older identity configurations remain unchanged.
func (h *AccountHandler) ClearCodexTurnState(c *gin.Context) {
	account, err := h.codexTurnStateAccount(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.codexTurnState == nil {
		response.Error(c, http.StatusServiceUnavailable, "Turn-state observations are unavailable")
		return
	}
	h.codexTurnState.ClearCodexTurnState(c.Request.Context(), account)
	c.Header("Cache-Control", "no-store")
	response.Success(c, h.codexTurnState.CodexTurnStateStatus(c.Request.Context(), account))
}

func (h *AccountHandler) codexTurnStateAccount(c *gin.Context) (*service.Account, error) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_ACCOUNT_ID", "Invalid account ID")
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	}
	if !account.IsOpenAIOAuthLike() || account.IsOpenAIAgentIdentity() {
		return nil, infraerrors.BadRequest("CODEX_TURN_STATE_UNSUPPORTED_ACCOUNT", "Turn-state observations require an OpenAI OAuth or setup-token account without agent identity authentication")
	}
	return account, nil
}
