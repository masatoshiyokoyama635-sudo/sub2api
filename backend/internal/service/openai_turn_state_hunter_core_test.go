//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type hunterCoreRepository struct {
	AccountRepository
	mu       sync.Mutex
	accounts map[int64]*Account
	writes   []map[string]any
}

func cloneHunterCoreAccount(a *Account) *Account {
	out := *a
	if a.Proxy != nil {
		proxy := *a.Proxy
		out.Proxy = &proxy
	}
	raw, _ := json.Marshal(a.Extra)
	out.Extra = nil
	_ = json.Unmarshal(raw, &out.Extra)
	raw, _ = json.Marshal(a.Credentials)
	out.Credentials = nil
	_ = json.Unmarshal(raw, &out.Credentials)
	return &out
}

func (r *hunterCoreRepository) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.accounts[id]; a != nil {
		return cloneHunterCoreAccount(a), nil
	}
	return nil, errors.New("not found")
}

func (r *hunterCoreRepository) ListByPlatform(_ context.Context, _ string) ([]Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int64, 0, len(r.accounts))
	for id := range r.accounts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]Account, 0, len(ids))
	for _, id := range ids {
		out = append(out, *cloneHunterCoreAccount(r.accounts[id]))
	}
	return out, nil
}

func (r *hunterCoreRepository) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[id]
	if a == nil {
		return errors.New("not found")
	}
	raw, _ := json.Marshal(updates)
	saved := map[string]any{}
	_ = json.Unmarshal(raw, &saved)
	r.writes = append(r.writes, saved)
	for k, v := range saved {
		a.Extra[k] = v
	}
	return nil
}

type hunterCoreProxyRepository struct{ proxies []Proxy }

func (r *hunterCoreProxyRepository) ListByIDs(_ context.Context, ids []int64) ([]Proxy, error) {
	wanted := map[int64]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var out []Proxy
	for _, p := range r.proxies {
		if wanted[p.ID] {
			out = append(out, p)
		}
	}
	return out, nil
}

type hunterCoreHarness struct {
	svc       *OpenAITurnStateHunterService
	repo      *hunterCoreRepository
	proxyRepo *hunterCoreProxyRepository
	now       time.Time
}

func newHunterCoreHarness(t *testing.T, count int) *hunterCoreHarness {
	t.Helper()
	normalProxy := Proxy{ID: 1, Name: "normal", Protocol: "socks5", Host: "normal.invalid", Port: 1080, Status: StatusActive}
	probeProxy := Proxy{ID: 20, Name: "rotating", Protocol: "socks5", Host: "rotating.invalid", Port: 1080, Status: StatusActive}
	h := &hunterCoreHarness{now: time.Now().UTC(), repo: &hunterCoreRepository{accounts: map[int64]*Account{}}, proxyRepo: &hunterCoreProxyRepository{proxies: []Proxy{probeProxy}}}
	for id := int64(1); id <= int64(count); id++ {
		normalID := normalProxy.ID
		h.repo.accounts[id] = &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, ProxyID: &normalID, Proxy: &normalProxy,
			Credentials: map[string]any{"access_token": "fixture-token"},
			Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse", codexTurnStateCandidateLengthsExtraKey: []int{332}, openAITurnStateHunterExtraKey: map[string]any{
				"enabled": true, "models": []string{"gpt-6-astra"}, "proxy_ids": []int64{20}, "rotating_proxy_ids": []int64{20}, "max_per_hour": 8, "gap_seconds": 2, "idle_minutes": -1,
			}},
		}
	}
	gateway := &OpenAIGatewayService{accountRepo: h.repo}
	h.svc = NewOpenAITurnStateHunterService(gateway, h.repo, h.proxyRepo, nil, time.Minute)
	h.svc.now = func() time.Time { return h.now }
	h.svc.sleep = func(ctx context.Context, d time.Duration) error { h.now = h.now.Add(d); return ctx.Err() }
	return h
}

func (h *hunterCoreHarness) config(id int64) map[string]any {
	return h.repo.accounts[id].Extra[openAITurnStateHunterExtraKey].(map[string]any)
}
func (h *hunterCoreHarness) state(id int64) openAITurnStateHuntState {
	a, _ := h.repo.GetByID(context.Background(), id)
	return readOpenAITurnStateHuntState(a)
}

func TestCodexHunterCoreInterleavesAccountsAndKeepsNormalProxy(t *testing.T) {
	h := newHunterCoreHarness(t, 2)
	var order []int64
	counts := map[int64]int{}
	h.svc.probeFunc = func(_ context.Context, a *Account, model string, _ openAITurnStateHunterConfig, p *Proxy) openAITurnStateHuntAttempt {
		require.Equal(t, int64(1), *a.ProxyID)
		require.Equal(t, "socks5://normal.invalid:1080", a.Proxy.URL())
		require.Equal(t, int64(20), p.ID)
		order = append(order, a.ID)
		counts[a.ID]++
		return openAITurnStateHuntAttempt{Status: 200, Chars: 332, Healthy: counts[a.ID] == 2, ResponseModel: model}
	}
	h.svc.runOnce(context.Background())
	require.Len(t, order, 4)
	require.Equal(t, []int64{1, 2}, order[:2], "each ready account gets a probe before either is retried")
	require.ElementsMatch(t, []int64{1, 2}, order[2:], "random gaps may change the second round order")
	for _, a := range h.repo.accounts {
		require.Equal(t, int64(1), *a.ProxyID)
		require.Equal(t, StatusActive, a.Status)
		require.True(t, a.Schedulable)
	}
	for _, updates := range h.repo.writes {
		require.Len(t, updates, 1)
		require.Contains(t, updates, openAITurnStateHuntExtraKey)
	}
	require.Equal(t, 2, h.state(1).HourCount)
}

func TestCodexHunterCoreFixedExitOnceAndRotatingEndpointRepeats(t *testing.T) {
	for _, rotating := range []bool{false, true} {
		t.Run(map[bool]string{false: "fixed", true: "rotating"}[rotating], func(t *testing.T) {
			h := newHunterCoreHarness(t, 1)
			if !rotating {
				h.config(1)["rotating_proxy_ids"] = []int64{}
			}
			calls := 0
			h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
				calls++
				return openAITurnStateHuntAttempt{Status: 200, Chars: 312, Healthy: calls == 3}
			}
			h.svc.runOnce(context.Background())
			if rotating {
				require.Equal(t, 3, calls)
			} else {
				require.Equal(t, 1, calls)
				require.True(t, h.state(1).NextAt.After(h.now))
			}
		})
	}
}

func TestCodexHunterCoreTransientErrorsBoundedAndIsolated(t *testing.T) {
	for _, transport := range []bool{true, false} {
		t.Run(map[bool]string{true: "transport", false: "server"}[transport], func(t *testing.T) {
			h := newHunterCoreHarness(t, 1)
			calls := 0
			h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
				calls++
				return openAITurnStateHuntAttempt{Status: 503, Error: "fixture transient", transport: transport}
			}
			h.svc.runOnce(context.Background())
			require.Equal(t, 3, calls)
			require.Equal(t, h.now.Add(time.Hour), h.state(1).NextAt)
			if transport {
				require.Zero(t, h.state(1).HourCount)
			} else {
				require.Equal(t, 3, h.state(1).HourCount)
			}
			require.Equal(t, StatusActive, h.repo.accounts[1].Status)
		})
	}
}

func TestCodexHunterCoreUnauthorizedWaitsForCredentialChange(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 401, Error: "unauthorized"}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	require.NotEmpty(t, h.state(1).AuthBlockedCredential)
	h.now = h.now.Add(24 * time.Hour)
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	h.repo.accounts[1].Credentials["access_token"] = "replacement-fixture-token"
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Healthy: true, Chars: 332}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 2, calls)
	require.Empty(t, h.state(1).AuthBlockedCredential)
}

func TestCodexHunterCoreRateLimitHonorsRetryAfter(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 429, Error: "limited", RetryAfter: 2 * time.Hour}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, h.now.Add(2*time.Hour), h.state(1).NextAt)
	h.now = h.now.Add(time.Hour)
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
}

func TestCodexHunterCoreForbiddenAndPreflightDoNotRetryNodes(t *testing.T) {
	for _, attempt := range []openAITurnStateHuntAttempt{{Status: 403, Error: "forbidden"}, {preflight: true, Error: "token unavailable"}} {
		h := newHunterCoreHarness(t, 1)
		calls := 0
		h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
			calls++
			return attempt
		}
		h.svc.runOnce(context.Background())
		require.Equal(t, 1, calls)
		require.Equal(t, h.now.Add(10*time.Minute), h.state(1).NextAt)
		if attempt.preflight {
			require.Zero(t, h.state(1).HourCount)
		}
	}
}

func TestCodexHunterCoreConfigChangeStopsCurrentRound(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		h.config(1)["enabled"] = false
		return openAITurnStateHuntAttempt{Status: 200, Chars: 312}
	}
	h.svc.runOnce(context.Background())
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	require.Equal(t, false, h.config(1)["enabled"])
}

func TestCodexHunterCoreHourlyCapAndRaisedCap(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.config(1)["max_per_hour"] = 2
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Chars: 312}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 2, calls)
	require.True(t, h.state(1).CapWait)
	h.svc.runOnce(context.Background())
	require.Equal(t, 2, calls)
	h.config(1)["max_per_hour"] = 3
	h.svc.runOnce(context.Background())
	require.Equal(t, 3, calls)
}

func TestCodexHunterCoreIdleGateAndInvalidProxy(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.config(1)["idle_minutes"] = 60
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Healthy: true}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, "idle", h.state(1).Gate)
	require.Zero(t, calls)
	h.svc.gateway.noteOpenAITurnStateTraffic(1, "gpt-6-astra", h.now)
	h.proxyRepo.proxies[0].Status = "disabled"
	h.svc.runOnce(context.Background())
	require.Zero(t, calls)
	require.Equal(t, "no usable hunt proxy", h.state(1).LastError)
}

func TestCodexHunterCoreTrafficHintsAreBounded(t *testing.T) {
	h := newHunterCoreHarness(t, 0)
	for i := int64(1); i <= 5000; i++ {
		h.svc.gateway.noteOpenAITurnStateTraffic(i, "gpt-6-astra", h.now)
	}
	h.svc.gateway.sweepCodexHunterTraffic(h.now)
	count := 0
	h.svc.gateway.openaiTurnStateTraffic.Range(func(_, _ any) bool { count++; return true })
	require.Equal(t, 4096, count)
	h.svc.gateway.sweepCodexHunterTraffic(h.now.Add(25 * time.Hour))
	count = 0
	h.svc.gateway.openaiTurnStateTraffic.Range(func(_, _ any) bool { count++; return true })
	require.Zero(t, count)
}

func TestCodexHunterCoreStopCancelsAndJoins(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.svc.interval = time.Millisecond
	started := make(chan struct{}, 1)
	h.svc.probeFunc = func(ctx context.Context, _ *Account, _ string, _ openAITurnStateHunterConfig, _ *Proxy) openAITurnStateHuntAttempt {
		started <- struct{}{}
		<-ctx.Done()
		return openAITurnStateHuntAttempt{Error: "canceled", transport: true}
	}
	h.svc.Start()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background worker never started")
	}
	done := make(chan struct{})
	go func() { h.svc.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel and join")
	}
	h.svc.Stop()
}

func TestCodexHunterCoreRotatingRequiresExplicitModeOutsideWebshare(t *testing.T) {
	p := Proxy{ID: 20, Host: "provider.invalid", Username: "user-rotate"}
	require.False(t, openAITurnStateHuntProxyRotating(openAITurnStateHunterConfig{}, p))
	require.True(t, openAITurnStateHuntProxyRotating(openAITurnStateHunterConfig{RotatingProxyIDs: []int64{20}}, p))
	p.Host = "p.webshare.io"
	require.True(t, openAITurnStateHuntProxyRotating(openAITurnStateHunterConfig{}, p))
	p.Host = "not-webshare.io"
	require.False(t, openAITurnStateHuntProxyRotating(openAITurnStateHunterConfig{}, p))
}

func TestCodexHunterCoreStateDoesNotExposeCredentials(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		return openAITurnStateHuntAttempt{Status: http.StatusUnauthorized, Error: "unauthorized"}
	}
	h.svc.runOnce(context.Background())
	raw, err := json.Marshal(h.repo.writes)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fixture-token")
	require.NotContains(t, string(raw), "access_token")
}

func TestCodexHunterCoreModelMismatchKeepsHunting(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		if calls == 1 {
			return openAITurnStateHuntAttempt{Status: 200, Chars: 332, ResponseModel: "gpt-6-luna", Error: "model_mismatch"}
		}
		return openAITurnStateHuntAttempt{Status: 200, Chars: 332, ResponseModel: "gpt-6-astra", Healthy: true}
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 2, calls)
	require.Equal(t, 2, h.state(1).HourCount)
	require.Empty(t, h.state(1).Exits, "a model mismatch alone does not justify an IP cooldown")
}

func TestCodexHunterCoreDisableDuringGapStopsBeforeNextProbe(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Chars: 312}
	}
	h.svc.sleep = func(ctx context.Context, d time.Duration) error {
		h.now = h.now.Add(d)
		h.config(1)["enabled"] = false
		return ctx.Err()
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
}

func TestCodexHunterCoreProxyDisableDuringGapStopsBeforeNextProbe(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	calls := 0
	h.svc.probeFunc = func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Chars: 312}
	}
	h.svc.sleep = func(ctx context.Context, d time.Duration) error {
		h.now = h.now.Add(d)
		h.proxyRepo.proxies[0].Status = "disabled"
		return ctx.Err()
	}
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	require.Equal(t, "hunt proxy changed or unavailable", h.state(1).LastError)
}

func TestCodexHunterCoreRestartsRetainHourlyBudget(t *testing.T) {
	h := newHunterCoreHarness(t, 1)
	h.config(1)["max_per_hour"] = 1
	calls := 0
	probe := func(context.Context, *Account, string, openAITurnStateHunterConfig, *Proxy) openAITurnStateHuntAttempt {
		calls++
		return openAITurnStateHuntAttempt{Status: 200, Chars: 312}
	}
	h.svc.probeFunc = probe
	h.svc.runOnce(context.Background())
	require.Equal(t, 1, calls)
	next := NewOpenAITurnStateHunterService(h.svc.gateway, h.repo, h.proxyRepo, nil, time.Minute)
	next.now = h.svc.now
	next.sleep = h.svc.sleep
	next.probeFunc = probe
	next.runOnce(context.Background())
	require.Equal(t, 1, calls)
	h.now = h.now.Add(time.Hour)
	next.runOnce(context.Background())
	require.Equal(t, 2, calls)
}
