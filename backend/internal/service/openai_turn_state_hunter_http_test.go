//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexHunterGenerationValidatesSuccessorAndActualModel(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, scenario := range []string{"successor", "echo", "wrong_model", "disabled"} {
			name := map[bool]string{false: "standard", true: "passthrough"}[passthrough] + "/" + scenario
			t.Run(name, func(t *testing.T) {
				old := testCodexTurnStateEnvelope(time.Now().Add(-5*time.Minute), 12, 41)
				fresh := testCodexTurnStateEnvelope(time.Now().Add(-time.Minute), 12, 42)
				responseModel := codexCollectionIntegrationModel
				if scenario == "echo" {
					fresh = old
				}
				if scenario == "wrong_model" {
					responseModel = "gpt-5.6-luna"
				}
				svc, a, repo, worker, up := newHunterIntegration(t, passthrough,
					codexCollectionIntegrationResponse(200, "text/event-stream", old, codexCollectionIntegrationSSE(codexCollectionIntegrationModel)),
					codexCollectionIntegrationResponse(200, "text/event-stream", fresh, codexCollectionIntegrationSSE(responseModel)))
				worker.runOnce(context.Background())
				before := svc.codexHunterCandidate(context.Background(), a, codexCollectionIntegrationModel)
				require.NotNil(t, before)
				if scenario == "disabled" {
					svc.httpUpstream = &hunterConfigChangingTransport{HTTPUpstream: up, repo: repo, id: a.ID}
				}
				_, _, c, _, _ := newCodexCollectionIntegrationRequest(t, passthrough, []byte(codexCollectionIntegrationBody))
				_, err := svc.Forward(context.Background(), c, a, []byte(codexCollectionIntegrationBody))
				require.NoError(t, err)
				require.Len(t, up.requests, 2)
				require.Equal(t, old, up.requests[1].Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, a.Proxy.URL(), up.proxyURLs[1])
				after, err := svc.hunterCandidateStore().GetCodexHunterCandidate(context.Background(), codexHunterStoreKey(a, codexCollectionIntegrationModel))
				require.NoError(t, err)
				switch scenario {
				case "wrong_model":
					require.Nil(t, after, "an explicitly mismatched response retires the borrowed candidate")
				case "disabled", "echo":
					require.Equal(t, before, after, "no successor publication or expiry renewal")
				default:
					require.NotNil(t, after)
					require.Equal(t, fresh, after.Value)
					require.Greater(t, after.IssuedUnix, before.IssuedUnix)
				}
				_, retained := svc.openaiCodexTurnStateCandidates.Candidate(codexTurnStateCandidateScope(a, a), codexCollectionIntegrationModel, time.Now())
				require.False(t, retained, "Hunter candidates must not leak into the passive pool")
			})
		}
	}
}
