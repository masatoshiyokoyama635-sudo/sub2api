package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// recordCodexHunterUsage bills only usage actually reported by the completed
// probe. An absent usage event remains zero; token estimates never create a bill.
func (s *OpenAITurnStateHunterService) recordCodexHunterUsage(ctx context.Context, account *Account, cfg openAITurnStateHunterConfig, result *OpenAIForwardResult) error {
	if s == nil || cfg.UsageAPIKeyID <= 0 || account == nil || result == nil {
		return nil
	}
	if s.apiKeys == nil || s.recordUsage == nil {
		return fmt.Errorf("turn-state probe billing is unavailable")
	}
	key, err := s.apiKeys.GetByID(ctx, cfg.UsageAPIKeyID)
	if err != nil {
		return fmt.Errorf("load probe billing API key: %w", err)
	}
	if key == nil || key.User == nil || !key.IsActive() || key.IsExpired() || !key.User.IsActive() {
		return fmt.Errorf("probe billing API key or user is unavailable")
	}
	// Never silently charge a subscription key's wallet when its subscription
	// cannot be resolved. The normal RecordUsage path uses the supplied mode.
	var subscription *UserSubscription
	if key.Group != nil && key.Group.IsSubscriptionType() {
		if s.subscriptions == nil {
			return fmt.Errorf("probe subscription billing is unavailable")
		}
		subscription, err = s.subscriptions.GetActiveSubscription(ctx, key.User.ID, key.Group.ID)
		if err != nil {
			return fmt.Errorf("load probe billing subscription: %w", err)
		}
		if subscription == nil {
			return fmt.Errorf("probe billing requires an active subscription")
		}
	}
	copyResult := *result
	id := strings.TrimSpace(copyResult.RequestID)
	if id == "" {
		id = uuid.NewString()
	}
	if !strings.HasPrefix(id, "turn_state_probe:") {
		id = "turn_state_probe:" + id
	}
	copyResult.RequestID = id
	endpoint := strings.TrimSpace(copyResult.UpstreamEndpoint)
	if endpoint == "" {
		endpoint = "/backend-api/codex/responses"
	}
	effort := cfg.ReasoningEffort
	if copyResult.ReasoningEffort == nil && effort != "" {
		copyResult.ReasoningEffort = &effort
	}
	return s.recordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &copyResult, APIKey: key, User: key.User, Account: account,
		Subscription: subscription, APIKeyService: s.apiKeys,
		InboundEndpoint: "turn-state-probe", UpstreamEndpoint: endpoint,
		RequestType: RequestTypeTurnStateProbe, PricingAt: time.Now().Add(-result.Duration),
	})
}
