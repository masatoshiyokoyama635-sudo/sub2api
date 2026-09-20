package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func codexHunterLocalTestCandidate(issued time.Time, fill byte) CodexHunterCandidate {
	return CodexHunterCandidate{Value: testCodexTurnStateEnvelope(issued, 12, fill), IssuedUnix: issued.Unix(), ExpiresUnix: issued.Add(time.Hour).Unix(), ProbeProxyID: 20, ProxyIdentity: strings.Repeat("a", 64)}
}

func TestCodexHunterLocalCacheRejectAndReplacement(t *testing.T) {
	ctx := context.Background()
	var cache codexHunterLocalStore
	old := codexHunterLocalTestCandidate(time.Now().Add(-2*time.Minute), 1)
	replacement := codexHunterLocalTestCandidate(time.Now().Add(-time.Minute), 2)
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", old))
	require.NoError(t, cache.DeleteCodexHunterCandidate(ctx, "model", old.Value))
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", old))
	got, err := cache.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", replacement))
	require.NoError(t, cache.DeleteCodexHunterCandidate(ctx, "model", old.Value))
	got, err = cache.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Equal(t, replacement.Value, got.Value)
	require.NoError(t, cache.DeleteCodexHunterCandidate(ctx, "absent", old.Value))
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "absent", old))
	got, err = cache.GetCodexHunterCandidate(ctx, "absent")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestCodexHunterScopeBindsCandidateLengths(t *testing.T) {
	a := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: hunterSettingsTestExtra()}
	original := codexHunterScope(a)
	a.Extra[codexTurnStateCandidateLengthsExtraKey] = []int{332}
	require.NotEqual(t, original, codexHunterScope(a))
	a.Extra[codexTurnStateCandidateLengthsExtraKey] = []int{292, 332}
	require.Equal(t, original, codexHunterScope(a))
	gateway := &OpenAIGatewayService{}
	require.True(t, gateway.codexHunterReusable(a))
	a.Extra[codexIdentityVersionExtraKey] = "v1"
	require.False(t, gateway.codexHunterReusable(a))
}
