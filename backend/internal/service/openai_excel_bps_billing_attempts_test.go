package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExcelBPSRepairBillingUsesAttemptContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inputs []int
		long   bool
	}{
		{"two_short", []int{140000, 140000}, false},
		{"three_short", []int{100000, 100000, 100000}, false},
		{"one_long_one_short", []int{300000, 140000}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			swapInOpenAILadderCatalog(t, svc)
			result := &OpenAIForwardResult{RequestID: "bps-" + tc.name, Model: "gpt-5.4-2026-03-05", UpstreamEndpoint: "/basispoints/api/responses", Duration: time.Second}
			want := 0.0
			for _, n := range tc.inputs {
				usage := OpenAIUsage{InputTokens: n, OutputTokens: 1000}
				result.BasispointsUsageAttempts = append(result.BasispointsUsageAttempts, usage)
				result.Usage.InputTokens += n
				result.Usage.OutputTokens += 1000
				input, output := float64(n)*2.5e-6, 1000*15e-6
				if n > 272000 {
					input *= 2
					output *= 1.5
				}
				want += input + output
			}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: openAIRecordUsageAPIKeyWithGroup(svc, 201, true), User: &User{ID: 202}, Account: &Account{ID: 203, Platform: PlatformOpenAI, Extra: map[string]any{"openai_long_context_billing_enabled": true}}})
			require.NoError(t, err)
			require.NotNil(t, logs.lastLog)
			require.InDelta(t, want, logs.lastLog.TotalCost, 1e-10, "each completed attempt has its own context threshold")
			require.InDelta(t, want*1.1, logs.lastLog.ActualCost, 1e-10)
			require.Equal(t, tc.long, logs.lastLog.LongContextBillingApplied)
			require.Equal(t, result.Usage.InputTokens, logs.lastLog.InputTokens)
			require.Equal(t, 1, logs.calls, "one downstream request has one usage row")
			require.Equal(t, 1, users.deductCalls)
		})
	}
}

func TestExcelBPSRepairBillingKeepsRequestPriceOnce(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = newOpenAIImageChannelPricingResolverForTest(t, 1, "gpt-5.4", 0.75)
	result := &OpenAIForwardResult{RequestID: "bps-flat", Model: "gpt-5.4", UpstreamEndpoint: "/basispoints/api/responses", Usage: OpenAIUsage{InputTokens: 280000, OutputTokens: 2000}, BasispointsUsageAttempts: []OpenAIUsage{{InputTokens: 140000, OutputTokens: 1000}, {InputTokens: 140000, OutputTokens: 1000}}}
	cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, &APIKey{Group: &Group{ID: 1}}, []string{"gpt-5.4"}, 1, 1, 1, 1, UsageTokens{InputTokens: 280000, OutputTokens: 2000}, "", nil, time.Time{})
	require.NoError(t, err)
	require.InDelta(t, 0.75, cost.TotalCost, 1e-12, "internal repairs must not multiply request fees")
}

func TestExcelBPSRepairBillingCacheAndTierPolicy(t *testing.T) {
	for _, cacheAsInput := range []bool{false, true} {
		for _, tier := range []string{"", "priority"} {
			for _, gate := range []bool{false, true} {
				svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				swapInOpenAILadderCatalog(t, svc)
				apiKey := openAIRecordUsageAPIKeyWithGroup(svc, 1, gate)
				usage := OpenAIUsage{InputTokens: 140000, OutputTokens: 1000, CacheReadInputTokens: 10000, CacheCreationInputTokens: 20000}
				result := &OpenAIForwardResult{UpstreamEndpoint: "/basispoints/api/responses", BasispointsUsageAttempts: []OpenAIUsage{usage, usage}, BasispointsCacheCreationAsInput: cacheAsInput}
				cacheCreation, input := 20000, 110000
				if cacheAsInput {
					cacheCreation = 0
					input = 130000
				}
				one := UsageTokens{InputTokens: input, OutputTokens: 1000, CacheReadTokens: 10000, CacheCreationTokens: cacheCreation}
				expected, err := svc.calculateOpenAIRecordUsageTokenCost(context.Background(), apiKey, "gpt-5.4-2026-03-05", 1.3, time.Time{}, one, tier, "", &gate)
				require.NoError(t, err)
				total := UsageTokens{InputTokens: input * 2, OutputTokens: 2000, CacheReadTokens: 20000, CacheCreationTokens: cacheCreation * 2}
				cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, apiKey, []string{"gpt-5.4-2026-03-05"}, 1.3, 1, 1, 1, total, tier, &gate, time.Time{})
				require.NoError(t, err)
				require.InDelta(t, 2*expected.InputCost, cost.InputCost, 1e-12)
				require.InDelta(t, 2*expected.CacheCreationCost, cost.CacheCreationCost, 1e-12)
				require.InDelta(t, 2*expected.CacheReadCost, cost.CacheReadCost, 1e-12)
				require.InDelta(t, 2*expected.OutputCost, cost.OutputCost, 1e-12)
				require.InDelta(t, 2*expected.TotalCost, cost.TotalCost, 1e-12)
				require.InDelta(t, 2*expected.ActualCost, cost.ActualCost, 1e-12)
				require.False(t, cost.LongContextBillingApplied)
			}
		}
	}
}

func TestExcelBPSRepairBillingLeavesNonBPSUnchanged(t *testing.T) {
	svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	swapInOpenAILadderCatalog(t, svc)
	apiKey := openAIRecordUsageAPIKeyWithGroup(svc, 1, true)
	gate := true
	tokens := UsageTokens{InputTokens: 280000, OutputTokens: 2000}
	result := &OpenAIForwardResult{UpstreamEndpoint: "/responses", BasispointsUsageAttempts: []OpenAIUsage{{InputTokens: 140000, OutputTokens: 1000}, {InputTokens: 140000, OutputTokens: 1000}}}
	want, err := svc.calculateOpenAIRecordUsageTokenCost(context.Background(), apiKey, "gpt-5.4-2026-03-05", 1, time.Time{}, tokens, "", "", &gate)
	require.NoError(t, err)
	cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, apiKey, []string{"gpt-5.4-2026-03-05"}, 1, 1, 1, 1, tokens, "", &gate, time.Time{})
	require.NoError(t, err)
	require.Equal(t, want, cost)
}

func TestExcelBPSRepairBillingPerRequestMode(t *testing.T) {
	svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	price := 0.75
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: 1, model: "gpt-5.4"}] = &ChannelModelPricing{BillingMode: BillingModePerRequest, PerRequestPrice: &price}
	cache.channelByGroupID[1] = &Channel{ID: 1, Status: StatusActive}
	cache.groupPlatform[1] = ""
	cache.loadedAt = time.Now()
	channel := &ChannelService{}
	channel.cache.Store(cache)
	svc.resolver = NewModelPricingResolver(channel, svc.billingService)
	result := &OpenAIForwardResult{UpstreamEndpoint: "/basispoints/api/responses", BasispointsUsageAttempts: []OpenAIUsage{{InputTokens: 140000}, {InputTokens: 140000}}}
	cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, &APIKey{Group: &Group{ID: 1}}, []string{"gpt-5.4"}, 1, 1, 1, 1, UsageTokens{InputTokens: 280000}, "", nil, time.Time{})
	require.NoError(t, err)
	require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
	require.InDelta(t, 0.75, cost.TotalCost, 1e-12)
}
