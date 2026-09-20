//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type hunterStatusSharedCache struct {
	GatewayCache
	codexHunterLocalStore
}

func hunterStatusFixture(t *testing.T) (*OpenAIGatewayService, *OpenAIGatewayService, *Account, *hunterStatusSharedCache, Proxy) {
	t.Helper()
	first, a, repo, worker, _ := newHunterIntegration(t, false)
	shared := &hunterStatusSharedCache{}
	first.cache = shared
	second := &OpenAIGatewayService{accountRepo: repo, cache: shared}
	secondWorker := NewOpenAITurnStateHunterService(second, repo, worker.proxyRepo, nil, time.Minute)
	second.openaiTurnStateHunter = secondWorker
	proxy := worker.proxyRepo.(*hunterCoreProxyRepository).proxies[0]
	return first, second, a, shared, proxy
}

func putHunterStatusCandidate(t *testing.T, svc *OpenAIGatewayService, a *Account, proxy Proxy, model, value string) CodexHunterCandidate {
	t.Helper()
	issued, expires, ok := parseOpenAICodexTurnStateCandidate(value, time.Now())
	require.True(t, ok)
	candidate := CodexHunterCandidate{Value: value, ProbeProxyID: proxy.ID, ProxyIdentity: codexHunterProxyIdentity(&proxy), IssuedUnix: issued.Unix(), ExpiresUnix: expires.Unix()}
	require.NoError(t, svc.hunterCandidateStore().PutCodexHunterCandidate(context.Background(), codexHunterStoreKey(a, model), candidate))
	return candidate
}

func TestCodexHunterStatusSharedCandidateSurvivesRestartWithoutInventedCounts(t *testing.T) {
	first, restarted, a, _, proxy := hunterStatusFixture(t)
	value := codexTeamHTTPFixtureState(t)
	expected := putHunterStatusCandidate(t, first, a, proxy, codexCollectionIntegrationModel, value)
	first.openaiCodexTurnStateCandidates.Observe(codexHunterScope(a), codexCollectionIntegrationModel, value, []int{332}, time.Now())
	require.Empty(t, restarted.openaiCodexTurnStateCandidates.Snapshot(codexHunterScope(a), time.Now()))
	for range 2 {
		status := restarted.CodexTurnStateStatus(context.Background(), a)
		require.True(t, status.HunterSharedCache)
		require.Len(t, status.HunterModels, 1)
		entry := status.HunterModels[0]
		require.Equal(t, codexCollectionIntegrationModel, entry.Model)
		require.NotNil(t, entry.Candidate)
		require.Equal(t, 332, entry.Candidate.Length)
		require.Equal(t, expected.IssuedUnix, entry.Candidate.IssuedAt.Unix())
		require.Equal(t, expected.ExpiresUnix, entry.Candidate.ExpiresAt.Unix())
		require.Zero(t, entry.ObservedCount)
		require.Zero(t, entry.ReuseAttemptCount)
		require.Zero(t, entry.Candidate.ObservedCount)
		require.Zero(t, entry.Candidate.ReuseCount)
		raw, err := json.Marshal(status)
		require.NoError(t, err)
		require.NotContains(t, string(raw), value)
		require.NotContains(t, string(raw), proxy.URL())
		require.NotContains(t, string(raw), "fixture-token")
	}
	require.Empty(t, restarted.openaiCodexTurnStateCandidates.Snapshot(codexHunterScope(a), time.Now()), "reading shared metadata must not create local observations")
}

func TestCodexHunterStatusPreservesRealLocalCounters(t *testing.T) {
	first, _, a, _, proxy := hunterStatusFixture(t)
	value := codexTeamHTTPFixtureState(t)
	putHunterStatusCandidate(t, first, a, proxy, codexCollectionIntegrationModel, value)
	scope := codexHunterScope(a)
	for range 2 {
		first.openaiCodexTurnStateCandidates.Observe(scope, codexCollectionIntegrationModel, value, []int{332}, time.Now())
	}
	_, selected := first.openaiCodexTurnStateCandidates.SelectCandidate(scope, codexCollectionIntegrationModel, []int{332}, time.Now())
	require.True(t, selected)
	before := first.openaiCodexTurnStateCandidates.Snapshot(scope, time.Now())
	for range 2 {
		got := first.codexHunterModelStatus(context.Background(), a)
		require.Len(t, got, 1)
		require.Equal(t, before[0].ObservedCount, got[0].ObservedCount)
		require.Equal(t, before[0].ReuseAttemptCount, got[0].ReuseAttemptCount)
		require.Equal(t, before[0].Candidate.ObservedCount, got[0].Candidate.ObservedCount)
		require.Equal(t, before[0].Candidate.ReuseCount, got[0].Candidate.ReuseCount)
	}
	after := first.openaiCodexTurnStateCandidates.Snapshot(scope, time.Now())
	require.Equal(t, before, after)
}

func TestCodexHunterStatusPeerClearAndTombstoneRemoveStaleMirror(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		t.Run(fmt.Sprint(tombstone), func(t *testing.T) {
			first, peer, a, _, proxy := hunterStatusFixture(t)
			value := codexTeamHTTPFixtureState(t)
			putHunterStatusCandidate(t, first, a, proxy, codexCollectionIntegrationModel, value)
			first.openaiCodexTurnStateCandidates.Observe(codexHunterScope(a), codexCollectionIntegrationModel, value, []int{332}, time.Now())
			if tombstone {
				require.NoError(t, peer.hunterCandidateStore().DeleteCodexHunterCandidate(context.Background(), codexHunterStoreKey(a, codexCollectionIntegrationModel), value))
			} else {
				// Peer has never observed this candidate locally; configured models
				// must still let its clear endpoint remove the shared state.
				peer.ClearCodexTurnState(context.Background(), a)
			}
			got := first.codexHunterModelStatus(context.Background(), a)
			require.Len(t, got, 1)
			require.Nil(t, got[0].Candidate)
			require.Equal(t, uint64(1), got[0].ObservedCount)
			local := first.openaiCodexTurnStateCandidates.Snapshot(codexHunterScope(a), time.Now())
			require.Len(t, local, 1)
			require.Nil(t, local[0].Candidate)
			require.Equal(t, uint64(1), local[0].ObservedCount)
		})
	}
}

func TestCodexHunterStatusFindsAutoModelsInRecentRuntime(t *testing.T) {
	first, restarted, a, _, proxy := hunterStatusFixture(t)
	cfg := a.Extra[openAITurnStateHunterExtraKey].(map[string]any)
	cfg["auto_models"] = true
	cfg["models"] = []string{}
	a.Extra[openAITurnStateHuntExtraKey] = openAITurnStateHuntState{Last: []openAITurnStateHuntAttempt{{Model: codexCollectionIntegrationModel, At: time.Now()}}}
	value := codexTeamHTTPFixtureState(t)
	putHunterStatusCandidate(t, first, a, proxy, codexCollectionIntegrationModel, value)
	got := restarted.codexHunterModelStatus(context.Background(), a)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Candidate)
	restarted.ClearCodexTurnState(context.Background(), a)
	got = first.codexHunterModelStatus(context.Background(), a)
	require.Len(t, got, 1)
	require.Nil(t, got[0].Candidate)
}

func TestCodexHunterKnownModelsPrioritizesConfigAndRuntimeWithinBound(t *testing.T) {
	first, _, a, _, _ := hunterStatusFixture(t)
	configModels := make([]string, 8)
	for i := range configModels {
		configModels[i] = fmt.Sprintf("configured-%02d", i)
	}
	a.Extra[openAITurnStateHunterExtraKey].(map[string]any)["models"] = configModels
	attempts := make([]openAITurnStateHuntAttempt, 12)
	for i := range attempts {
		attempts[i].Model = fmt.Sprintf("recent-%02d", i)
	}
	a.Extra[openAITurnStateHuntExtraKey] = openAITurnStateHuntState{Last: attempts}
	first.openaiCodexTurnStateCandidates.Observe(codexHunterScope(a), "local-tail", codexTeamHTTPFixtureState(t), []int{332}, time.Now())
	first.noteOpenAITurnStateTraffic(a.ID, "traffic-tail", time.Now())
	models := first.codexHunterKnownModels(a)
	require.Len(t, models, codexHunterStatusMaxModels)
	require.True(t, sort.StringsAreSorted(models))
	for _, model := range configModels {
		require.Contains(t, models, model)
	}
	for i := 0; i < 10; i++ {
		require.Contains(t, models, attempts[i].Model)
	}
	require.NotContains(t, models, "recent-10")
	require.NotContains(t, models, "local-tail")
	require.NotContains(t, models, "traffic-tail")
}

func TestCodexHunterKnownModelsIncludesLocalAndTrafficDeduplicated(t *testing.T) {
	first, _, a, _, _ := hunterStatusFixture(t)
	a.Extra[openAITurnStateHuntExtraKey] = openAITurnStateHuntState{Last: []openAITurnStateHuntAttempt{{Model: codexCollectionIntegrationModel}, {Model: "recent-model"}}}
	first.openaiCodexTurnStateCandidates.Observe(codexHunterScope(a), "local-model", codexTeamHTTPFixtureState(t), []int{332}, time.Now())
	first.noteOpenAITurnStateTraffic(a.ID, "traffic-model", time.Now())
	first.noteOpenAITurnStateTraffic(a.ID, "recent-model", time.Now())
	first.noteOpenAITurnStateTraffic(a.ID+1, "other-account", time.Now())
	first.noteOpenAITurnStateTraffic(a.ID, "stale-model", time.Now().Add(-25*time.Hour))
	first.noteOpenAITurnStateTraffic(a.ID, strings.Repeat("x", 129), time.Now())
	models := first.codexHunterKnownModels(a)
	require.ElementsMatch(t, []string{codexCollectionIntegrationModel, "recent-model", "local-model", "traffic-model"}, models)
}

type hunterStatusBlockingCache struct {
	GatewayCache
	calls    int
	deadline time.Time
}

func (s *hunterStatusBlockingCache) GetCodexHunterCandidate(ctx context.Context, _ string) (*CodexHunterCandidate, error) {
	s.calls++
	s.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s *hunterStatusBlockingCache) PutCodexHunterCandidate(context.Context, string, CodexHunterCandidate) error {
	return nil
}
func (s *hunterStatusBlockingCache) DeleteCodexHunterCandidate(context.Context, string, string) error {
	return nil
}

func TestCodexHunterStatusTimeoutDoesNotAdvertiseOrDeleteLocalCandidate(t *testing.T) {
	first, _, a, _, _ := hunterStatusFixture(t)
	value := codexTeamHTTPFixtureState(t)
	first.openaiCodexTurnStateCandidates.Observe(codexHunterScope(a), codexCollectionIntegrationModel, value, []int{332}, time.Now())
	blocked := &hunterStatusBlockingCache{}
	first.cache = blocked
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := first.codexHunterModelStatus(ctx, a)
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, 1, blocked.calls)
	require.LessOrEqual(t, blocked.deadline.Sub(start), codexHunterStatusTimeout)
	require.Len(t, got, 1)
	require.Nil(t, got[0].Candidate)
	local := first.openaiCodexTurnStateCandidates.Snapshot(codexHunterScope(a), time.Now())
	require.Len(t, local, 1)
	require.NotNil(t, local[0].Candidate, "an unavailable store does not prove rejection")
}

func TestCodexHunterStatusInvalidProxyDoesNotAdvertiseCachedState(t *testing.T) {
	first, _, a, _, proxy := hunterStatusFixture(t)
	value := codexTeamHTTPFixtureState(t)
	putHunterStatusCandidate(t, first, a, proxy, codexCollectionIntegrationModel, value)
	first.openaiCodexTurnStateCandidates.Observe(codexHunterScope(a), codexCollectionIntegrationModel, value, []int{332}, time.Now())
	first.openaiTurnStateHunter.proxyRepo.(*hunterCoreProxyRepository).proxies[0].Status = StatusDisabled
	got := first.codexHunterModelStatus(context.Background(), a)
	require.Len(t, got, 1)
	require.Nil(t, got[0].Candidate)
	require.Nil(t, first.openaiCodexTurnStateCandidates.Snapshot(codexHunterScope(a), time.Now())[0].Candidate)
}
