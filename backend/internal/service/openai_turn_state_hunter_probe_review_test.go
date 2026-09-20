//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type hunterCancelAtEOFBody struct {
	io.Reader
	cancel context.CancelFunc
}

func (b *hunterCancelAtEOFBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		b.cancel()
	}
	return n, err
}
func (b *hunterCancelAtEOFBody) Close() error { return nil }

func TestCodexHunterProbeBillsObservedUsageAfterReadCancellation(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	body := `{"status":"completed","model":"` + codexCollectionIntegrationModel + `","usage":{"input_tokens":40,"output_tokens":3}}`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response := codexCollectionIntegrationResponse(200, "application/json", state, body)
	response.Body = &hunterCancelAtEOFBody{Reader: strings.NewReader(body), cancel: cancel}
	svc, a, _, worker, _ := newHunterIntegration(t, false, response)
	worker.SetAPIKeys(&codexHunterUsageKeyStub{key: codexHunterUsageKey()})
	var recorded *OpenAIRecordUsageInput
	worker.recordUsage = func(billingCtx context.Context, input *OpenAIRecordUsageInput) error {
		require.NoError(t, billingCtx.Err(), "probe cancellation must not discard reported usage")
		deadline, ok := billingCtx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
		recorded = input
		return nil
	}
	cfg, _ := readOpenAITurnStateHunterConfig(a)
	cfg.UsageAPIKeyID = 20
	proxy := worker.proxyRepo.(*hunterCoreProxyRepository).proxies[0]
	attempt := svc.probeCodexHunter(ctx, a, codexCollectionIntegrationModel, cfg, &proxy)
	require.Equal(t, "incomplete_response", attempt.Error)
	require.False(t, attempt.Healthy)
	require.NotNil(t, recorded)
	require.Equal(t, 40, recorded.Result.Usage.InputTokens)
	require.Equal(t, 3, recorded.Result.Usage.OutputTokens)
	require.Nil(t, svc.codexHunterCandidate(context.Background(), a, codexCollectionIntegrationModel))
}

func TestCodexHunterProbeBillingRetainsModelConflictAndResponseTier(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	body := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"model\":\"gpt-6-luna\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"" + codexCollectionIntegrationModel + "\",\"service_tier\":\"priority\",\"usage\":{\"input_tokens\":40,\"output_tokens\":3}}}\n\n"
	svc, a, _, worker, _ := newHunterIntegration(t, false, codexCollectionIntegrationResponse(http.StatusOK, "text/event-stream", state, body))
	worker.SetAPIKeys(&codexHunterUsageKeyStub{key: codexHunterUsageKey()})
	var recorded *OpenAIRecordUsageInput
	worker.recordUsage = func(_ context.Context, input *OpenAIRecordUsageInput) error { recorded = input; return nil }
	cfg, _ := readOpenAITurnStateHunterConfig(a)
	cfg.UsageAPIKeyID = 20
	proxy := worker.proxyRepo.(*hunterCoreProxyRepository).proxies[0]
	proxy.Name = "socks5://secret-user:secret-password@fixture.invalid"
	attempt := svc.probeCodexHunter(context.Background(), a, codexCollectionIntegrationModel, cfg, &proxy)
	require.Equal(t, "model_mismatch", attempt.Error)
	require.False(t, attempt.Healthy)
	require.Equal(t, "#20", attempt.Proxy)
	require.NotNil(t, recorded)
	require.True(t, recorded.Result.UpstreamResponseModelConflict)
	require.Equal(t, "priority", recorded.Result.UpstreamResponseServiceTier)
	require.Equal(t, 40, recorded.Result.Usage.InputTokens)
}

func TestCodexHunterProbeDoesNotClaimHealthyWhenRejectedStateIsNotRetained(t *testing.T) {
	state := codexTeamHTTPFixtureState(t)
	svc, a, _, worker, _ := newHunterIntegration(t, false, codexCollectionIntegrationResponse(http.StatusOK, "application/json", state, codexCollectionIntegrationJSON(codexCollectionIntegrationModel)))
	proxy := worker.proxyRepo.(*hunterCoreProxyRepository).proxies[0]
	issued, expires, ok := parseOpenAICodexTurnStateCandidate(state, time.Now())
	require.True(t, ok)
	key := codexHunterStoreKey(a, codexCollectionIntegrationModel)
	stored := CodexHunterCandidate{Value: state, ProbeProxyID: proxy.ID, ProxyIdentity: codexHunterProxyIdentity(&proxy), IssuedUnix: issued.Unix(), ExpiresUnix: expires.Unix()}
	require.NoError(t, svc.hunterCandidateStore().PutCodexHunterCandidate(context.Background(), key, stored))
	require.NoError(t, svc.hunterCandidateStore().DeleteCodexHunterCandidate(context.Background(), key, state))
	cfg, _ := readOpenAITurnStateHunterConfig(a)
	attempt := svc.probeCodexHunter(context.Background(), a, codexCollectionIntegrationModel, cfg, &proxy)
	require.False(t, attempt.Healthy)
	require.Equal(t, "candidate_not_retained", attempt.Error)
	require.Empty(t, svc.openaiCodexTurnStateCandidates.Snapshot(codexHunterScope(a), time.Now()))
}

func TestCodexHunterProbeMirrorsNewerRetainedState(t *testing.T) {
	oldState := testCodexTurnStateEnvelope(time.Now().Add(-2*time.Minute), 12, 1)
	newState := testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 12, 2)
	svc, a, _, worker, _ := newHunterIntegration(t, false, codexCollectionIntegrationResponse(http.StatusOK, "application/json", oldState, codexCollectionIntegrationJSON(codexCollectionIntegrationModel)))
	proxy := worker.proxyRepo.(*hunterCoreProxyRepository).proxies[0]
	issued, expires, ok := parseOpenAICodexTurnStateCandidate(newState, time.Now())
	require.True(t, ok)
	key := codexHunterStoreKey(a, codexCollectionIntegrationModel)
	stored := CodexHunterCandidate{Value: newState, ProbeProxyID: proxy.ID, ProxyIdentity: codexHunterProxyIdentity(&proxy), IssuedUnix: issued.Unix(), ExpiresUnix: expires.Unix()}
	require.NoError(t, svc.hunterCandidateStore().PutCodexHunterCandidate(context.Background(), key, stored))
	cfg, _ := readOpenAITurnStateHunterConfig(a)
	attempt := svc.probeCodexHunter(context.Background(), a, codexCollectionIntegrationModel, cfg, &proxy)
	require.True(t, attempt.Healthy)
	mirrored, present := svc.openaiCodexTurnStateCandidates.Candidate(codexHunterScope(a), codexCollectionIntegrationModel, time.Now())
	require.True(t, present)
	require.Equal(t, newState, mirrored)
}
