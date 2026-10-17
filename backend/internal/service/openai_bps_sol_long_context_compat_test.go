package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The BPS attempt ledger must retain the same upstream-model exemption as a
// single request, while charging one downstream usage row and one deduction.
func TestExcelBPSGPT61SolAttemptBillingCompatibility(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, model := range []struct {
				name, request, upstream, response string
				exempt                            bool
			}{
				{"direct_sol", "gpt-6.1-sol", "", "", true},
				{"alias_sent_to_sol", "customer-alias", "gpt-6.1-sol", "gpt-6-sol", true},
				{"sol_sent_to_old_model", "gpt-6.1-sol", "gpt-6-sol", "gpt-6.1-sol", false},
				{"billing_and_response_labels_are_not_authoritative", "gpt-6-sol", "", "gpt-6.1-sol", false},
			} {
				for _, attempts := range [][]int{{140000, 140000}, {272000, 272001}} {
					name := fmt.Sprintf("resolver=%t/%s/%s/%v", resolved, accountType, model.name, attempts)
					t.Run(name, func(t *testing.T) {
						svc, logs, users := newBPSSolCompatibilityService(t)
						key := &APIKey{ID: 1}
						if resolved {
							key = openAIRecordUsageAPIKeyWithGroup(svc, 1, true)
						}
						result := bpsSolCompatibilityResult(attempts)
						result.Model, result.UpstreamModel, result.UpstreamResponseModel = model.request, model.upstream, model.response
						// Deliberately keep the billing label constant; only the model sent
						// upstream is allowed to decide the official-price exemption.
						result.BillingModel = "gpt-6.1-sol"
						wantInput, wantOutput, wantWrite, wantRead := 0.0, 0.0, 0.0, 0.0
						wantLong := false
						for _, n := range attempts {
							inputFactor, outputFactor := 1.0, 1.0
							if !model.exempt && n > 272000 {
								inputFactor, outputFactor, wantLong = 2, 1.5, true
							}
							wantInput += float64(n-100000) * 2e-6 * inputFactor
							wantOutput += 1000 * 10e-6 * outputFactor
							wantWrite += 20000 * 2.5e-6 * inputFactor
							wantRead += 80000 * 0.1e-6 * inputFactor
						}
						assertBPSSolCompatibilityCharge(t, svc, logs, users, key, accountType, result, wantInput, wantOutput, wantWrite, wantRead)
						require.Equal(t, wantLong, logs.lastLog.LongContextBillingApplied)
					})
				}
			}
		}
	}
}

func TestExcelBPSGPT61SolAttemptBillingPreservesCustomIntervalBoundaries(t *testing.T) {
	for _, attempts := range [][]int{{199999, 200000, 200001}, {299999, 300000, 300001}} {
		t.Run(fmt.Sprint(attempts), func(t *testing.T) {
			svc, logs, users := newBPSSolCompatibilityService(t)
			key := openAIRecordUsageAPIKeyWithGroup(svc, 1, true)
			cache := newEmptyChannelCache()
			p := func(v float64) *float64 { return &v }
			upper := 300000
			cache.pricingByGroupModel[channelModelKey{groupID: 1, model: "gpt-6.1-sol"}] = &ChannelModelPricing{
				BillingMode: BillingModeToken,
				Intervals: []PricingInterval{
					{MinTokens: 200000, MaxTokens: &upper, InputMultiplier: p(3), OutputMultiplier: p(4), CacheWriteMultiplier: p(2), CacheReadMultiplier: p(5)},
					{MinTokens: 300000, InputMultiplier: p(4), OutputMultiplier: p(5), CacheWriteMultiplier: p(3), CacheReadMultiplier: p(6)},
				},
			}
			cache.channelByGroupID[1] = &Channel{ID: 1, Status: StatusActive}
			cache.groupPlatform[1] = ""
			cache.loadedAt = time.Now()
			channel := &ChannelService{}
			channel.cache.Store(cache)
			svc.resolver = NewModelPricingResolver(channel, svc.billingService)
			result := bpsSolCompatibilityResult(attempts)
			result.Model, result.UpstreamModel = "customer-alias", "gpt-6.1-sol"
			wantInput, wantOutput, wantWrite, wantRead := 0.0, 0.0, 0.0, 0.0
			for _, n := range attempts {
				inputFactor, outputFactor, writeFactor, readFactor := 1.0, 1.0, 1.0, 1.0
				if n > 300000 {
					inputFactor, outputFactor, writeFactor, readFactor = 4, 5, 3, 6
				} else if n > 200000 {
					inputFactor, outputFactor, writeFactor, readFactor = 3, 4, 2, 5
				}
				wantInput += float64(n-100000) * 2e-6 * inputFactor
				wantOutput += 1000 * 10e-6 * outputFactor
				wantWrite += 20000 * 2.5e-6 * writeFactor
				wantRead += 80000 * 0.1e-6 * readFactor
			}
			assertBPSSolCompatibilityCharge(t, svc, logs, users, key, AccountTypeOAuth, result, wantInput, wantOutput, wantWrite, wantRead)
		})
	}
}

func newBPSSolCompatibilityService(t *testing.T) (*OpenAIGatewayService, *openAIRecordUsageLogRepoStub, *openAIRecordUsageUserRepoStub) {
	t.Helper()
	logs, users := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	svc.billingService = NewBillingService(svc.cfg, newStubPricingServiceFromJSON(t, `{
		"gpt-6.1-sol": {"litellm_provider":"openai", "mode":"chat",
		"input_cost_per_token":2e-6, "output_cost_per_token":10e-6,
		"cache_read_input_token_cost":0.1e-6, "cache_creation_input_token_cost":2.5e-6,
		"input_cost_per_token_above_272k_tokens":4e-6,
		"output_cost_per_token_above_272k_tokens":15e-6}
	}`))
	return svc, logs, users
}

func bpsSolCompatibilityResult(attempts []int) *OpenAIForwardResult {
	result := &OpenAIForwardResult{RequestID: "bps-sol-compat", UpstreamEndpoint: "/basispoints/api/responses", Duration: time.Second}
	for _, n := range attempts {
		usage := OpenAIUsage{InputTokens: n, OutputTokens: 1000, CacheReadInputTokens: 80000, CacheCreationInputTokens: 20000}
		result.BasispointsUsageAttempts = append(result.BasispointsUsageAttempts, usage)
		result.Usage.InputTokens += n
		result.Usage.OutputTokens += usage.OutputTokens
		result.Usage.CacheReadInputTokens += usage.CacheReadInputTokens
		result.Usage.CacheCreationInputTokens += usage.CacheCreationInputTokens
	}
	return result
}

func assertBPSSolCompatibilityCharge(t *testing.T, svc *OpenAIGatewayService, logs *openAIRecordUsageLogRepoStub, users *openAIRecordUsageUserRepoStub, key *APIKey, accountType string, result *OpenAIForwardResult, input, output, write, read float64) {
	t.Helper()
	raw := result.Usage
	ledger := append([]OpenAIUsage(nil), result.BasispointsUsageAttempts...)
	err := svc.RecordUsage(t.Context(), &OpenAIRecordUsageInput{
		Result: result, APIKey: key, User: &User{ID: 2},
		Account: &Account{ID: 3, Platform: PlatformOpenAI, Type: accountType, Extra: map[string]any{openAILongContextBillingEnabledKey: true}},
	})
	require.NoError(t, err)
	require.NotNil(t, logs.lastLog)
	require.InDelta(t, input, logs.lastLog.InputCost, 1e-10)
	require.InDelta(t, output, logs.lastLog.OutputCost, 1e-10)
	require.InDelta(t, write, logs.lastLog.CacheCreationCost, 1e-10)
	require.InDelta(t, read, logs.lastLog.CacheReadCost, 1e-10)
	require.InDelta(t, input+output+write+read, logs.lastLog.TotalCost, 1e-10)
	require.InDelta(t, (input+output+write+read)*1.1, logs.lastLog.ActualCost, 1e-10)
	require.InDelta(t, logs.lastLog.ActualCost, users.lastAmount, 1e-10)
	require.Equal(t, 1, logs.calls)
	require.Equal(t, 1, users.deductCalls)
	require.Equal(t, raw, result.Usage)
	require.Equal(t, ledger, result.BasispointsUsageAttempts)
}
