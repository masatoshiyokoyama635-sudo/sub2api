//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexHunterHoldTestRepo struct {
	AccountRepository
	held         []string
	expected     []map[string]any
	released     []string
	releaseUntil []time.Time
}

func (r *codexHunterHoldTestRepo) SetCodexHunterModelHold(_ context.Context, _ int64, model string, _ time.Time, expected map[string]any) error {
	r.held = append(r.held, model)
	r.expected = append(r.expected, expected)
	return nil
}
func (r *codexHunterHoldTestRepo) ReleaseCodexHunterModelHold(_ context.Context, _ int64, model string, until time.Time) error {
	r.released = append(r.released, model)
	r.releaseUntil = append(r.releaseUntil, until)
	return nil
}
func codexHunterHoldTestAccount() *Account {
	extra := hunterSettingsTestExtra()
	extra[openAITurnStateHunterExtraKey].(map[string]any)["hold_when_degraded"] = true
	return &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra}
}

func TestCodexHunterHoldRequestOnlyPausesMissingCandidateModel(t *testing.T) {
	repo := &codexHunterHoldTestRepo{}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	account := codexHunterHoldTestAccount()
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	held := gateway.codexHunterHoldRequest(req, account, "gpt-6-astra")
	require.Error(t, codexHunterHoldError(held))
	require.Equal(t, []string{"gpt-6-astra"}, repo.held)
	require.Nil(t, repo.expected[0])
	untouched := gateway.codexHunterHoldRequest(req, account, "gpt-5.6-luna")
	require.Same(t, req, untouched)
	require.NoError(t, codexHunterHoldError(untouched))
	require.Len(t, repo.held, 1)
}

func TestCodexHunterHoldRequestPreservesProviderLimits(t *testing.T) {
	for _, until := range []string{time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "malformed"} {
		repo := &codexHunterHoldTestRepo{}
		gateway := &OpenAIGatewayService{accountRepo: repo}
		account := codexHunterHoldTestAccount()
		account.Extra["model_rate_limits"] = map[string]any{"gpt-6-astra": map[string]any{"reason": "provider_429", "rate_limit_reset_at": until}}
		req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
		require.NoError(t, err)
		held := gateway.codexHunterHoldRequest(req, account, "gpt-6-astra")
		require.Error(t, codexHunterHoldError(held))
		require.Empty(t, repo.held)
		require.Empty(t, openAITurnStateHeldModels(account, time.Now()))
	}
}

func TestCodexHunterHoldReleaseOnlyOwnActiveModels(t *testing.T) {
	now := time.Now().UTC()
	until := now.Add(time.Hour).Truncate(time.Second)
	account := codexHunterHoldTestAccount()
	account.Extra["model_rate_limits"] = map[string]any{
		"gpt-6-astra":    map[string]any{"reason": OpenAITurnStateHoldSelectionReason, "rate_limit_reset_at": until.Format(time.RFC3339)},
		"provider-model": map[string]any{"reason": "provider_429", "rate_limit_reset_at": until.Format(time.RFC3339)},
		"expired-model":  map[string]any{"reason": OpenAITurnStateHoldSelectionReason, "rate_limit_reset_at": now.Add(-time.Minute).Format(time.RFC3339)},
	}
	account.Extra[openAITurnStateHunterExtraKey].(map[string]any)["enabled"] = false
	repo := &codexHunterHoldTestRepo{}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	worker := &OpenAITurnStateHunterService{gateway: gateway, accountRepo: repo}
	worker.syncHold(context.Background(), account, now)
	require.Equal(t, []string{"gpt-6-astra"}, repo.released)
	require.Equal(t, []time.Time{until}, repo.releaseUntil)
	require.True(t, codexHunterHeldForRequest(account, "gpt-6-astra", now))
	require.False(t, codexHunterHeldForRequest(account, "provider-model", now))
}
