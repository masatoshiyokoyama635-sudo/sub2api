package service

import (
	"container/list"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	openAICodexTurnStateCandidateMaxBuckets       = 1024
	openAICodexTurnStateCandidateMaxLengths       = 16
	openAICodexTurnStateCandidateMaxModelBytes    = 128
	openAICodexTurnStateCandidateMaxScopeBytes    = 1024
	openAICodexTurnStateCandidateMaxValueBytes    = 2048
	openAICodexTurnStateCandidateLocalLifetime    = time.Hour
	openAICodexTurnStateCandidateHashPrefixLength = 12
)

// CodexTurnStateLengthCount describes observed lengths, not model quality.
type CodexTurnStateLengthCount struct {
	Length int    `json:"length"`
	Count  uint64 `json:"count"`
}

// CodexTurnStateCandidateSnapshot deliberately contains no raw state. IssuedAt
// comes from an unverified Fernet envelope; ExpiresAt is our local retention
// limit, not an assertion about the upstream server's acceptance window.
type CodexTurnStateCandidateSnapshot struct {
	HashPrefix     string     `json:"hash_prefix"`
	Length         int        `json:"length"`
	IssuedAt       time.Time  `json:"issued_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	LastObservedAt time.Time  `json:"last_observed_at"`
	ObservedCount  uint64     `json:"observed_count"`
	ReuseCount     uint64     `json:"reuse_count"`
	LastReusedAt   *time.Time `json:"last_reused_at,omitempty"`
}

// CodexTurnStateDiagnostic contains a fixed reason code, never upstream text.
type CodexTurnStateDiagnostic struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

type CodexTurnStateModelSnapshot struct {
	Model                     string                           `json:"model"`
	ObservedCount             uint64                           `json:"observed_count"`
	LastObservedAt            time.Time                        `json:"last_observed_at"`
	Lengths                   []CodexTurnStateLengthCount      `json:"lengths"`
	OtherLengthCount          uint64                           `json:"other_length_count"`
	Candidate                 *CodexTurnStateCandidateSnapshot `json:"candidate,omitempty"`
	ReuseAttemptCount         uint64                           `json:"reuse_attempt_count"`
	LastReuseAttemptAt        *time.Time                       `json:"last_reuse_attempt_at,omitempty"`
	LastSelection             *CodexTurnStateDiagnostic        `json:"last_selection,omitempty"`
	LastCandidateObservation  *CodexTurnStateDiagnostic        `json:"last_candidate_observation,omitempty"`
	LastCandidateRejection    *CodexTurnStateDiagnostic        `json:"last_candidate_rejection,omitempty"`
	LastCandidateInvalidation *CodexTurnStateDiagnostic        `json:"last_candidate_invalidation,omitempty"`
	LastRequest               *CodexTurnStateRequestSnapshot   `json:"last_request,omitempty"`
}

type openAICodexTurnStateCandidateKey struct {
	scope string
	model string
}

type openAICodexTurnStateCandidate struct {
	value    string
	metadata CodexTurnStateCandidateSnapshot
}

type openAICodexTurnStateCandidateBucket struct {
	key                       openAICodexTurnStateCandidateKey
	observedCount             uint64
	lastObservedAt            time.Time
	lengths                   map[int]uint64
	otherLengthCount          uint64
	candidate                 *openAICodexTurnStateCandidate
	reuseAttemptCount         uint64
	lastReuseAttemptAt        *time.Time
	lastSelection             *CodexTurnStateDiagnostic
	lastCandidateObservation  *CodexTurnStateDiagnostic
	lastCandidateRejection    *CodexTurnStateDiagnostic
	lastCandidateInvalidation *CodexTurnStateDiagnostic
	lastRequest               *CodexTurnStateRequestSnapshot
}

// openAICodexTurnStateCandidates is a zero-value-ready, process-local store.
// Callers supply a credential-scoped key and the actual upstream model. Each
// bucket retains at most one candidate; eviction follows observation recency.
// An allow-listed length and a valid envelope are only selection criteria, not
// proof of signature validity or model quality.
type openAICodexTurnStateCandidates struct {
	mu      sync.Mutex
	buckets map[openAICodexTurnStateCandidateKey]*list.Element
	order   list.List
}

func validOpenAICodexTurnStateCandidateScope(scope string) bool {
	return strings.TrimSpace(scope) != "" && len(scope) <= openAICodexTurnStateCandidateMaxScopeBytes
}

func validOpenAICodexTurnStateCandidateKey(scope, model string) bool {
	return validOpenAICodexTurnStateCandidateScope(scope) &&
		strings.TrimSpace(model) != "" && len(model) <= openAICodexTurnStateCandidateMaxModelBytes
}

// parseOpenAICodexTurnStateCandidate checks only the public Fernet structure:
// version, timestamp, IV, block-aligned ciphertext, and HMAC length. Without the
// upstream key it cannot authenticate or decrypt a token. Invalid timestamps
// never fall back to the observation time.
func parseOpenAICodexTurnStateCandidate(value string, now time.Time) (time.Time, time.Time, bool) {
	issuedAt, expiresAt, reason := inspectOpenAICodexTurnStateCandidate(value, now)
	return issuedAt, expiresAt, reason == ""
}

func inspectOpenAICodexTurnStateCandidate(value string, now time.Time) (time.Time, time.Time, string) {
	if len(value) == 0 || len(value) > openAICodexTurnStateCandidateMaxValueBytes || now.Unix() < 0 {
		return time.Time{}, time.Time{}, "invalid_format"
	}
	encoding := base64.RawURLEncoding.Strict()
	if strings.HasSuffix(value, "=") {
		encoding = base64.URLEncoding.Strict()
	}
	raw, err := encoding.DecodeString(value)
	if err != nil || encoding.EncodeToString(raw) != value {
		return time.Time{}, time.Time{}, "invalid_format"
	}
	const envelopeBytes = 1 + 8 + 16 + 32
	if len(raw) < envelopeBytes+16 || raw[0] != 0x80 || (len(raw)-envelopeBytes)%16 != 0 {
		return time.Time{}, time.Time{}, "invalid_format"
	}
	issuedSeconds := binary.BigEndian.Uint64(raw[1:9])
	if issuedSeconds > uint64(now.Unix()) {
		return time.Time{}, time.Time{}, "future_timestamp"
	}
	issuedAt := time.Unix(int64(issuedSeconds), 0).UTC()
	expiresAt := issuedAt.Add(openAICodexTurnStateCandidateLocalLifetime)
	if !now.Before(expiresAt) {
		return time.Time{}, time.Time{}, "expired"
	}
	return issuedAt, expiresAt, ""
}

func (bucket *openAICodexTurnStateCandidateBucket) expire(now time.Time) {
	if bucket.candidate != nil && !now.Before(bucket.candidate.metadata.ExpiresAt) {
		bucket.candidate = nil
		bucket.lastCandidateInvalidation = codexTurnStateDiagnostic("expired", now)
	}
}

func codexTurnStateDiagnostic(reason string, now time.Time) *CodexTurnStateDiagnostic {
	return &CodexTurnStateDiagnostic{Reason: reason, At: now.UTC()}
}

func cloneCodexTurnStateDiagnostic(value *CodexTurnStateDiagnostic) *CodexTurnStateDiagnostic {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func (bucket *openAICodexTurnStateCandidateBucket) observation(reason string, now time.Time) {
	bucket.lastCandidateObservation = codexTurnStateDiagnostic(reason, now)
	switch reason {
	case "length_not_allowed", "invalid_format", "future_timestamp", "expired", "stale", "state_echo":
		bucket.lastCandidateRejection = codexTurnStateDiagnostic(reason, now)
	}
}

func validCodexTurnStateSelectionReason(reason string) bool {
	switch reason {
	case "reused", "no_candidate", "expired", "length_not_allowed", "observe_mode", "client_state", "client_continuation", "client_metadata_state", "unsupported_path":
		return true
	default:
		return false
	}
}

// bucketLocked drops malformed nodes instead of exposing an unverified owner.
// Callers must hold cache.mu.
func (cache *openAICodexTurnStateCandidates) bucketLocked(key openAICodexTurnStateCandidateKey) *openAICodexTurnStateCandidateBucket {
	element, exists := cache.buckets[key]
	if !exists {
		return nil
	}
	if element != nil {
		bucket, ok := element.Value.(*openAICodexTurnStateCandidateBucket)
		if ok && bucket != nil && bucket.key == key && bucket.lengths != nil {
			return bucket
		}
		cache.order.Remove(element)
	}
	delete(cache.buckets, key)
	return nil
}

// ensureBucketLocked also bounds diagnostics-only buckets; callers hold mu.
func (cache *openAICodexTurnStateCandidates) ensureBucketLocked(key openAICodexTurnStateCandidateKey) *openAICodexTurnStateCandidateBucket {
	if cache.buckets == nil {
		cache.buckets = make(map[openAICodexTurnStateCandidateKey]*list.Element)
	}
	bucket := cache.bucketLocked(key)
	if bucket == nil {
		if len(cache.buckets) >= openAICodexTurnStateCandidateMaxBuckets {
			oldest := cache.order.Front()
			if oldest != nil {
				oldestBucket, ok := oldest.Value.(*openAICodexTurnStateCandidateBucket)
				if ok && oldestBucket != nil && cache.buckets[oldestBucket.key] == oldest {
					delete(cache.buckets, oldestBucket.key)
					cache.order.Remove(oldest)
				} else {
					cache.buckets = make(map[openAICodexTurnStateCandidateKey]*list.Element)
					cache.order.Init()
				}
			} else {
				cache.buckets = make(map[openAICodexTurnStateCandidateKey]*list.Element)
				cache.order.Init()
			}
		}
		bucket = &openAICodexTurnStateCandidateBucket{
			key:     key,
			lengths: make(map[int]uint64),
		}
		cache.buckets[key] = cache.order.PushBack(bucket)
	}
	return bucket
}

// Observe counts every supplied header length, including malformed or
// ineligible states. Empty lengths disables candidate selection. The account
// getter, rather than this store, supplies any default length policy.
func (cache *openAICodexTurnStateCandidates) Observe(scope, model, value string, lengths []int, now time.Time) {
	cache.observe(scope, model, value, lengths, now, false)
}

// ObserveEcho records a replayed response without refreshing or replacing the
// candidate. It preserves the same bounded length histogram as normal Observe.
func (cache *openAICodexTurnStateCandidates) ObserveEcho(scope, model, value string, now time.Time) {
	cache.observe(scope, model, value, nil, now, true)
}

func (cache *openAICodexTurnStateCandidates) observe(scope, model, value string, lengths []int, now time.Time, echo bool) {
	if cache == nil || !validOpenAICodexTurnStateCandidateKey(scope, model) {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	key := openAICodexTurnStateCandidateKey{scope: scope, model: model}
	bucket := cache.ensureBucketLocked(key)
	cache.order.MoveToBack(cache.buckets[key])
	bucket.expire(now)
	bucket.observedCount++
	bucket.lastObservedAt = now.UTC()
	length := len(value)
	if _, exists := bucket.lengths[length]; exists || len(bucket.lengths) < openAICodexTurnStateCandidateMaxLengths {
		bucket.lengths[length]++
	} else {
		bucket.otherLengthCount++
	}
	if echo {
		bucket.observation("state_echo", now)
		return
	}
	eligible := false
	for _, allowed := range lengths {
		if allowed == length {
			eligible = true
			break
		}
	}
	if !eligible {
		bucket.observation("length_not_allowed", now)
		return
	}
	issuedAt, expiresAt, rejection := inspectOpenAICodexTurnStateCandidate(value, now)
	if rejection != "" {
		bucket.observation(rejection, now)
		return
	}
	result := "accepted"
	if previous := bucket.candidate; previous != nil {
		if previous.value == value {
			previous.metadata.LastObservedAt = now.UTC()
			previous.metadata.ObservedCount++
			bucket.observation("unchanged", now)
			return
		}
		if issuedAt.Before(previous.metadata.IssuedAt) {
			bucket.observation("stale", now)
			return
		}
		result = "refreshed"
	}
	hash := sha256.Sum256([]byte(value))
	bucket.candidate = &openAICodexTurnStateCandidate{
		value: value,
		metadata: CodexTurnStateCandidateSnapshot{
			HashPrefix:     hex.EncodeToString(hash[:])[:openAICodexTurnStateCandidateHashPrefixLength],
			Length:         length,
			IssuedAt:       issuedAt,
			ExpiresAt:      expiresAt,
			LastObservedAt: now.UTC(),
			ObservedCount:  1,
		},
	}
	bucket.observation(result, now)
}

// Candidate returns only the exact scope/model match while it remains within
// its original local lifetime. Successful retrievals count as reuse attempts;
// neither retrievals nor repeat observations extend that lifetime.
func (cache *openAICodexTurnStateCandidates) Candidate(scope, model string, now time.Time) (string, bool) {
	value, ok, _ := cache.selectCandidate(scope, model, nil, now, false)
	return value, ok
}

// SelectCandidate checks the current length policy and records the selection
// under one lock. A rejected length never increments either reuse counter.
func (cache *openAICodexTurnStateCandidates) SelectCandidate(scope, model string, lengths []int, now time.Time) (string, bool) {
	value, ok, _ := cache.SelectCandidateWithReason(scope, model, lengths, now)
	return value, ok
}

// SelectCandidateWithReason returns the decision for this lookup atomically;
// another request's later selection cannot change its diagnostic attribution.
func (cache *openAICodexTurnStateCandidates) SelectCandidateWithReason(scope, model string, lengths []int, now time.Time) (string, bool, string) {
	return cache.selectCandidate(scope, model, lengths, now, true)
}

func (cache *openAICodexTurnStateCandidates) selectCandidate(scope, model string, lengths []int, now time.Time, checkLengths bool) (string, bool, string) {
	if cache == nil || !validOpenAICodexTurnStateCandidateKey(scope, model) {
		return "", false, "no_candidate"
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	bucket := cache.ensureBucketLocked(openAICodexTurnStateCandidateKey{scope: scope, model: model})
	bucket.expire(now)
	if bucket.candidate == nil || now.Before(bucket.candidate.metadata.IssuedAt) {
		reason := "no_candidate"
		if bucket.lastCandidateInvalidation != nil && bucket.lastCandidateInvalidation.Reason == "expired" {
			reason = "expired"
		}
		bucket.lastSelection = codexTurnStateDiagnostic(reason, now)
		return "", false, reason
	}
	if checkLengths {
		allowed := false
		for _, length := range lengths {
			if length == bucket.candidate.metadata.Length {
				allowed = true
				break
			}
		}
		if !allowed {
			bucket.lastSelection = codexTurnStateDiagnostic("length_not_allowed", now)
			return "", false, "length_not_allowed"
		}
	}
	bucket.candidate.metadata.ReuseCount++
	bucket.reuseAttemptCount++
	reusedAt := now.UTC()
	bucket.candidate.metadata.LastReusedAt = &reusedAt
	bucket.lastReuseAttemptAt = &reusedAt
	bucket.lastSelection = codexTurnStateDiagnostic("reused", now)
	return bucket.candidate.value, true, "reused"
}

// RecordSelection accepts only fixed reason codes supplied by the HTTP layer.
// Only SelectCandidate/Candidate count actual candidate reuse attempts.
func (cache *openAICodexTurnStateCandidates) RecordSelection(scope, model, reason string, now time.Time) {
	if cache == nil || !validOpenAICodexTurnStateCandidateKey(scope, model) || !validCodexTurnStateSelectionReason(reason) {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	bucket := cache.ensureBucketLocked(openAICodexTurnStateCandidateKey{scope: scope, model: model})
	bucket.expire(now)
	bucket.lastSelection = codexTurnStateDiagnostic(reason, now)
}

// RecordRequest retains at most one detached, bounded request summary per
// bucket. The caller supplies metadata only, never state values or credentials.
func (cache *openAICodexTurnStateCandidates) RecordRequest(scope, model string, snapshot CodexTurnStateRequestSnapshot) {
	if cache == nil || !validOpenAICodexTurnStateCandidateKey(scope, model) {
		return
	}
	snapshot.At = snapshot.At.UTC()
	if !validCodexTurnStateSelectionReason(snapshot.SelectionReason) {
		snapshot.SelectionReason = ""
	}
	switch snapshot.StateSource {
	case "none", "client", "candidate":
	default:
		snapshot.StateSource = "none"
	}
	snapshot.RequestID = codexTurnStateBoundedLabel(snapshot.RequestID, 128)
	snapshot.UpstreamResponseModel = codexTurnStateBoundedLabel(snapshot.UpstreamResponseModel, openAICodexTurnStateCandidateMaxModelBytes)
	if snapshot.UpstreamResponseModel == "" {
		snapshot.ResponseModelObserved = false
		snapshot.ModelMismatch = false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	bucket := cache.ensureBucketLocked(openAICodexTurnStateCandidateKey{scope: scope, model: model})
	bucket.lastRequest = &snapshot
}

func codexTurnStateBoundedLabel(value string, limit int) string {
	if len(value) > limit {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.:/", char) {
			continue
		}
		return ""
	}
	return value
}

// Snapshot provides detached, sorted metadata suitable for administrator APIs.
// Expired raw values are discarded. Scope keys and raw states never leave this
// method, including when a candidate fails validation.
func (cache *openAICodexTurnStateCandidates) Snapshot(scope string, now time.Time) []CodexTurnStateModelSnapshot {
	snapshots := make([]CodexTurnStateModelSnapshot, 0)
	if cache == nil || !validOpenAICodexTurnStateCandidateScope(scope) {
		return snapshots
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for key := range cache.buckets {
		if key.scope != scope {
			continue
		}
		bucket := cache.bucketLocked(key)
		if bucket == nil {
			continue
		}
		bucket.expire(now)
		snapshot := CodexTurnStateModelSnapshot{
			Model:                     key.model,
			ObservedCount:             bucket.observedCount,
			LastObservedAt:            bucket.lastObservedAt,
			Lengths:                   make([]CodexTurnStateLengthCount, 0, len(bucket.lengths)),
			OtherLengthCount:          bucket.otherLengthCount,
			ReuseAttemptCount:         bucket.reuseAttemptCount,
			LastSelection:             cloneCodexTurnStateDiagnostic(bucket.lastSelection),
			LastCandidateObservation:  cloneCodexTurnStateDiagnostic(bucket.lastCandidateObservation),
			LastCandidateRejection:    cloneCodexTurnStateDiagnostic(bucket.lastCandidateRejection),
			LastCandidateInvalidation: cloneCodexTurnStateDiagnostic(bucket.lastCandidateInvalidation),
		}
		if bucket.lastReuseAttemptAt != nil {
			at := *bucket.lastReuseAttemptAt
			snapshot.LastReuseAttemptAt = &at
		}
		if bucket.lastRequest != nil {
			lastRequest := *bucket.lastRequest
			snapshot.LastRequest = &lastRequest
		}
		for length, count := range bucket.lengths {
			snapshot.Lengths = append(snapshot.Lengths, CodexTurnStateLengthCount{Length: length, Count: count})
		}
		sort.Slice(snapshot.Lengths, func(i, j int) bool { return snapshot.Lengths[i].Length < snapshot.Lengths[j].Length })
		if candidate := bucket.candidate; candidate != nil && !now.Before(candidate.metadata.IssuedAt) {
			metadata := candidate.metadata
			if metadata.LastReusedAt != nil {
				reusedAt := *metadata.LastReusedAt
				metadata.LastReusedAt = &reusedAt
			}
			snapshot.Candidate = &metadata
		}
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Model < snapshots[j].Model })
	return snapshots
}

func (cache *openAICodexTurnStateCandidates) Clear(scope string) {
	if cache == nil || !validOpenAICodexTurnStateCandidateScope(scope) {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for key, element := range cache.buckets {
		if key.scope == scope {
			delete(cache.buckets, key)
			if element != nil {
				cache.order.Remove(element)
			}
		}
	}
}

// Invalidate drops only the candidate that was actually rejected by upstream.
// A concurrent response may already have supplied a newer candidate, which
// must survive invalidation of the earlier request's value.
func (cache *openAICodexTurnStateCandidates) Invalidate(scope, model, value string) {
	if cache == nil || value == "" || !validOpenAICodexTurnStateCandidateKey(scope, model) {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	bucket := cache.bucketLocked(openAICodexTurnStateCandidateKey{scope: scope, model: model})
	if bucket == nil {
		return
	}
	if bucket.candidate != nil && bucket.candidate.value == value {
		bucket.candidate = nil
		bucket.lastCandidateInvalidation = codexTurnStateDiagnostic("upstream_rejected", time.Now())
	}
}
