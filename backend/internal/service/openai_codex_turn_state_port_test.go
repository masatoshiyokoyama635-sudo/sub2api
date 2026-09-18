package service

import (
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newTurnStateV2Account(id int64, credentialID string) *Account {
	return &Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": credentialID},
		Extra:       map[string]any{"codex_identity_version": "v2"},
	}
}

func TestCodexIdentityV2TurnStateTracksFirstBlobAcrossFailover(t *testing.T) {
	svc := &OpenAIGatewayService{}
	accountA := newTurnStateV2Account(41, "credential-a")
	accountB := newTurnStateV2Account(42, "credential-b")
	c, _ := newTurnStateTestContext(t, 7, "one-turn")
	relay := func(account *Account, state string) {
		h := http.Header{}
		h.Set(openAICodexTurnStateHeader, state)
		svc.relayOpenAICodexTurnState(c, account, h)
	}
	relay(accountA, "first-blob")
	relay(accountB, "later-blob")

	// The client kept the first blob in this turn even though B later minted
	// another. Both blob owners must remain independently attributable.
	for _, tc := range []struct {
		account *Account
		state   string
		want    string
	}{
		{accountA, "first-blob", "first-blob"},
		{accountB, "first-blob", ""},
		{accountA, "later-blob", ""},
		{accountB, "later-blob", "later-blob"},
		{accountB, "unknown-blob", "unknown-blob"},
	} {
		h := http.Header{}
		h.Set(openAICodexTurnStateHeader, tc.state)
		svc.guardOpenAICodexTurnStateEcho(c, tc.account, h)
		require.Equal(t, tc.want, h.Get(openAICodexTurnStateHeader))
	}

	// Headers are not required to recover a known blob owner: a reconnect can
	// have a different or absent downstream session and API key.
	otherContext, _ := newTurnStateTestContext(t, 99, "")
	require.Empty(t, svc.guardOpenAICodexTurnStateValue(otherContext, accountB, "first-blob"))
}

func TestCodexIdentityV2TurnStateSharesCredentialOwnerAndShadowSource(t *testing.T) {
	svc := &OpenAIGatewayService{}
	first := newTurnStateV2Account(41, "shared-credential")
	second := newTurnStateV2Account(42, "shared-credential")
	first.Credentials["chatgpt_user_id"] = "shared-user"
	second.Credentials["chatgpt_user_id"] = "shared-user"
	other := newTurnStateV2Account(43, "different-credential")
	svc.noteOpenAICodexTurnStateOrigin(nil, first, "shared-blob")
	require.Equal(t, "shared-blob", svc.guardOpenAICodexTurnStateValue(nil, second, "shared-blob"))
	require.Empty(t, svc.guardOpenAICodexTurnStateValue(nil, other, "shared-blob"))

	// The selected shadow has no credentials or version setting of its own.
	// It inherits both from the prepared parent used for outbound projection.
	c, _ := newTurnStateTestContext(t, 7, "shadow-session")
	c.Set(codexAccountIdentitySourceContextKey, first)
	shadow := &Account{ID: 141, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &first.ID}
	require.Equal(t, openAICodexTurnStateOwner(nil, first), openAICodexTurnStateOwner(c, shadow))
	require.Equal(t, "shared-blob", svc.guardOpenAICodexTurnStateValue(c, shadow, "shared-blob"))

	first.Credentials["chatgpt_user_id"] = "user-a"
	second.Credentials["chatgpt_user_id"] = "user-b"
	svc.noteOpenAICodexTurnStateOrigin(nil, first, "user-a-blob")
	require.Empty(t, svc.guardOpenAICodexTurnStateValue(nil, second, "user-a-blob"))
}

func TestCodexIdentityV2TurnStateRejectsKnownBlobAcrossVersionSwitch(t *testing.T) {
	svc := &OpenAIGatewayService{}
	legacy := newTurnStateV2Account(41, "shared-credential")
	legacy.Extra = nil
	v2 := newTurnStateV2Account(41, "shared-credential")
	c, _ := newTurnStateTestContext(t, 7, "same-session")
	svc.noteOpenAICodexTurnStateOrigin(c, legacy, "legacy-blob")
	svc.noteOpenAICodexTurnStateOrigin(c, v2, "v2-blob")

	for _, tc := range []struct {
		account *Account
		state   string
	}{
		{v2, "legacy-blob"},
		{legacy, "v2-blob"},
	} {
		h := http.Header{}
		h.Set(openAICodexTurnStateHeader, tc.state)
		svc.guardOpenAICodexTurnStateEcho(c, tc.account, h)
		require.Empty(t, h.Get(openAICodexTurnStateHeader))
		frame := []byte(`{"type":"response.create","client_metadata":{"session_id":"same-session","x-codex-turn-state":"` + tc.state + `"}}`)
		guarded := svc.guardOpenAICodexWSFrameTurnState(c, tc.account, frame)
		require.False(t, gjson.GetBytes(guarded, "client_metadata.x-codex-turn-state").Exists())
		require.Equal(t, "same-session", gjson.GetBytes(guarded, "client_metadata.session_id").String())
	}
}

func TestCodexIdentityV2TurnStateLeavesLegacyFrameAndSessionGuardUnchanged(t *testing.T) {
	svc := &OpenAIGatewayService{}
	first := &Account{ID: 41}
	second := &Account{ID: 42}
	c, _ := newTurnStateTestContext(t, 7, "legacy-session")
	svc.noteOpenAICodexTurnStateOrigin(c, first, "blob-a")
	svc.noteOpenAICodexTurnStateOrigin(c, second, "blob-b")

	// v1 deliberately retains its existing session-based decision until the
	// account opts into v2; merely deploying this code does not change it.
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, "blob-a")
	svc.guardOpenAICodexTurnStateEcho(c, second, h)
	require.Equal(t, "blob-a", h.Get(openAICodexTurnStateHeader))

	frame := []byte(`{ "type":"response.create", "client_metadata":{"x-codex-turn-state":"blob-a"}, "sequence":9007199254740993 }`)
	require.Equal(t, frame, svc.guardOpenAICodexWSFrameTurnState(c, second, frame))
	svc.noteOpenAICodexTurnStateFromWSEvent(c, first, []byte(`{"type":"response.metadata","headers":{"x-codex-turn-state":"ws-blob"}}`))
	raw, ok := svc.openaiCodexTurnStateOrigins.Load(openAICodexTurnStateSeed(c))
	require.True(t, ok)
	origin, valid := raw.(openAICodexTurnStateOrigin)
	require.True(t, valid)
	require.Equal(t, second.ID, origin.accountID, "WS observations must not change v1 session provenance")
}

func TestCodexIdentityV2TurnStateRecordsOnlyCommittedHeader(t *testing.T) {
	svc := &OpenAIGatewayService{}
	first := newTurnStateV2Account(41, "credential-a")
	second := newTurnStateV2Account(42, "credential-b")
	c, _ := newTurnStateTestContext(t, 7, "one-turn")
	var staged http.Header
	upstream := http.Header{}
	upstream.Set(openAICodexTurnStateHeader, "abandoned-blob")
	stageOpenAICodexTurnState(&staged, upstream)
	require.Equal(t, "abandoned-blob", svc.guardOpenAICodexTurnStateValue(c, second, "abandoned-blob"))
	_, observed := svc.openaiCodexTurnStateV2Origins.load(openAICodexTurnStateKey("abandoned-blob"), time.Now())
	require.False(t, observed)

	// A failover abandons the old staged response and commits a different blob.
	upstream.Set(openAICodexTurnStateHeader, "committed-blob")
	stageOpenAICodexTurnState(&staged, upstream)
	svc.noteStagedOpenAICodexTurnStateCommitted(c, first, staged)
	require.Empty(t, svc.guardOpenAICodexTurnStateValue(c, second, "committed-blob"))
	require.Equal(t, "abandoned-blob", svc.guardOpenAICodexTurnStateValue(c, second, "abandoned-blob"))
}

func TestCodexIdentityV2TurnStateObservesWSMetadataAndGuardsOnlyItsBlob(t *testing.T) {
	svc := &OpenAIGatewayService{}
	first := newTurnStateV2Account(41, "credential-a")
	second := newTurnStateV2Account(42, "credential-b")
	svc.noteOpenAICodexTurnStateFromWSEvent(nil, first, []byte(`{"type":"response.metadata","headers":{"X-CoDeX-TuRn-StAtE":"ws-blob"}}`))
	frame := []byte(`{"type":"response.create","client_metadata":{"X-Codex-Turn-State":"ws-blob","thread_id":"thread-a"},"sequence":9007199254740993}`)
	guarded := svc.guardOpenAICodexWSFrameTurnState(nil, second, frame)
	require.False(t, gjson.GetBytes(guarded, "client_metadata.X-Codex-Turn-State").Exists())
	require.Equal(t, "thread-a", gjson.GetBytes(guarded, "client_metadata.thread_id").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(guarded, "sequence").Raw)
	require.Equal(t, frame, svc.guardOpenAICodexWSFrameTurnState(nil, first, frame))
	// The compatibility bridge can remove the WS envelope's type before the
	// final HTTP write. Ownership still applies to its client metadata.
	bridgeBody := []byte(`{"model":"gpt-5.1","client_metadata":{"x-codex-turn-state":"ws-blob"}}`)
	require.False(t, gjson.GetBytes(svc.guardOpenAICodexWSFrameTurnState(nil, second, bridgeBody), "client_metadata.x-codex-turn-state").Exists())

	for _, invalidEvent := range []string{
		`{"type":"response.created","headers":{"x-codex-turn-state":"not-observed"}}`,
		`{"type":"response.metadata","headers":{"x-codex-turn-state":123}}`,
		`{"type":"response.metadata","headers":{"x-codex-turn-state":"not-observed"}`,
	} {
		svc.noteOpenAICodexTurnStateFromWSEvent(nil, first, []byte(invalidEvent))
	}
	require.Equal(t, "not-observed", svc.guardOpenAICodexTurnStateValue(nil, second, "not-observed"))
	require.Equal(t, "123", svc.guardOpenAICodexTurnStateValue(nil, second, "123"))
}

func TestCodexIdentityV2TurnStateExpiresAndBoundsMemory(t *testing.T) {
	cache := &openAICodexTurnStateCache{}
	now := time.Now()
	key := openAICodexTurnStateKey("expiring-blob")
	cache.store(key, openAICodexTurnStateBlobOrigin{owner: "owner-a", expiresAt: now.Add(time.Second)}, now)
	_, ok := cache.load(key, now.Add(time.Second))
	require.False(t, ok, "TTL expiry includes the exact deadline")
	require.Empty(t, cache.entries)

	for i := 0; i < openAICodexTurnStateMaxOrigins+1; i++ {
		cache.store(openAICodexTurnStateKey(strconv.Itoa(i)), openAICodexTurnStateBlobOrigin{
			owner: "owner-a", expiresAt: now.Add(time.Hour),
		}, now)
	}
	require.Len(t, cache.entries, openAICodexTurnStateMaxOrigins)
	require.Equal(t, openAICodexTurnStateMaxOrigins, cache.order.Len())
	_, oldestPresent := cache.load(openAICodexTurnStateKey("0"), now)
	require.False(t, oldestPresent, "the oldest write is evicted at capacity")
	_, newestPresent := cache.load(openAICodexTurnStateKey(strconv.Itoa(openAICodexTurnStateMaxOrigins)), now)
	require.True(t, newestPresent)

	cache.store(openAICodexTurnStateKey("new-after-expiry"), openAICodexTurnStateBlobOrigin{
		owner: "owner-b", expiresAt: now.Add(3 * time.Hour),
	}, now.Add(2*time.Hour))
	require.Len(t, cache.entries, 1, "subsequent writes remove the expired prefix")
}

func TestCodexIdentityV2TurnStateUnknownAfterExpiryOrRestartPassesThrough(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := newTurnStateV2Account(42, "credential-b")
	now := time.Now()
	svc.openaiCodexTurnStateV2Origins.store(openAICodexTurnStateKey("expired-blob"), openAICodexTurnStateBlobOrigin{
		owner: "different-owner", version: "v2", expiresAt: now.Add(-time.Second),
	}, now)
	require.Equal(t, "expired-blob", svc.guardOpenAICodexTurnStateValue(nil, account, "expired-blob"))
	svc.noteOpenAICodexTurnStateOrigin(nil, newTurnStateV2Account(41, "credential-a"), "foreign-instance-blob")
	require.Empty(t, svc.guardOpenAICodexTurnStateValue(nil, account, "foreign-instance-blob"))
	otherInstance := &OpenAIGatewayService{}
	require.Equal(t, "foreign-instance-blob", otherInstance.guardOpenAICodexTurnStateValue(nil, account, "foreign-instance-blob"))
}

func TestCodexIdentityV2TurnStateCacheConcurrentReadAndWrite(t *testing.T) {
	cache := &openAICodexTurnStateCache{}
	now := time.Now()
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 256; i++ {
				key := openAICodexTurnStateKey(strconv.Itoa((worker + i) % 512))
				cache.store(key, openAICodexTurnStateBlobOrigin{owner: "owner-a", expiresAt: now.Add(time.Hour)}, now)
				cache.load(key, now)
			}
		}(worker)
	}
	wg.Wait()
	require.Equal(t, len(cache.entries), cache.order.Len(), "overwriting a key must not leak list entries")
	require.LessOrEqual(t, len(cache.entries), 512)
}
