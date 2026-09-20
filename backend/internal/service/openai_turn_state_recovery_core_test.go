//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (h *hunterCoreHarness) enableRecovery(id int64, streak int) {
	h.config(id)["enabled"] = false
	h.repo.accounts[id].Extra[openAITurnStateRecoveryExtraKey] = map[string]any{"enabled": true, "model": "gpt-6-astra", "streak_target": streak, "min_minutes": 1, "max_minutes": 1, "cooldown_hours": 2}
}
func (h *hunterCoreHarness) recoveryState(id int64) openAITurnStateRecoveryState {
	a, _ := h.repo.GetByID(context.Background(), id)
	return readOpenAITurnStateRecoveryState(a)
}

func TestCodexHunterRecoveryIndependentSwitchUsesOriginalExit(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.enableRecovery(1, 2)
	calls := 0
	h.svc.probeFunc = func(_ context.Context, a *Account, model string, _ openAITurnStateHunterConfig, p *Proxy) openAITurnStateHuntAttempt {
		require.Nil(t, p, "recovery must use the account's own exit")
		require.Equal(t, int64(1), *a.ProxyID)
		require.Equal(t, "normal.invalid", a.Proxy.Host)
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Healthy: true, Chars: 332, ResponseModel: model}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	require.Equal(t, 1, h.recoveryState(1).Streak)
	require.True(t, h.recoveryState(1).RecoveredAt.IsZero())
	h.now = h.now.Add(time.Minute)
	h.svc.runOnce(context.Background())
	require.Equal(t, 2, calls)
	require.False(t, h.recoveryState(1).RecoveredAt.IsZero())
	h.now = h.now.Add(time.Hour)
	h.svc.runOnce(context.Background())
	require.Equal(t, 2, calls)
	for _, update := range h.repo.writes {
		require.Len(t, update, 1)
		require.Contains(t, update, openAITurnStateRecoveryStateExtraKey)
	}
}

func TestCodexHunterRecoveryFailureCooldownAndReset(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.enableRecovery(1, 2)
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Chars: 312}
	}
	h.svc.runOnce(context.Background())
	h.now = h.now.Add(time.Minute)
	h.svc.runOnce(context.Background())
	st := h.recoveryState(1)
	require.Equal(t, h.now.Add(2*time.Hour), st.CoolingUntil)
	require.Equal(t, st.CoolingUntil, st.NextAt)
	require.Zero(t, st.FailStreak)
	h.now = h.now.Add(time.Hour)
	h.svc.runOnce(context.Background())
	require.Equal(t, 2, calls)
}

func TestCodexHunterRecoveryUnauthorizedStopsBothProbeModes(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.enableRecovery(1, 2)
	h.config(1)["enabled"] = true
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 401, Error: "unauthorized"}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls, "hunter must observe the recovery credential block in this same tick")
	h.now = h.now.Add(24 * time.Hour)
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
}

func TestCodexHunterRecoveryRateLimitSharedWithHunter(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.enableRecovery(1, 2)
	h.config(1)["enabled"] = true
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 429, Error: "limited", RetryAfter: 3 * time.Hour}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	require.Equal(t, h.now.Add(3*time.Hour), h.recoveryState(1).RateLimitUntil)
	h.now = h.now.Add(time.Hour)
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
}

func TestCodexHunterRecoveryZeroTimestampIsNotRecovered(t *testing.T) {
	raw, err := json.Marshal(openAITurnStateRecoveryState{})
	require.NoError(t, err)
	require.NotContains(t, string(raw), "recovered_at")
	require.NotContains(t, string(raw), "cooling_until")
}

func TestCodexHunterRecoveryResetPreservesConfiguration(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.enableRecovery(1, 1)
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		return openAITurnStateHuntAttempt{Status: 200, Healthy: true, Chars: 332}
	}
	h.svc.runOnce(context.Background())
	require.False(t, h.recoveryState(1).RecoveredAt.IsZero())
	account, _ := h.repo.GetByID(context.Background(), 1)
	before, _ := json.Marshal(account.Extra[openAITurnStateRecoveryExtraKey])
	h.svc.gateway.resetOpenAITurnStateRecovery(context.Background(), account)
	require.True(t, h.recoveryState(1).RecoveredAt.IsZero())
	require.Zero(t, h.recoveryState(1).Streak)
	after, _ := json.Marshal(h.repo.accounts[1].Extra[openAITurnStateRecoveryExtraKey])
	require.JSONEq(t, string(before), string(after))
}

func TestCodexHunterRecoveryAutoModelUsesMostRecentTraffic(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	a := h.repo.accounts[1]
	h.svc.gateway.noteOpenAITurnStateTraffic(a.ID, "gpt-6-astra", h.now.Add(-time.Minute))
	h.svc.gateway.noteOpenAITurnStateMinted(a.ID, "gpt-6-astra")
	h.svc.gateway.noteOpenAITurnStateTraffic(a.ID, "gpt-6-luna", h.now)
	h.svc.gateway.noteOpenAITurnStateMinted(a.ID, "gpt-6-luna")
	require.Equal(t, "gpt-6-luna", h.svc.recoveryModel(a, openAITurnStateRecoveryConfig{}, h.now))
}

func TestCodexHunterRecoveryInterleavesAccountsAtShortIntervals(t *testing.T) {
	h := newHunterCoreHarness(t, 2)
	h.enableRecovery(1, 5)
	h.enableRecovery(2, 5)
	var order []int64
	h.svc.probeFunc = func(_ context.Context, a *Account, _ string, _ openAITurnStateHunterConfig, p *Proxy) openAITurnStateHuntAttempt {
		require.Nil(t, p)
		order = append(order, a.ID)
		return openAITurnStateHuntAttempt{Status: 200, Chars: 312}
	}
	h.svc.runOnce(context.Background())
	h.now = h.now.Add(time.Minute)
	h.svc.runOnce(context.Background())
	require.Equal(t, []int64{1, 2}, order, "a short interval on the first account must not starve the second")
}

func TestCodexHunterRecoveryObserveModeRunsWithHunterDormant(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.enableRecovery(1, 1)
	h.repo.accounts[1].Extra[codexTurnStateModeExtraKey] = "observe"
	calls := 0
	h.svc.probeFunc = func(_ context.Context, _ *Account, _ string, _ openAITurnStateHunterConfig, p *Proxy) openAITurnStateHuntAttempt {
		require.Nil(t, p)
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Healthy: true, Chars: 332}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	require.False(t, h.repo.accounts[1].IsOpenAITurnStateHunterEnabled())
}
