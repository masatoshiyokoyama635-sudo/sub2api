package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexHunterUsageKeyStub struct {
	key *APIKey
	err error
}

func (s *codexHunterUsageKeyStub) GetByID(context.Context, int64) (*APIKey, error) {
	return s.key, s.err
}
func (s *codexHunterUsageKeyStub) UpdateQuotaUsed(context.Context, int64, float64) error { return nil }
func (s *codexHunterUsageKeyStub) UpdateRateLimitUsage(context.Context, int64, float64) error {
	return nil
}

type codexHunterSubscriptionStub struct {
	subscription *UserSubscription
	err          error
}

func (s *codexHunterSubscriptionStub) GetActiveSubscription(context.Context, int64, int64) (*UserSubscription, error) {
	return s.subscription, s.err
}

func codexHunterUsageKey() *APIKey {
	return &APIKey{ID: 20, Status: StatusActive, User: &User{ID: 30, Status: StatusActive}}
}

func TestOpenAITurnStateProbeUsageUsesOnlyObservedTokensAndTypedRecord(t *testing.T) {
	for _, usage := range []OpenAIUsage{{}, {InputTokens: 40, OutputTokens: 3, CacheReadInputTokens: 7}} {
		var recorded *OpenAIRecordUsageInput
		hunter := &OpenAITurnStateHunterService{
			apiKeys:     &codexHunterUsageKeyStub{key: codexHunterUsageKey()},
			recordUsage: func(_ context.Context, in *OpenAIRecordUsageInput) error { recorded = in; return nil },
		}
		result := &OpenAIForwardResult{RequestID: "upstream-id", Model: "gpt-6-astra", Usage: usage, Stream: true, Duration: time.Second}
		err := hunter.recordCodexHunterUsage(context.Background(), &Account{ID: 4}, openAITurnStateHunterConfig{UsageAPIKeyID: 20, ReasoningEffort: "high"}, result)
		require.NoError(t, err)
		require.NotNil(t, recorded)
		require.Equal(t, usage, recorded.Result.Usage)
		require.Equal(t, RequestTypeTurnStateProbe, recorded.RequestType)
		require.Equal(t, "turn-state-probe", recorded.InboundEndpoint)
		require.Equal(t, "turn_state_probe:upstream-id", recorded.Result.RequestID)
		require.Equal(t, "upstream-id", result.RequestID, "billing must not mutate probe result")
		require.True(t, isForcedUsageBillingRequestID(recorded.Result.RequestID))
	}
}

func TestOpenAITurnStateProbeUsageDoesNotChargeUnavailableKeyOrSubscription(t *testing.T) {
	for _, scenario := range []string{"disabled", "missing_key", "inactive_key", "inactive_user", "expired_key", "missing_subscription", "subscription_error"} {
		t.Run(scenario, func(t *testing.T) {
			key := codexHunterUsageKey()
			cfg := openAITurnStateHunterConfig{UsageAPIKeyID: key.ID}
			calls := 0
			hunter := &OpenAITurnStateHunterService{recordUsage: func(context.Context, *OpenAIRecordUsageInput) error { calls++; return nil }}
			switch scenario {
			case "disabled":
				cfg.UsageAPIKeyID = 0
			case "missing_key":
				key = nil
			case "inactive_key":
				key.Status = StatusDisabled
			case "inactive_user":
				key.User.Status = StatusDisabled
			case "expired_key":
				expired := time.Now().Add(-time.Minute)
				key.ExpiresAt = &expired
			case "missing_subscription", "subscription_error":
				key.Group = &Group{ID: 5, SubscriptionType: "subscription"}
				if scenario == "subscription_error" {
					hunter.subscriptions = &codexHunterSubscriptionStub{err: errors.New("unavailable")}
				}
			}
			hunter.apiKeys = &codexHunterUsageKeyStub{key: key}
			err := hunter.recordCodexHunterUsage(context.Background(), &Account{ID: 4}, cfg, &OpenAIForwardResult{Model: "gpt-6-astra"})
			if scenario == "disabled" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Zero(t, calls)
		})
	}
}

func TestOpenAITurnStateProbeRecordUsagePreservesRealTrafficCooldownAndIdle(t *testing.T) {
	for _, applied := range []bool{true, false} {
		counter := &openAI403CounterResetStub{}
		rateLimit := NewRateLimitService(nil, nil, nil, nil, nil)
		rateLimit.SetOpenAI403CounterCache(counter)
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: applied}}
		svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		svc.rateLimitService = rateLimit
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{RequestID: "turn_state_probe:test", Model: "gpt-5.1", Stream: true},
			APIKey: &APIKey{ID: 20, Group: &Group{RateMultiplier: 1}}, User: &User{ID: 30},
			Account: &Account{ID: 7, Platform: PlatformOpenAI}, RequestType: RequestTypeTurnStateProbe,
		})
		require.NoError(t, err)
		require.Empty(t, counter.resetCalls)
		_, changed := svc.deferredService.lastUsedUpdates.Load(int64(7))
		require.False(t, changed)
		require.Equal(t, RequestTypeTurnStateProbe, usageRepo.lastLog.EffectiveRequestType())
		require.Zero(t, usageRepo.lastLog.TotalCost)
		require.Zero(t, usageRepo.lastLog.InputTokens)
	}
}

func TestOpenAITurnStateProbeRecordUsageBillsReportedTokens(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	usage := OpenAIUsage{InputTokens: 120, OutputTokens: 4, CacheReadInputTokens: 20}
	require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "turn_state_probe:reported", Model: "gpt-5.1", Usage: usage, Stream: true},
		APIKey: &APIKey{ID: 20}, User: &User{ID: 30}, Account: &Account{ID: 7}, RequestType: RequestTypeTurnStateProbe,
	}))
	require.Equal(t, 100, usageRepo.lastLog.InputTokens)
	require.Equal(t, 20, usageRepo.lastLog.CacheReadTokens)
	require.Equal(t, 4, usageRepo.lastLog.OutputTokens)
	require.Equal(t, 1, userRepo.deductCalls)
	expected := expectedOpenAICost(t, svc, "gpt-5.1", usage, 1.1)
	require.InDelta(t, expected.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
}

func TestOpenAITurnStateProbeUsageUsesConfiguredSubscription(t *testing.T) {
	key := codexHunterUsageKey()
	key.Group = &Group{ID: 5, SubscriptionType: SubscriptionTypeSubscription}
	subscription := &UserSubscription{ID: 44}
	var recorded *OpenAIRecordUsageInput
	hunter := &OpenAITurnStateHunterService{
		apiKeys:       &codexHunterUsageKeyStub{key: key},
		subscriptions: &codexHunterSubscriptionStub{subscription: subscription},
		recordUsage:   func(_ context.Context, input *OpenAIRecordUsageInput) error { recorded = input; return nil },
	}
	require.NoError(t, hunter.recordCodexHunterUsage(context.Background(), &Account{ID: 7}, openAITurnStateHunterConfig{UsageAPIKeyID: key.ID}, &OpenAIForwardResult{Model: "gpt-6-astra"}))
	require.Same(t, subscription, recorded.Subscription)
}

func TestOpenAITurnStateProbeRequestTypeRoundTrip(t *testing.T) {
	kind, err := ParseUsageRequestType("probe")
	require.NoError(t, err)
	require.Equal(t, int16(6), int16(kind))
	require.Equal(t, "probe", RequestTypeFromInt16(6).String())
	stream, ws := ApplyLegacyRequestFields(kind, true, false)
	require.True(t, stream)
	require.False(t, ws)
}
