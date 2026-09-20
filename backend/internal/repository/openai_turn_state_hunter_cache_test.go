package repository

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCodexHunterCacheTest(t *testing.T) (*gatewayCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &gatewayCache{rdb: client}, server
}

func codexHunterCacheCandidate(issued time.Time, fill byte) service.CodexHunterCandidate {
	raw := make([]byte, 1+8+16+16*12+32)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issued.Unix()))
	for i := 9; i < len(raw); i++ {
		raw[i] = fill
	}
	return service.CodexHunterCandidate{Value: base64.URLEncoding.EncodeToString(raw), IssuedUnix: issued.Unix(), ExpiresUnix: issued.Add(time.Hour).Unix(), ProbeProxyID: 20, ProxyIdentity: strings.Repeat("a", 64)}
}

func TestCodexHunterSharedCacheKeepsNewestWithoutRefreshingTTL(t *testing.T) {
	cache, server := newCodexHunterCacheTest(t)
	ctx := context.Background()
	value := codexHunterCacheCandidate(time.Now().Add(-2*time.Minute), 1)
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", value))
	initial := server.TTL(codexHunterCachePrefix + "model")
	require.Positive(t, initial)
	require.LessOrEqual(t, initial, time.Hour)
	server.FastForward(10 * time.Second)
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", value))
	require.Equal(t, initial-10*time.Second, server.TTL(codexHunterCachePrefix+"model"))
	older := codexHunterCacheCandidate(time.Now().Add(-5*time.Minute), 2)
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", older))
	otherInstance := &gatewayCache{rdb: cache.rdb}
	got, err := otherInstance.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Equal(t, value, *got)
	server.FastForward(time.Hour)
	got, err = otherInstance.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestCodexHunterSharedCacheRejectedStateCannotResurrect(t *testing.T) {
	cache, server := newCodexHunterCacheTest(t)
	ctx := context.Background()
	old := codexHunterCacheCandidate(time.Now().Add(-4*time.Minute), 1)
	newer := codexHunterCacheCandidate(time.Now().Add(-3*time.Minute), 2)
	latest := codexHunterCacheCandidate(time.Now().Add(-2*time.Minute), 3)
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", old))
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", newer))
	require.NoError(t, cache.DeleteCodexHunterCandidate(ctx, "model", old.Value))
	got, err := cache.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Equal(t, newer.Value, got.Value, "late rejection of old candidate must preserve replacement")
	ttl := server.TTL(codexHunterCachePrefix + "model")
	require.NoError(t, cache.DeleteCodexHunterCandidate(ctx, "model", newer.Value))
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", newer))
	got, err = cache.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Nil(t, got, "a late writer must not revive a rejected candidate")
	require.LessOrEqual(t, server.TTL(codexHunterCachePrefix+"model"), ttl)
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", latest))
	got, err = cache.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Equal(t, latest.Value, got.Value)
	require.NoError(t, cache.DeleteCodexHunterCandidate(ctx, "absent", old.Value))
	require.NoError(t, cache.PutCodexHunterCandidate(ctx, "absent", old))
	got, err = cache.GetCodexHunterCandidate(ctx, "absent")
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, cache.DeleteCodexHunterCandidate(ctx, "absent", ""))
	require.False(t, server.Exists(codexHunterCachePrefix+"absent"), "admin clear removes the tombstone")
}

func TestCodexHunterSharedCacheRepairsMalformedEntries(t *testing.T) {
	cache, _ := newCodexHunterCacheTest(t)
	ctx := context.Background()
	fresh := codexHunterCacheCandidate(time.Now().Add(-time.Minute), 1)
	for _, payload := range []string{"not-json", "{}", "null", "4", "[]", `{"issued_unix":"invalid","value":"x"}`} {
		require.NoError(t, cache.rdb.Set(ctx, codexHunterCachePrefix+"model", payload, time.Hour).Err())
		require.NoError(t, cache.PutCodexHunterCandidate(ctx, "model", fresh), payload)
		got, err := cache.GetCodexHunterCandidate(ctx, "model")
		require.NoError(t, err)
		require.Equal(t, fresh.Value, got.Value)
	}
	expired := codexHunterCacheCandidate(time.Now().Add(-2*time.Hour), 2)
	raw, err := json.Marshal(expired)
	require.NoError(t, err)
	require.NoError(t, cache.rdb.Set(ctx, codexHunterCachePrefix+"model", raw, time.Hour).Err())
	got, err := cache.GetCodexHunterCandidate(ctx, "model")
	require.NoError(t, err)
	require.Nil(t, got, "a stale TTL must not make an expired envelope usable")
	require.Error(t, cache.PutCodexHunterCandidate(ctx, "new", expired))
	fresh.ExpiresUnix++
	require.Error(t, cache.PutCodexHunterCandidate(ctx, "new", fresh), "metadata must match the envelope lifetime")
}

func TestCodexHunterSharedCacheConcurrentWritersKeepNewest(t *testing.T) {
	cache, _ := newCodexHunterCacheTest(t)
	now := time.Now().Add(-time.Minute)
	var group sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			errors <- cache.PutCodexHunterCandidate(context.Background(), "model", codexHunterCacheCandidate(now.Add(-time.Duration(i)*time.Second), byte(i)))
		}(i)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	got, err := cache.GetCodexHunterCandidate(context.Background(), "model")
	require.NoError(t, err)
	require.Equal(t, now.Unix(), got.IssuedUnix)
}
