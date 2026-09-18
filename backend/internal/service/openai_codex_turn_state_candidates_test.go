package service

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This creates an envelope, not a signed Fernet token. The cache intentionally
// cannot verify upstream signatures and must never present this as such.
func testCodexTurnStateEnvelope(issuedAt time.Time, cipherBlocks int, fill byte) string {
	raw := make([]byte, 1+8+16+16*cipherBlocks+32)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issuedAt.Unix()))
	for i := 9; i < len(raw); i++ {
		raw[i] = fill
	}
	return base64.URLEncoding.EncodeToString(raw)
}

func TestCodexTurnStateCandidatesAcceptConfigured292AndTeam332(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	var cache openAICodexTurnStateCandidates
	legacy := testCodexTurnStateEnvelope(now.Add(-10*time.Minute), 10, 1)
	team := testCodexTurnStateEnvelope(now.Add(-5*time.Minute), 12, 2)
	otherLength := testCodexTurnStateEnvelope(now, 11, 3)
	require.Len(t, legacy, 292)
	require.Len(t, team, 332)
	require.Len(t, otherLength, 312)

	cache.Observe("member-a", "gpt-test", legacy, []int{292, 332}, now)
	value, ok := cache.Candidate("member-a", "gpt-test", now)
	require.True(t, ok)
	require.Equal(t, legacy, value)
	cache.Observe("member-a", "gpt-test", team, []int{292, 332}, now)
	cache.Observe("member-a", "gpt-test", otherLength, []int{292, 332}, now)
	// An old response arriving late must not replace the newer candidate.
	cache.Observe("member-a", "gpt-test", legacy, []int{292, 332}, now)
	value, ok = cache.Candidate("member-a", "gpt-test", now)
	require.True(t, ok)
	require.Equal(t, team, value)

	snapshots := cache.Snapshot("member-a", now)
	require.Len(t, snapshots, 1)
	require.Equal(t, uint64(4), snapshots[0].ObservedCount)
	require.Equal(t, []CodexTurnStateLengthCount{{Length: 292, Count: 2}, {Length: 312, Count: 1}, {Length: 332, Count: 1}}, snapshots[0].Lengths)
	require.Equal(t, 332, snapshots[0].Candidate.Length)
	require.Equal(t, now.Add(-5*time.Minute), snapshots[0].Candidate.IssuedAt)
	require.Equal(t, now.Add(55*time.Minute), snapshots[0].Candidate.ExpiresAt)
}

func TestCodexTurnStateCandidatesObserveOnlyWithoutLengthPolicy(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	value := testCodexTurnStateEnvelope(now, 12, 1)
	for _, lengths := range [][]int{nil, {}} {
		var cache openAICodexTurnStateCandidates
		cache.Observe("member-a", "gpt-test", value, lengths, now)
		_, ok := cache.Candidate("member-a", "gpt-test", now)
		require.False(t, ok)
		snapshots := cache.Snapshot("member-a", now)
		require.Equal(t, uint64(1), snapshots[0].ObservedCount)
		require.Equal(t, []CodexTurnStateLengthCount{{Length: 332, Count: 1}}, snapshots[0].Lengths)
		require.Nil(t, snapshots[0].Candidate)
	}
}

func TestCodexTurnStateCandidatesIsolateCredentialsModelsAndClear(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	value := testCodexTurnStateEnvelope(now, 12, 1)
	cache.Observe("workspace/member-a", "gpt-a", value, []int{332}, now)
	_, ok := cache.Candidate("workspace/member-b", "gpt-a", now)
	require.False(t, ok)
	_, ok = cache.Candidate("workspace/member-a", "gpt-b", now)
	require.False(t, ok)
	cache.Observe("workspace/member-a", "gpt-b", value, []int{332}, now)
	cache.Observe("workspace/member-b", "gpt-a", value, []int{332}, now)
	cache.Clear("workspace/member-a")
	require.Empty(t, cache.Snapshot("workspace/member-a", now))
	_, ok = cache.Candidate("workspace/member-b", "gpt-a", now)
	require.True(t, ok)
}

func TestCodexTurnStateCandidatesNeverRefreshLifetimeFromObservation(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-10 * time.Minute)
	expiresAt := issuedAt.Add(time.Hour)
	var cache openAICodexTurnStateCandidates
	value := testCodexTurnStateEnvelope(issuedAt, 12, 1)
	cache.Observe("member-a", "gpt-test", value, []int{332}, now)
	cache.Observe("member-a", "gpt-test", value, []int{332}, now.Add(20*time.Minute))
	_, ok := cache.Candidate("member-a", "gpt-test", expiresAt.Add(-time.Nanosecond))
	require.True(t, ok)
	candidate := cache.Snapshot("member-a", expiresAt.Add(-time.Nanosecond))[0].Candidate
	require.Equal(t, expiresAt, candidate.ExpiresAt)
	require.Equal(t, uint64(2), candidate.ObservedCount)
	require.Equal(t, uint64(1), candidate.ReuseCount)
	require.Equal(t, expiresAt.Add(-time.Nanosecond), *candidate.LastReusedAt)

	_, ok = cache.Candidate("member-a", "gpt-test", expiresAt)
	require.False(t, ok, "the exact expiration deadline is already expired")
	cache.Observe("member-a", "gpt-test", value, []int{332}, expiresAt)
	snapshots := cache.Snapshot("member-a", expiresAt)
	require.Nil(t, snapshots[0].Candidate, "repeat observation must not resurrect an expired token")
	require.Equal(t, uint64(3), snapshots[0].ObservedCount)
}

func TestCodexTurnStateCandidatesRejectMalformedFutureAndExpiredEnvelopes(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	valid := testCodexTurnStateEnvelope(now.Add(-time.Minute), 12, 2)
	decode := func(token string) []byte {
		raw, err := base64.URLEncoding.DecodeString(token)
		require.NoError(t, err)
		return raw
	}
	badVersion := decode(valid)
	badVersion[0] = 0x81
	badTimestamp := decode(valid)
	binary.BigEndian.PutUint64(badTimestamp[1:9], ^uint64(0))
	unpadded := strings.TrimRight(testCodexTurnStateEnvelope(now, 10, 1), "=")
	nonCanonical := unpadded[:len(unpadded)-1] + "R=="
	for name, invalid := range map[string]string{
		"invalid-base64":     "!" + valid[1:],
		"wrong-version":      base64.URLEncoding.EncodeToString(badVersion),
		"overflow-timestamp": base64.URLEncoding.EncodeToString(badTimestamp),
		"future":             testCodexTurnStateEnvelope(now.Add(time.Second), 12, 1),
		"expired":            testCodexTurnStateEnvelope(now.Add(-time.Hour), 12, 1),
		"too-old":            testCodexTurnStateEnvelope(now.Add(-24*time.Hour), 12, 1),
		"missing-ciphertext": testCodexTurnStateEnvelope(now, 0, 1),
		"misaligned-cipher":  base64.URLEncoding.EncodeToString(decode(valid)[:len(decode(valid))-1]),
		"short-envelope":     base64.URLEncoding.EncodeToString([]byte{0x80, 0, 0, 0}),
		"embedded-newline":   valid[:20] + "\n" + valid[20:],
		"leading-space":      " " + valid,
		"noncanonical-bits":  nonCanonical,
		"too-large":          testCodexTurnStateEnvelope(now, 96, 1),
	} {
		t.Run(name, func(t *testing.T) {
			var cache openAICodexTurnStateCandidates
			cache.Observe("member-a", "gpt-test", invalid, []int{len(invalid)}, now)
			_, ok := cache.Candidate("member-a", "gpt-test", now)
			require.False(t, ok, "invalid state must not acquire a fresh observation-based TTL")
			snapshot := cache.Snapshot("member-a", now)[0]
			require.Equal(t, uint64(1), snapshot.ObservedCount)
			require.Nil(t, snapshot.Candidate)
			cache.Observe("member-a", "gpt-test", valid, []int{332}, now)
			cache.Observe("member-a", "gpt-test", invalid, []int{len(invalid)}, now)
			got, ok := cache.Candidate("member-a", "gpt-test", now)
			require.True(t, ok)
			require.Equal(t, valid, got, "invalid observations must not displace a valid candidate")
		})
	}
	// Both canonical forms are acceptable; whitespace and non-zero pad bits are not.
	_, _, ok := parseOpenAICodexTurnStateCandidate(unpadded, now)
	require.True(t, ok)
}

func TestCodexTurnStateCandidatesSnapshotRedactsAndDetachesMetadata(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	value := testCodexTurnStateEnvelope(now, 12, 1)
	cache.Observe("secret-scope", "gpt-b", value, []int{332}, now)
	cache.Observe("secret-scope", "gpt-a", value, []int{332}, now)
	_, ok := cache.Candidate("secret-scope", "gpt-a", now)
	require.True(t, ok)
	snapshots := cache.Snapshot("secret-scope", now)
	require.Equal(t, "gpt-a", snapshots[0].Model)
	require.Equal(t, "gpt-b", snapshots[1].Model)
	require.Len(t, snapshots[0].Candidate.HashPrefix, openAICodexTurnStateCandidateHashPrefixLength)
	raw, err := json.Marshal(snapshots)
	require.NoError(t, err)
	require.NotContains(t, string(raw), value)
	require.NotContains(t, string(raw), "secret-scope")
	*snapshots[0].Candidate.LastReusedAt = now.Add(100 * time.Hour)
	snapshots[0].Candidate.Length = 999
	snapshots[0].Lengths[0].Count = 999
	again := cache.Snapshot("secret-scope", now)[0]
	require.Equal(t, now, *again.Candidate.LastReusedAt)
	require.Equal(t, 332, again.Candidate.Length)
	require.Equal(t, uint64(1), again.Lengths[0].Count)
}

func TestCodexTurnStateCandidatesBoundBucketsHistogramAndModelKeys(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	value := testCodexTurnStateEnvelope(now, 12, 1)
	for _, invalid := range []struct{ scope, model string }{
		{"", "gpt-test"},
		{"member-a", ""},
		{"member-a", " \t"},
		{"member-a", strings.Repeat("m", openAICodexTurnStateCandidateMaxModelBytes+1)},
		{strings.Repeat("s", openAICodexTurnStateCandidateMaxScopeBytes+1), "gpt-test"},
	} {
		cache.Observe(invalid.scope, invalid.model, value, []int{332}, now)
	}
	require.Empty(t, cache.buckets)
	for i := 0; i < openAICodexTurnStateCandidateMaxBuckets+1; i++ {
		cache.Observe("member-a", fmt.Sprintf("gpt-%04d", i), value, []int{332}, now)
	}
	require.Len(t, cache.buckets, openAICodexTurnStateCandidateMaxBuckets)
	require.Equal(t, openAICodexTurnStateCandidateMaxBuckets, cache.order.Len())
	_, ok := cache.Candidate("member-a", "gpt-0000", now)
	require.False(t, ok)
	_, ok = cache.Candidate("member-a", "gpt-1024", now)
	require.True(t, ok)

	cache.Clear("member-a")
	for length := 1; length <= openAICodexTurnStateCandidateMaxLengths+4; length++ {
		cache.Observe("member-a", "gpt-test", strings.Repeat("!", length), nil, now)
	}
	cache.Observe("member-a", "gpt-test", "!", nil, now)
	snapshot := cache.Snapshot("member-a", now)[0]
	require.Len(t, snapshot.Lengths, openAICodexTurnStateCandidateMaxLengths)
	require.Equal(t, uint64(4), snapshot.OtherLengthCount)
	require.Equal(t, uint64(21), snapshot.ObservedCount)
	require.Equal(t, uint64(2), snapshot.Lengths[0].Count)
}

func TestCodexTurnStateCandidatesInvalidationKeepsNewerConcurrentValue(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	oldValue := testCodexTurnStateEnvelope(now.Add(-time.Minute), 12, 1)
	newValue := testCodexTurnStateEnvelope(now, 12, 2)
	cache.Observe("member-a", "gpt-test", oldValue, []int{332}, now)
	cache.Observe("member-a", "gpt-test", newValue, []int{332}, now)
	cache.Invalidate("member-a", "gpt-test", oldValue)
	value, ok := cache.Candidate("member-a", "gpt-test", now)
	require.True(t, ok)
	require.Equal(t, newValue, value)
	cache.Invalidate("member-b", "gpt-test", newValue)
	cache.Invalidate("member-a", "gpt-other", newValue)
	_, ok = cache.Candidate("member-a", "gpt-test", now)
	require.True(t, ok)
	cache.Invalidate("member-a", "gpt-test", newValue)
	_, ok = cache.Candidate("member-a", "gpt-test", now)
	require.False(t, ok)
	require.Equal(t, uint64(2), cache.Snapshot("member-a", now)[0].ObservedCount)
}

func TestCodexTurnStateCandidatesConcurrentObservationAndAdministration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	value := testCodexTurnStateEnvelope(now, 12, 1)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			model := fmt.Sprintf("gpt-%d", worker)
			for iteration := 0; iteration < 40; iteration++ {
				cache.Observe("member-a", model, value, []int{332}, now)
				cache.Candidate("member-a", model, now)
				cache.Snapshot("member-a", now)
				cache.Invalidate("member-a", model, value)
				if iteration%10 == 0 {
					cache.Clear("member-a")
				}
			}
		}(worker)
	}
	workers.Wait()
	require.LessOrEqual(t, len(cache.buckets), 8)
	require.Equal(t, len(cache.buckets), cache.order.Len())
}
