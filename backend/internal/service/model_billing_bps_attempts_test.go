package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelBillingBPSAttemptAndCompactionLedger(t *testing.T) {
	for _, knownSplit := range []bool{false, true} {
		t.Run(map[bool]string{false: "progressive_aggregate", true: "native_repairs_plus_compaction"}[knownSplit], func(t *testing.T) {
			raw := OpenAIUsage{InputTokens: 350000, OutputTokens: 2500}
			ledger := []OpenAIUsage{{InputTokens: 140000, OutputTokens: 1000}, {InputTokens: 140000, OutputTokens: 1000}, {InputTokens: 70000, OutputTokens: 500}}
			if !knownSplit {
				ledger = nil
			}
			type costs struct {
				total, actual, rate, accountQuota float64
				accountStats                      *float64
			}
			var off costs
			for _, enabled := range []bool{false, true} {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: false}}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing, nil, nil, nil)
				svc.StopOpenAICodexTicketHarvester()
				cfg := DefaultModelBillingConfig()
				cfg.Enabled = enabled
				svc.settingService = modelBillingSettings(t, cfg)
				accountRate, accountGroupRate := .3, 2.0
				group := &Group{ID: 2, RateMultiplier: .2}
				result := &OpenAIForwardResult{RequestID: "bps-model-multiplier", Model: "gpt-6-luna", UpstreamEndpoint: "/basispoints/api/responses", Usage: raw, BasispointsUsageAttempts: append([]OpenAIUsage(nil), ledger...), Duration: time.Second}
				input := &OpenAIRecordUsageInput{Result: result, APIKey: &APIKey{ID: 1, GroupID: &group.ID, Group: group}, User: &User{ID: 3}, Account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, RateMultiplier: &accountRate, GroupRateMultiplier: &accountGroupRate}}
				require.NoError(t, svc.RecordUsage(t.Context(), input))
				require.NotNil(t, logs.lastLog)
				require.NotNil(t, billing.lastCmd)
				require.Equal(t, raw, result.Usage, "raw usage must not be multiplied")
				require.Equal(t, ledger, result.BasispointsUsageAttempts, "independent native/compact ledger or unknown-split fallback must not be rewritten")
				require.Equal(t, raw.InputTokens, logs.lastLog.InputTokens)
				require.Equal(t, raw.OutputTokens, logs.lastLog.OutputTokens)
				require.Equal(t, 1, logs.calls, "one downstream request keeps one usage record")
				got := costs{logs.lastLog.TotalCost, logs.lastLog.ActualCost, logs.lastLog.RateMultiplier, billing.lastCmd.AccountQuotaCost, logs.lastLog.AccountStatsCost}
				if !enabled {
					off = got
					continue
				}
				require.InDelta(t, off.actual*10, got.actual, 1e-10, "apply configured customer multiplier once after attempt pricing")
				require.InDelta(t, off.total, got.total, 1e-10)
				require.InDelta(t, off.rate*10, got.rate, 1e-12)
				require.InDelta(t, off.accountQuota, got.accountQuota, 1e-12)
				require.Equal(t, off.accountStats, got.accountStats)
				require.Equal(t, accountRate, *logs.lastLog.AccountRateMultiplier)
			}
		})
	}
}
