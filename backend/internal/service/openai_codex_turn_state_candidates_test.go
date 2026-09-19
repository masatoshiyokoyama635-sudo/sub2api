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

func TestCodexTurnStateCandidatesDiagnosticsPreserveBucketAttemptsAcrossReplacement(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	var cache openAICodexTurnStateCandidates
	first := testCodexTurnStateEnvelope(now.Add(-time.Minute), 12, 1)
	second := testCodexTurnStateEnvelope(now, 12, 2)
	cache.Observe("member-a", "gpt-test", first, []int{332}, now)
	_, ok := cache.SelectCandidate("member-a", "gpt-test", []int{332}, now)
	require.True(t, ok)
	cache.Observe("member-a", "gpt-test", second, []int{332}, now.Add(time.Second))
	snapshot := cache.Snapshot("member-a", now.Add(time.Second))[0]
	require.Equal(t, uint64(1), snapshot.ReuseAttemptCount)
	require.Equal(t, uint64(0), snapshot.Candidate.ReuseCount)
	require.Equal(t, now, *snapshot.LastReuseAttemptAt)
	require.Equal(t, "refreshed", snapshot.LastCandidateObservation.Reason)
	_, ok = cache.SelectCandidate("member-a", "gpt-test", []int{332}, now.Add(2*time.Second))
	require.True(t, ok)
	cache.Invalidate("member-a", "gpt-test", second)
	snapshot = cache.Snapshot("member-a", now.Add(3*time.Second))[0]
	require.Nil(t, snapshot.Candidate)
	require.Equal(t, uint64(2), snapshot.ReuseAttemptCount)
	require.Equal(t, now.Add(2*time.Second), *snapshot.LastReuseAttemptAt)
	require.Equal(t, "upstream_rejected", snapshot.LastCandidateInvalidation.Reason)
	cache.Observe("member-a", "gpt-test", first, []int{332}, now.Add(3*time.Second))
	snapshot = cache.Snapshot("member-a", now.Add(time.Hour))[0]
	require.Nil(t, snapshot.Candidate)
	require.Equal(t, uint64(2), snapshot.ReuseAttemptCount)
	require.Equal(t, "expired", snapshot.LastCandidateInvalidation.Reason)
	cache.Clear("member-a")
	require.Empty(t, cache.Snapshot("member-a", now))
}

func TestCodexTurnStateCandidatesDiagnosticsSelectionIncludesMissesAndLengthPolicy(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	_, ok := cache.SelectCandidate("member-a", "gpt-test", []int{332}, now)
	require.False(t, ok)
	snapshot := cache.Snapshot("member-a", now)[0]
	require.Equal(t, "no_candidate", snapshot.LastSelection.Reason)
	require.Equal(t, uint64(0), snapshot.ObservedCount)
	value := testCodexTurnStateEnvelope(now, 12, 1)
	cache.Observe("member-a", "gpt-test", value, []int{332}, now)
	for _, lengths := range [][]int{{292}, nil, {}} {
		_, ok = cache.SelectCandidate("member-a", "gpt-test", lengths, now)
		require.False(t, ok)
		snapshot = cache.Snapshot("member-a", now)[0]
		require.Equal(t, "length_not_allowed", snapshot.LastSelection.Reason)
		require.Zero(t, snapshot.ReuseAttemptCount)
		require.Zero(t, snapshot.Candidate.ReuseCount)
	}
	_, ok = cache.SelectCandidate("member-a", "gpt-test", []int{332}, now)
	require.True(t, ok)
	snapshot = cache.Snapshot("member-a", now)[0]
	require.Equal(t, "reused", snapshot.LastSelection.Reason)
	require.Equal(t, uint64(1), snapshot.ReuseAttemptCount)
	// A status poll may expire the candidate before the next selection.
	cache.Snapshot("member-a", now.Add(time.Hour))
	_, ok = cache.SelectCandidate("member-a", "gpt-test", []int{332}, now.Add(time.Hour))
	require.False(t, ok)
	snapshot = cache.Snapshot("member-a", now.Add(time.Hour))[0]
	require.Equal(t, "expired", snapshot.LastSelection.Reason)
	require.Equal(t, "expired", snapshot.LastCandidateInvalidation.Reason)
	require.Equal(t, uint64(1), snapshot.ReuseAttemptCount)
}

func TestCodexTurnStateCandidatesDiagnosticsExplainObservationWithoutDisplacingCandidate(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	valid := testCodexTurnStateEnvelope(now.Add(-time.Minute), 12, 1)
	for _, tc := range []struct {
		name, value, reason string
		lengths             []int
	}{
		{name: "policy", value: valid, lengths: []int{292}, reason: "length_not_allowed"},
		{name: "malformed", value: "!" + valid[1:], lengths: []int{332}, reason: "invalid_format"},
		{name: "future", value: testCodexTurnStateEnvelope(now.Add(time.Second), 12, 1), lengths: []int{332}, reason: "future_timestamp"},
		{name: "expired", value: testCodexTurnStateEnvelope(now.Add(-time.Hour), 12, 1), lengths: []int{332}, reason: "expired"},
		{name: "stale", value: testCodexTurnStateEnvelope(now.Add(-2*time.Minute), 12, 1), lengths: []int{332}, reason: "stale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache openAICodexTurnStateCandidates
			cache.Observe("member-a", "gpt-test", valid, []int{332}, now)
			require.Equal(t, "accepted", cache.Snapshot("member-a", now)[0].LastCandidateObservation.Reason)
			cache.Observe("member-a", "gpt-test", tc.value, tc.lengths, now)
			snapshot := cache.Snapshot("member-a", now)[0]
			require.Equal(t, tc.reason, snapshot.LastCandidateObservation.Reason)
			require.Equal(t, tc.reason, snapshot.LastCandidateRejection.Reason)
			require.Equal(t, now, snapshot.LastCandidateRejection.At)
			got, ok := cache.SelectCandidate("member-a", "gpt-test", []int{332}, now)
			require.True(t, ok)
			require.Equal(t, valid, got)
			cache.Observe("member-a", "gpt-test", valid, []int{332}, now)
			snapshot = cache.Snapshot("member-a", now)[0]
			require.Equal(t, "unchanged", snapshot.LastCandidateObservation.Reason)
			require.Equal(t, tc.reason, snapshot.LastCandidateRejection.Reason, "retain the last rejection as history")
		})
	}
}

func TestCodexTurnStateCandidatesDiagnosticsEchoDoesNotRenewOrSeed(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	value := testCodexTurnStateEnvelope(now, 12, 1)
	cache.Observe("member-a", "gpt-test", value, []int{332}, now)
	cache.ObserveEcho("member-a", "gpt-test", value, now.Add(time.Minute))
	snapshot := cache.Snapshot("member-a", now.Add(time.Minute))[0]
	require.Equal(t, uint64(2), snapshot.ObservedCount)
	require.Equal(t, "state_echo", snapshot.LastCandidateObservation.Reason)
	require.Equal(t, "state_echo", snapshot.LastCandidateRejection.Reason)
	require.Equal(t, uint64(1), snapshot.Candidate.ObservedCount)
	require.Equal(t, now, snapshot.Candidate.LastObservedAt)
	require.Equal(t, now.Add(time.Hour), snapshot.Candidate.ExpiresAt)
	cache.ObserveEcho("member-a", "gpt-empty", value, now)
	require.Nil(t, cache.Snapshot("member-a", now)[0].Candidate)
	cache.ObserveEcho("member-a", "gpt-test", value, now.Add(time.Hour))
	require.Nil(t, cache.Snapshot("member-a", now.Add(time.Hour))[1].Candidate)
}

func TestCodexTurnStateCandidatesDiagnosticsBoundRedactAndDetachRequestMetadata(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	value := testCodexTurnStateEnvelope(now, 12, 1)
	cache.RecordSelection("secret-scope", "gpt-test", value, now)
	require.Empty(t, cache.buckets, "arbitrary reason strings are discarded before allocation")
	cache.Observe("secret-scope", "gpt-test", value, []int{332}, now)
	cache.RecordSelection("secret-scope", "gpt-test", "client_continuation", now)
	cache.RecordRequest("secret-scope", "gpt-test", CodexTurnStateRequestSnapshot{
		At: now, RequestID: "request-1", StateSource: "candidate", OutboundStateLength: 332,
		SelectionReason: "reused", UpstreamResponseModel: "gpt-luna", ResponseModelObserved: true, ModelMismatch: true,
	})
	snapshot := cache.Snapshot("secret-scope", now)[0]
	require.Equal(t, "client_continuation", snapshot.LastSelection.Reason)
	require.Zero(t, snapshot.ReuseAttemptCount, "diagnostic writes do not increment attempts")
	snapshot.LastSelection.Reason = "mutated"
	snapshot.LastCandidateObservation.At = now.Add(time.Hour)
	snapshot.LastRequest.RequestID = "mutated"
	again := cache.Snapshot("secret-scope", now)[0]
	require.Equal(t, "client_continuation", again.LastSelection.Reason)
	require.Equal(t, now, again.LastCandidateObservation.At)
	require.Equal(t, "request-1", again.LastRequest.RequestID)
	cache.RecordRequest("secret-scope", "gpt-test", CodexTurnStateRequestSnapshot{
		At: now, RequestID: value, SelectionReason: value, StateSource: value,
		UpstreamResponseModel: "private@example.invalid\nBearer secret", ResponseModelObserved: true, ModelMismatch: true,
	})
	again = cache.Snapshot("secret-scope", now)[0]
	require.Empty(t, again.LastRequest.RequestID)
	require.Empty(t, again.LastRequest.SelectionReason)
	require.Equal(t, "none", again.LastRequest.StateSource)
	require.Empty(t, again.LastRequest.UpstreamResponseModel)
	require.False(t, again.LastRequest.ResponseModelObserved)
	require.False(t, again.LastRequest.ModelMismatch)
	raw, err := json.Marshal(again)
	require.NoError(t, err)
	for _, sensitive := range []string{value, "secret-scope", "private@example.invalid", "Bearer"} {
		require.NotContains(t, string(raw), sensitive)
	}
	for i := 0; i < openAICodexTurnStateCandidateMaxBuckets+1; i++ {
		model := fmt.Sprintf("gpt-%04d", i)
		cache.RecordSelection("member-a", model, "observe_mode", now)
		cache.RecordRequest("member-a", model, CodexTurnStateRequestSnapshot{At: now, StateSource: "none"})
	}
	require.Len(t, cache.buckets, openAICodexTurnStateCandidateMaxBuckets)
	require.Equal(t, openAICodexTurnStateCandidateMaxBuckets, cache.order.Len())
}

func TestCodexTurnStateCandidatesCollectionTracksRealStartsAndPreservesResults(t *testing.T) {
	now := time.Date(2026, 9, 19, 15, 0, 0, 0, time.UTC)
	var cache openAICodexTurnStateCandidates
	cache.RecordCollectionStart("scope-a", "model-a", now)
	snapshot := cache.Snapshot("scope-a", now)[0]
	require.Zero(t, snapshot.ObservedCount)
	require.Nil(t, snapshot.Candidate)
	require.NotNil(t, snapshot.Collection)
	require.Equal(t, uint64(1), snapshot.Collection.AttemptCount)
	require.True(t, snapshot.Collection.InFlight)
	require.Equal(t, now, snapshot.Collection.LastAttemptAt)
	require.Equal(t, "in_progress", snapshot.Collection.LastReason)
	finished := now.Add(time.Second)
	next := now.Add(5 * time.Minute)
	cache.RecordCollectionResult("scope-a", "model-a", CodexTurnStateCollectionSnapshot{
		AttemptCount: 999, LastAttemptAt: now.Add(time.Hour), InFlight: true,
		LastFinishedAt: &finished, NextEligibleAt: &next, LastReason: "accepted", LastHTTPStatus: 200,
		LastObservedLength: 332, LastResponseModel: "gpt-6-astra",
	})
	finished = finished.Add(time.Hour)
	next = next.Add(time.Hour)
	snapshot = cache.Snapshot("scope-a", now)[0]
	require.Equal(t, uint64(1), snapshot.Collection.AttemptCount, "result cannot invent network attempts")
	require.Equal(t, now, snapshot.Collection.LastAttemptAt)
	require.False(t, snapshot.Collection.InFlight)
	require.Equal(t, now.Add(time.Second), *snapshot.Collection.LastFinishedAt)
	require.Equal(t, now.Add(5*time.Minute), *snapshot.Collection.NextEligibleAt)
	require.Equal(t, "accepted", snapshot.Collection.LastReason)
	require.Equal(t, 200, snapshot.Collection.LastHTTPStatus)
	require.Equal(t, 332, snapshot.Collection.LastObservedLength)
	require.Equal(t, "gpt-6-astra", snapshot.Collection.LastResponseModel)
	// Neither status mutation nor a gateway's cooldown presentation may mutate
	// the shared cache or overwrite the last completed collection result.
	*snapshot.Collection.LastFinishedAt = now.Add(10 * time.Hour)
	*snapshot.Collection.NextEligibleAt = now.Add(11 * time.Hour)
	snapshot.Collection.AttemptCount = 777
	snapshot.Collection.LastReason = "mutated"
	again := cache.Snapshot("scope-a", now)[0].Collection
	require.Equal(t, uint64(1), again.AttemptCount)
	require.Equal(t, "accepted", again.LastReason)
	require.Equal(t, now.Add(time.Second), *again.LastFinishedAt)
	require.Equal(t, now.Add(5*time.Minute), *again.NextEligibleAt)
	cache.RecordCollectionStart("scope-a", "model-a", now.Add(6*time.Minute))
	again = cache.Snapshot("scope-a", now)[0].Collection
	require.Equal(t, uint64(2), again.AttemptCount)
	require.Equal(t, now.Add(6*time.Minute), again.LastAttemptAt)
	require.Equal(t, "in_progress", again.LastReason)
	require.True(t, again.InFlight)
	require.Equal(t, now.Add(time.Second), *again.LastFinishedAt)
	cache.Clear("scope-a")
	require.Empty(t, cache.Snapshot("scope-a", now))
}

func TestCodexTurnStateCandidatesCollectionBoundsAndRedactsMetadata(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	for _, reason := range []string{
		"accepted", "missing_state", "length_not_allowed", "invalid_format", "future_timestamp", "expired", "stale",
		"model_mismatch", "model_unobserved", "incomplete_response", "response_failed", "body_too_large", "transport_error",
		"timeout", "unauthorized", "forbidden", "rate_limited", "upstream_error", "configuration_changed", "collection_failed", "in_progress",
	} {
		cache.RecordCollectionResult("scope-a", "model-a", CodexTurnStateCollectionSnapshot{LastReason: reason})
		require.Equal(t, reason, cache.Snapshot("scope-a", now)[0].Collection.LastReason)
	}
	require.Zero(t, cache.Snapshot("scope-a", now)[0].Collection.AttemptCount, "result-only records never add attempts")
	state := testCodexTurnStateEnvelope(now, 12, 1)
	for _, unsafe := range []string{state, "private@example.invalid", "Bearer secret", "gpt-model\nprivate"} {
		cache.RecordCollectionResult("scope-a", "model-a", CodexTurnStateCollectionSnapshot{
			LastReason: unsafe, LastResponseModel: unsafe, LastHTTPStatus: 99999, LastObservedLength: -3,
		})
		snapshot := cache.Snapshot("scope-a", now)[0]
		require.Equal(t, "collection_failed", snapshot.Collection.LastReason)
		require.Empty(t, snapshot.Collection.LastResponseModel)
		require.Zero(t, snapshot.Collection.LastHTTPStatus)
		require.Zero(t, snapshot.Collection.LastObservedLength)
		raw, err := json.Marshal(snapshot)
		require.NoError(t, err)
		require.NotContains(t, string(raw), unsafe)
	}
	cache.RecordCollectionStart("scope-b", "model-a", now)
	require.Len(t, cache.Snapshot("scope-b", now), 1)
	cache.RecordCollectionStart("scope-a", "model-b", now)
	require.Len(t, cache.Snapshot("scope-a", now), 2)
	cache.RecordCollectionStart("scope-a", strings.Repeat("m", 129), now)
	require.Len(t, cache.Snapshot("scope-a", now), 2)
	for i := 0; i < openAICodexTurnStateCandidateMaxBuckets+1; i++ {
		model := fmt.Sprintf("model-%04d", i)
		cache.RecordCollectionStart("bounded", model, now)
		cache.RecordCollectionResult("bounded", model, CodexTurnStateCollectionSnapshot{LastReason: "missing_state"})
	}
	require.Len(t, cache.buckets, openAICodexTurnStateCandidateMaxBuckets)
	require.Equal(t, openAICodexTurnStateCandidateMaxBuckets, cache.order.Len())
}

func TestCodexTurnStateCandidatesCollectionConcurrentAccess(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var cache openAICodexTurnStateCandidates
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			model := fmt.Sprintf("model-%d", i)
			for attempt := 0; attempt < 30; attempt++ {
				cache.RecordCollectionStart("scope-a", model, now)
				cache.RecordCollectionResult("scope-a", model, CodexTurnStateCollectionSnapshot{
					LastFinishedAt: &now, LastReason: "accepted", LastHTTPStatus: 200,
				})
				cache.Snapshot("scope-a", now)
			}
		}(i)
	}
	workers.Wait()
	snapshots := cache.Snapshot("scope-a", now)
	require.Len(t, snapshots, 8)
	for _, snapshot := range snapshots {
		require.Equal(t, uint64(30), snapshot.Collection.AttemptCount)
		require.False(t, snapshot.Collection.InFlight)
	}
}
