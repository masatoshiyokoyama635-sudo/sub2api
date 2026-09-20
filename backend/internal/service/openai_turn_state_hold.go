package service

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"sort"
	"strings"
	"time"
)

const OpenAITurnStateHoldSelectionReason = "turn_state_hold"
const OpenAITurnStateHoldReason = GatewayFailureReason("openai_turn_state_hold")

type codexHunterHoldKey struct{}

func openAITurnStateHoldResetAt(a *Account, model string) (time.Time, bool) {
	if a == nil {
		return time.Time{}, false
	}
	limits, _ := a.Extra["model_rate_limits"].(map[string]any)
	item, _ := limits[model].(map[string]any)
	if item["reason"] != OpenAITurnStateHoldSelectionReason {
		return time.Time{}, false
	}
	raw, _ := item["rate_limit_reset_at"].(string)
	value, err := time.Parse(time.RFC3339, raw)
	return value, err == nil
}

func openAITurnStateHeldModels(a *Account, now time.Time) []string {
	if a == nil {
		return nil
	}
	limits, _ := a.Extra["model_rate_limits"].(map[string]any)
	var models []string
	for model := range limits {
		if until, ok := openAITurnStateHoldResetAt(a, model); ok && now.Before(until) {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models
}

func (s *OpenAIGatewayService) codexHunterHoldRequest(req *http.Request, a *Account, model string) *http.Request {
	if s == nil || req == nil || !s.codexHunterReusable(a) || !s.openAITurnStateHuntedModel(a, model) {
		return req
	}
	cfg, _ := readOpenAITurnStateHunterConfig(a)
	if !cfg.HoldWhenDegraded {
		return req
	}
	if repo, ok := s.accountRepo.(codexHunterHoldWriter); ok {
		if until, held := openAITurnStateHoldResetAt(a, model); !held || !time.Now().Before(until) {
			ttl := time.Hour
			if cfg.IdleMinutes > 0 {
				ttl = time.Duration(cfg.IdleMinutes) * time.Minute
			}
			limits, _ := a.Extra["model_rate_limits"].(map[string]any)
			current, _ := limits[model].(map[string]any)
			canReplace := len(current) == 0
			if !canReplace {
				raw, _ := current["rate_limit_reset_at"].(string)
				deadline, err := time.Parse(time.RFC3339, raw)
				canReplace = err == nil && !time.Now().Before(deadline)
			}
			if canReplace {
				if err := repo.SetCodexHunterModelHold(req.Context(), a.ID, model, time.Now().Add(ttl), maps.Clone(current)); err != nil {
					slog.Warn("openai_turn_state_hold_write_failed", "account_id", a.ID, "model", model, "error", err)
				}
			}
		}
	}
	return req.WithContext(context.WithValue(req.Context(), codexHunterHoldKey{}, true))
}

func codexHunterHoldError(req *http.Request) error {
	if req == nil || req.Context().Value(codexHunterHoldKey{}) != true {
		return nil
	}
	return &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, Reason: OpenAITurnStateHoldReason, ClientStatusCode: http.StatusServiceUnavailable, ClientMessage: "No usable turn-state candidate for this account and model; background hunter is collecting. Please retry later."}
}

// The repository performs a conditional update. A concurrent real rate limit
// cannot be erased when a hunter releases its own model-level hold.
type codexHunterHoldRepository interface {
	ReleaseCodexHunterModelHold(context.Context, int64, string, time.Time) error
}

type codexHunterHoldWriter interface {
	SetCodexHunterModelHold(context.Context, int64, string, time.Time, map[string]any) error
}

func (s *OpenAITurnStateHunterService) syncHold(ctx context.Context, a *Account, now time.Time) {
	if s == nil || s.gateway == nil {
		return
	}
	repo, ok := s.accountRepo.(codexHunterHoldRepository)
	if !ok || a == nil {
		return
	}
	for _, model := range openAITurnStateHeldModels(a, now) {
		cfg, _ := readOpenAITurnStateHunterConfig(a)
		release := !cfg.Enabled || !cfg.HoldWhenDegraded || !s.gateway.codexHunterReusable(a) || !s.gateway.openAITurnStateHuntedModel(a, model)
		if !release {
			_, release = s.gateway.codexHunterNewestUsableExpiry(ctx, a, model, now)
		}
		if release {
			until, _ := openAITurnStateHoldResetAt(a, model)
			if err := repo.ReleaseCodexHunterModelHold(ctx, a.ID, model, until); err != nil {
				slog.Warn("openai_turn_state_hold_release_failed", "account_id", a.ID, "model", model, "error", err)
			}
		}
	}
}

func codexHunterHeldForRequest(a *Account, model string, now time.Time) bool {
	if a == nil {
		return false
	}
	for _, name := range []string{strings.TrimSpace(model), a.GetMappedModel(model)} {
		if until, held := openAITurnStateHoldResetAt(a, name); held && now.Before(until) {
			return true
		}
	}
	return false
}
