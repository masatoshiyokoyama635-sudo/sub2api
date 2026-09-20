//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newHunterIntegration(t *testing.T, passthrough bool, responses ...*http.Response) (*OpenAIGatewayService, *Account, *hunterCoreRepository, *OpenAITurnStateHunterService, *codexCollectionIntegrationUpstream) {
	t.Helper()
	svc, a, _, _, up := newCodexCollectionIntegrationRequest(t, passthrough, []byte(codexCollectionIntegrationBody), responses...)
	a.Status = StatusActive
	a.Proxy.Status = StatusActive
	a.Extra[openAITurnStateHunterExtraKey] = map[string]any{"enabled": true, "models": []string{codexCollectionIntegrationModel}, "proxy_ids": []int64{20}, "rotating_proxy_ids": []int64{20}, "idle_minutes": -1, "max_per_hour": 2, "gap_seconds": 1}
	repo := &hunterCoreRepository{accounts: map[int64]*Account{a.ID: cloneHunterCoreAccount(a)}}
	svc.accountRepo = repo
	worker := NewOpenAITurnStateHunterService(svc, repo, &hunterCoreProxyRepository{proxies: []Proxy{{ID: 20, Protocol: "http", Host: "hunt.invalid", Port: 8080, Status: StatusActive}}}, nil, time.Minute)
	worker.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	svc.openaiTurnStateHunter = worker
	return svc, a, repo, worker, up
}

func TestCodexHunterBackgroundCollectionThenNormalProxyGeneration(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "standard", true: "passthrough"}[passthrough], func(t *testing.T) {
			state := codexTeamHTTPFixtureState(t)
			svc, a, repo, worker, up := newHunterIntegration(t, passthrough, codexCollectionIntegrationResponse(200, "text/event-stream", state, codexCollectionIntegrationSSE(codexCollectionIntegrationModel)), codexCollectionIntegrationResponse(200, "text/event-stream", "", codexCollectionIntegrationSSE(codexCollectionIntegrationModel)))
			worker.runOnce(context.Background()) // no normal request has arrived
			require.Len(t, up.requests, 1)
			require.Equal(t, "http://hunt.invalid:8080", up.proxyURLs[0])
			require.True(t, up.requests[0].Close)
			require.True(t, HTTPUpstreamFreshConnection(up.requests[0].Context()))
			require.Empty(t, up.requests[0].Header.Get(openAICodexTurnStateHeader))
			require.NotContains(t, string(up.bodies[0]), "PRIVATE_")
			latest, err := repo.GetByID(context.Background(), a.ID)
			require.NoError(t, err)
			require.Equal(t, 1, readOpenAITurnStateHuntState(latest).HourCount)
			require.True(t, readOpenAITurnStateHuntState(latest).Last[0].Healthy)
			_, _, c, _, _ := newCodexCollectionIntegrationRequest(t, passthrough, []byte(codexCollectionIntegrationBody))
			result, err := svc.Forward(context.Background(), c, a, []byte(codexCollectionIntegrationBody))
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, up.requests, 2)
			require.Equal(t, a.Proxy.URL(), up.proxyURLs[1])
			require.Equal(t, state, up.requests[1].Header.Get(openAICodexTurnStateHeader))
			require.False(t, up.requests[1].Close)
			require.False(t, HTTPUpstreamFreshConnection(up.requests[1].Context()))
			require.Equal(t, int64(81), *a.ProxyID)
			raw, err := json.Marshal(svc.CodexTurnStateStatus(context.Background(), latest))
			require.NoError(t, err)
			require.NotContains(t, string(raw), state)
			require.NotContains(t, string(raw), "fixture-token")
		})
	}
}

func TestCodexHunterProxyConfigAndOptOutInvalidateCandidate(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	svc, a, _, worker, _ := newHunterIntegration(t, false, codexCollectionIntegrationResponse(200, "application/json", state, codexCollectionIntegrationJSON(codexCollectionIntegrationModel)))
	worker.runOnce(context.Background())
	require.NotNil(t, svc.codexHunterCandidate(context.Background(), a, codexCollectionIntegrationModel))
	p := worker.proxyRepo.(*hunterCoreProxyRepository)
	p.proxies[0].Host = "changed.invalid"
	require.Nil(t, svc.codexHunterCandidate(context.Background(), a, codexCollectionIntegrationModel))
	p.proxies[0].Host = "hunt.invalid"
	a.Extra[openAITurnStateHunterExtraKey].(map[string]any)["enabled"] = false
	require.Nil(t, svc.codexHunterCandidate(context.Background(), a, codexCollectionIntegrationModel))
	require.Empty(t, svc.openaiCodexTurnStateCandidates.Snapshot(codexTurnStateCandidateScope(a, a), time.Now()))
}

type hunterConfigChangingTransport struct {
	HTTPUpstream
	repo *hunterCoreRepository
	id   int64
}

func (u *hunterConfigChangingTransport) Do(req *http.Request, proxy string, id int64, n int) (*http.Response, error) {
	r, e := u.HTTPUpstream.Do(req, proxy, id, n)
	u.repo.mu.Lock()
	u.repo.accounts[u.id].Extra[openAITurnStateHunterExtraKey].(map[string]any)["enabled"] = false
	u.repo.mu.Unlock()
	return r, e
}

func TestCodexHunterDisabledDuringProbeDoesNotPublish(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	svc, a, repo, worker, up := newHunterIntegration(t, false, codexCollectionIntegrationResponse(200, "application/json", state, codexCollectionIntegrationJSON(codexCollectionIntegrationModel)))
	svc.httpUpstream = &hunterConfigChangingTransport{HTTPUpstream: up, repo: repo, id: a.ID}
	worker.runOnce(context.Background())
	require.Nil(t, svc.codexHunterCandidate(context.Background(), a, codexCollectionIntegrationModel))
}

func TestCodexHunterRejectsWrongModelAndKeepsLooking(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	svc, a, _, worker, up := newHunterIntegration(t, false, codexCollectionIntegrationResponse(200, "application/json", state, codexCollectionIntegrationJSON("gpt-5.6-luna")), codexCollectionIntegrationResponse(200, "application/json", state, codexCollectionIntegrationJSON(codexCollectionIntegrationModel)))
	worker.runOnce(context.Background())
	require.Len(t, up.requests, 2)
	require.NotNil(t, svc.codexHunterCandidate(context.Background(), a, codexCollectionIntegrationModel))
}
