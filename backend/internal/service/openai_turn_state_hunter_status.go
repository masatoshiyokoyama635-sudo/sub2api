package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

const codexHunterStatusMaxModels = openAITurnStateHunterMaxModels + openAITurnStateHuntLastKeep
const codexHunterStatusTimeout = 3 * time.Second

// codexHunterKnownModels keeps admin reads and explicit clearing bounded. Model
// names remain case-sensitive because they are part of the shared cache key.
func (s *OpenAIGatewayService) codexHunterKnownModels(a *Account) []string {
	out := make([]string, 0, codexHunterStatusMaxModels)
	if s == nil || a == nil {
		return out
	}
	seen := make(map[string]bool, codexHunterStatusMaxModels)
	add := func(model string) {
		model = codexTurnStateBoundedLabel(strings.TrimSpace(model), openAICodexTurnStateCandidateMaxModelBytes)
		if model == "" || seen[model] || len(out) >= codexHunterStatusMaxModels {
			return
		}
		seen[model] = true
		out = append(out, model)
	}
	cfg, _ := readOpenAITurnStateHunterConfig(a)
	for _, model := range cfg.Models {
		add(model)
	}
	state := readOpenAITurnStateHuntState(a)
	for i, attempt := range state.Last {
		if i >= openAITurnStateHuntLastKeep {
			break
		}
		add(attempt.Model)
	}
	now := time.Now()
	for _, entry := range s.openaiCodexTurnStateCandidates.Snapshot(codexHunterScope(a), now) {
		add(entry.Model)
	}
	// Traffic hints are already process-bounded. Sort this optional tail before
	// filling remaining slots so repeated status calls select the same models.
	prefix := openAITurnStateTrafficKey(a.ID, "")
	traffic := make([]string, 0)
	s.openaiTurnStateTraffic.Range(func(key, value any) bool {
		k, ok := key.(string)
		mark, valid := value.(openAITurnStateTrafficMark)
		if ok && valid && strings.HasPrefix(k, prefix) && mark.at.After(now.Add(-24*time.Hour)) {
			traffic = append(traffic, mark.model)
		}
		return true
	})
	sort.Strings(traffic)
	for _, model := range traffic {
		add(model)
	}
	sort.Strings(out)
	return out
}

// codexHunterModelStatus overlays current shared candidate metadata on local
// statistics. Reading a status must not create observations or reuse attempts.
func (s *OpenAIGatewayService) codexHunterModelStatus(ctx context.Context, a *Account) []CodexTurnStateModelSnapshot {
	out := make([]CodexTurnStateModelSnapshot, 0)
	if s == nil || a == nil {
		return out
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, codexHunterStatusTimeout)
	defer cancel()
	now := time.Now()
	scope := codexHunterScope(a)
	local := make(map[string]CodexTurnStateModelSnapshot)
	for _, entry := range s.openaiCodexTurnStateCandidates.Snapshot(scope, now) {
		local[entry.Model] = entry
	}
	models := s.codexHunterKnownModels(a)
	out = make([]CodexTurnStateModelSnapshot, 0, len(models))
	for _, model := range models {
		entry, ok := local[model]
		if !ok {
			entry = CodexTurnStateModelSnapshot{Model: model, Lengths: []CodexTurnStateLengthCount{}}
		}
		entry.Candidate = nil // shared state is authoritative, including on timeout
		out = append(out, entry)
	}
	if !s.codexHunterReusable(a) || !a.IsOpenAITurnStateHunterEnabled() || ctx.Err() != nil {
		return out
	}

	cfg, _ := readOpenAITurnStateHunterConfig(a)
	selected := make(map[int64]bool, len(cfg.ProxyIDs))
	for _, id := range cfg.ProxyIDs {
		selected[id] = true
	}
	var proxies map[int64]Proxy
	if s.openaiTurnStateHunter != nil && s.openaiTurnStateHunter.proxyRepo != nil {
		rows, err := s.openaiTurnStateHunter.proxyRepo.ListByIDs(ctx, cfg.ProxyIDs)
		if err != nil || ctx.Err() != nil {
			return out
		}
		proxies = make(map[int64]Proxy, len(rows))
		for _, proxy := range rows {
			proxies[proxy.ID] = proxy
		}
	}
	store := s.hunterCandidateStore()
	for i := range out {
		if ctx.Err() != nil {
			break
		}
		model := out[i].Model
		previous := local[model].Candidate
		current, err := store.GetCodexHunterCandidate(ctx, codexHunterStoreKey(a, model))
		if err != nil || ctx.Err() != nil {
			continue
		} // don't turn outages into invalidations
		valid := current != nil && ValidCodexHunterCandidate(*current, time.Now()) &&
			selected[current.ProbeProxyID] && codexHunterValidState(a, current.Value, time.Now())
		if valid && proxies != nil {
			proxy, ok := proxies[current.ProbeProxyID]
			valid = ok && proxy.IsActive() && !proxy.IsExpired(time.Now()) && codexHunterProxyIdentity(&proxy) == current.ProxyIdentity
		}
		if !valid {
			s.openaiCodexTurnStateCandidates.discardCodexHunterStatusCandidate(scope, model, previous)
			continue
		}
		hash := sha256.Sum256([]byte(current.Value))
		metadata := CodexTurnStateCandidateSnapshot{
			HashPrefix: hex.EncodeToString(hash[:])[:openAICodexTurnStateCandidateHashPrefixLength],
			Length:     len(current.Value), IssuedAt: time.Unix(current.IssuedUnix, 0).UTC(), ExpiresAt: time.Unix(current.ExpiresUnix, 0).UTC(),
		}
		if previous != nil && previous.HashPrefix == metadata.HashPrefix && previous.IssuedAt.Equal(metadata.IssuedAt) {
			// Preserve actual local counters only for this exact candidate.
			metadata.LastObservedAt = previous.LastObservedAt
			metadata.ObservedCount = previous.ObservedCount
			metadata.ReuseCount = previous.ReuseCount
			metadata.LastReusedAt = previous.LastReusedAt
		} else {
			s.openaiCodexTurnStateCandidates.discardCodexHunterStatusCandidate(scope, model, previous)
		}
		out[i].Candidate = &metadata
	}
	return out
}

// Status refreshes can remove a stale mirror, but must not erase a newer local
// candidate written while the shared-cache lookup was in flight.
func (cache *openAICodexTurnStateCandidates) discardCodexHunterStatusCandidate(scope, model string, expected *CodexTurnStateCandidateSnapshot) {
	if cache == nil || expected == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	bucket := cache.bucketLocked(openAICodexTurnStateCandidateKey{scope: scope, model: model})
	if bucket == nil || bucket.candidate == nil {
		return
	}
	current := bucket.candidate.metadata
	if current.HashPrefix == expected.HashPrefix && current.IssuedAt.Equal(expected.IssuedAt) && current.Length == expected.Length {
		bucket.candidate = nil
	}
}
