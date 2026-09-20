package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func hunterSettingsTestExtra() map[string]any {
	return map[string]any{
		codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse",
		openAITurnStateHunterExtraKey: map[string]any{
			"enabled": true, "models": []any{"gpt-6-astra"}, "proxy_ids": []any{float64(20)},
			"rotating_proxy_ids": []any{float64(20)},
		},
	}
}

func TestOpenAITurnStateHunterSettingsValidateAtSharedBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"wrong_boolean", func(v map[string]any) { v["enabled"] = "true" }},
		{"missing_models", func(v map[string]any) { delete(v, "models") }},
		{"missing_proxies", func(v map[string]any) { delete(v, "proxy_ids") }},
		{"foreign_rotating_proxy", func(v map[string]any) { v["rotating_proxy_ids"] = []any{float64(21)} }},
		{"fractional_id", func(v map[string]any) { v["proxy_ids"] = []any{1.5} }},
		{"invalid_model", func(v map[string]any) { v["models"] = []any{7} }},
		{"hour_cap", func(v map[string]any) { v["max_per_hour"] = 601 }},
		{"negative_idle", func(v map[string]any) { v["idle_minutes"] = -2 }},
		{"non_finite_gap", func(v map[string]any) { v["gap_seconds"] = math.NaN() }},
		{"fractional_gap", func(v map[string]any) { v["gap_seconds"] = 2.5 }},
		{"unsupported_effort", func(v map[string]any) { v["reasoning_effort"] = "unlimited" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := hunterSettingsTestExtra()
			tc.mutate(extra[openAITurnStateHunterExtraKey].(map[string]any))
			require.Error(t, validateCodexIdentityVersionExtra(extra))
		})
	}
	extra := hunterSettingsTestExtra()
	cfg := extra[openAITurnStateHunterExtraKey].(map[string]any)
	cfg["models"], cfg["auto_models"] = []string{}, true
	cfg["proxy_ids"], cfg["rotating_proxy_ids"] = []int64{20}, []int{20}
	cfg["idle_minutes"], cfg["max_per_hour"] = -1, json.Number("600")
	require.NoError(t, validateCodexIdentityVersionExtra(extra))
}

func TestOpenAITurnStateHunterSettingsEligibilityAndDormantRollback(t *testing.T) {
	parentID := int64(1)
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}},
	} {
		require.Error(t, validateCodexIdentityVersionTarget(account, hunterSettingsTestExtra()))
	}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: hunterSettingsTestExtra()}
	require.NoError(t, validateCodexIdentityVersionTarget(account, account.Extra))
	extra := hunterSettingsTestExtra()
	extra[codexIdentityVersionExtraKey], extra[codexTurnStateModeExtraKey] = "v1", "off"
	require.Error(t, validateCodexIdentityVersionTarget(account, extra))
	extra[openAITurnStateHunterExtraKey].(map[string]any)["enabled"] = false
	require.NoError(t, validateCodexIdentityVersionTarget(account, extra))
	// Older clients can roll back v2 while leaving the background settings dormant.
	rollback := map[string]any{codexIdentityVersionExtraKey: "v1"}
	require.NoError(t, validateCodexIdentityVersionTarget(account, rollback))
	preserved := preserveCodexIdentityVersionForUpdate(account, rollback)
	require.Equal(t, account.Extra[openAITurnStateHunterExtraKey], preserved[openAITurnStateHunterExtraKey])
	account.Extra = preserved
	require.False(t, account.IsOpenAITurnStateHunterEnabled())
}

func TestOpenAITurnStateHunterSettingsRecoveryIndependentAndBounded(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "observe"}}
	extra := map[string]any{openAITurnStateRecoveryExtraKey: map[string]any{"enabled": true}}
	require.NoError(t, validateCodexIdentityVersionTarget(account, extra))
	for key, value := range map[string]any{"streak_target": 51, "min_minutes": 1441, "cooldown_hours": 169, "usage_api_key_id": -1, "reasoning_effort": "nope"} {
		require.Error(t, ValidateOpenAITurnStateHunterExtra(map[string]any{
			openAITurnStateRecoveryExtraKey: map[string]any{"enabled": true, key: value},
		}))
	}
	require.Error(t, ValidateOpenAITurnStateHunterExtra(map[string]any{
		openAITurnStateRecoveryExtraKey: map[string]any{"min_minutes": 90, "max_minutes": 30},
	}))
}

func TestOpenAITurnStateHunterSettingsRuntimeIsReadOnlyForAllAdminWrites(t *testing.T) {
	ctx := context.Background()
	for _, runtime := range OpenAITurnStateManagedExtraKeys() {
		for _, path := range []string{"create", "update", "extra", "bulk"} {
			t.Run(runtime+"/"+path, func(t *testing.T) {
				repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
					ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					Extra: map[string]any{codexIdentityVersionExtraKey: "v2"},
				}}}
				svc := &adminServiceImpl{accountRepo: repo}
				extra := map[string]any{runtime: map[string]any{"attempts": 999}}
				var err error
				switch path {
				case "create":
					_, err = buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, extra)
				case "update":
					_, err = svc.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: extra})
				case "extra":
					err = svc.UpdateAccountExtra(ctx, 1, extra)
				case "bulk":
					_, err = svc.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: extra})
				}
				require.ErrorContains(t, err, "managed by the background service")
				require.NotContains(t, repo.accounts[1].Extra, runtime)
			})
		}
	}
}

func TestOpenAITurnStateHunterSettingsExportRemovesRuntimeWithoutMutation(t *testing.T) {
	extra := hunterSettingsTestExtra()
	extra[openAITurnStateHuntExtraKey] = map[string]any{"attempts": 1}
	extra[openAITurnStateRecoveryStateExtraKey] = map[string]any{"streak": 2}
	portable := StripOpenAITurnStateManagedExtra(extra)
	require.Contains(t, extra, openAITurnStateHuntExtraKey)
	require.NotContains(t, portable, openAITurnStateHuntExtraKey)
	require.NotContains(t, portable, openAITurnStateRecoveryStateExtraKey)
	require.Contains(t, portable, openAITurnStateHunterExtraKey)
	require.NoError(t, ValidateOpenAITurnStateHunterExtra(portable))
}
