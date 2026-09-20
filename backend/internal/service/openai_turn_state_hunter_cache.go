package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CodexHunterCandidate is private cache data, never account Extra or an API DTO.
// The selected hunt proxy and the account's normal egress are independent.
type CodexHunterCandidate struct {
	Value         string `json:"value"`
	ProbeProxyID  int64  `json:"probe_proxy_id"`
	ProxyIdentity string `json:"proxy_identity"`
	IssuedUnix    int64  `json:"issued_unix"`
	ExpiresUnix   int64  `json:"expires_unix"`
	// Rejected retains a bounded tombstone so late writes cannot resurrect a
	// candidate which a real upstream request has already invalidated.
	Rejected bool `json:"rejected,omitempty"`
}

func ValidCodexHunterCandidate(value CodexHunterCandidate, now time.Time) bool {
	if value.Rejected || value.ProbeProxyID <= 0 || len(value.ProxyIdentity) != sha256.Size*2 {
		return false
	}
	if _, err := hex.DecodeString(value.ProxyIdentity); err != nil {
		return false
	}
	issued, expires, valid := parseOpenAICodexTurnStateCandidate(value.Value, now)
	return valid && issued.Unix() == value.IssuedUnix && expires.Unix() == value.ExpiresUnix
}

type CodexHunterCandidateStore interface {
	GetCodexHunterCandidate(context.Context, string) (*CodexHunterCandidate, error)
	PutCodexHunterCandidate(context.Context, string, CodexHunterCandidate) error
	DeleteCodexHunterCandidate(context.Context, string, string) error
}

type codexHunterLocalStore struct {
	mu      sync.Mutex
	entries map[string]CodexHunterCandidate
}

func (s *codexHunterLocalStore) GetCodexHunterCandidate(_ context.Context, key string) (*CodexHunterCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.entries[key]
	if !ok || time.Now().Unix() >= v.ExpiresUnix {
		delete(s.entries, key)
		return nil, nil
	}
	if !ValidCodexHunterCandidate(v, time.Now()) {
		return nil, nil
	}
	return &v, nil
}

func (s *codexHunterLocalStore) PutCodexHunterCandidate(_ context.Context, key string, value CodexHunterCandidate) error {
	if !ValidCodexHunterCandidate(value, time.Now()) {
		return fmt.Errorf("invalid or expired Codex hunter candidate")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]CodexHunterCandidate)
	}
	for k, v := range s.entries {
		if time.Now().Unix() >= v.ExpiresUnix {
			delete(s.entries, k)
		}
	}
	if old, ok := s.entries[key]; ok && (old.IssuedUnix >= value.IssuedUnix || old.Value == value.Value) {
		return nil
	}
	if len(s.entries) >= 1024 {
		for k := range s.entries {
			delete(s.entries, k)
			break
		}
	}
	s.entries[key] = value
	return nil
}

func (s *codexHunterLocalStore) DeleteCodexHunterCandidate(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == "" {
		delete(s.entries, key)
		return nil
	}
	if old, ok := s.entries[key]; ok {
		if old.Value == value {
			old.Rejected = true
			s.entries[key] = old
		}
	} else {
		if s.entries == nil {
			s.entries = make(map[string]CodexHunterCandidate)
		}
		for k, v := range s.entries {
			if time.Now().Unix() >= v.ExpiresUnix {
				delete(s.entries, k)
			}
		}
		if len(s.entries) >= 1024 {
			for k := range s.entries {
				delete(s.entries, k)
				break
			}
		}
		s.entries[key] = CodexHunterCandidate{Value: value, Rejected: true, ExpiresUnix: time.Now().Add(time.Hour).Unix()}
	}
	return nil
}

func codexHunterScope(account *Account) string {
	if account == nil {
		return ""
	}
	cfg, _ := readOpenAITurnStateHunterConfig(account)
	raw, _ := json.Marshal([]any{codexTurnStateCandidateScope(account, account), account.GetCodexTurnStateCandidateLengths(), cfg})
	sum := sha256.Sum256(raw)
	return "hunter:" + hex.EncodeToString(sum[:])
}

func codexHunterStoreKey(account *Account, model string) string {
	sum := sha256.Sum256([]byte(codexHunterScope(account) + "\x00" + model))
	return hex.EncodeToString(sum[:])
}

func codexHunterProxyIdentity(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(proxy.URL()))
	return hex.EncodeToString(sum[:])
}

func (s *OpenAIGatewayService) hunterCandidateStore() CodexHunterCandidateStore {
	if store, ok := s.cache.(CodexHunterCandidateStore); ok {
		return store
	}
	return &s.codexHunterLocal
}

func (s *OpenAIGatewayService) codexHunterReusable(a *Account) bool {
	return a != nil && a.IsOpenAIOAuthLike() && !a.IsOpenAIAgentIdentity() && !a.IsCredentialShadow() && a.GetCodexIdentityVersion() == "v2" && a.GetCodexTurnStateMode() == "reuse"
}

func codexHunterValidState(a *Account, value string, now time.Time) bool {
	if _, _, ok := parseOpenAICodexTurnStateCandidate(value, now); !ok {
		return false
	}
	for _, n := range a.GetCodexTurnStateCandidateLengths() {
		if len(value) == n {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) codexHunterCandidate(ctx context.Context, a *Account, model string) *CodexHunterCandidate {
	if !s.codexHunterReusable(a) || !a.IsOpenAITurnStateHunterEnabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := s.hunterCandidateStore().GetCodexHunterCandidate(ctx, codexHunterStoreKey(a, model))
	if err != nil || value == nil || !codexHunterValidState(a, value.Value, time.Now()) {
		return nil
	}
	cfg, _ := readOpenAITurnStateHunterConfig(a)
	selected := false
	for _, id := range cfg.ProxyIDs {
		if id == value.ProbeProxyID {
			selected = true
			break
		}
	}
	if !selected {
		return nil
	}
	// A proxy edit with the same database ID invalidates the previous candidate.
	if s.openaiTurnStateHunter != nil && s.openaiTurnStateHunter.proxyRepo != nil {
		proxies, err := s.openaiTurnStateHunter.proxyRepo.ListByIDs(ctx, []int64{value.ProbeProxyID})
		if err != nil || len(proxies) != 1 || !proxies[0].IsActive() || proxies[0].IsExpired(time.Now()) || codexHunterProxyIdentity(&proxies[0]) != value.ProxyIdentity {
			return nil
		}
	}
	return value
}

func (s *OpenAIGatewayService) codexHunterNewestUsableExpiry(ctx context.Context, a *Account, model string, now time.Time) (time.Time, bool) {
	var expires time.Time
	for _, entry := range s.openaiCodexTurnStateCandidates.Snapshot(codexTurnStateCandidateScope(a, a), now) {
		if entry.Model == model && entry.Candidate != nil {
			for _, length := range a.GetCodexTurnStateCandidateLengths() {
				if length == entry.Candidate.Length {
					expires = entry.Candidate.ExpiresAt
				}
			}
		}
	}
	if candidate := s.codexHunterCandidate(ctx, a, model); candidate != nil {
		if end := time.Unix(candidate.ExpiresUnix, 0); end.After(expires) {
			expires = end
		}
	}
	return expires, now.Before(expires)
}

func (s *OpenAIGatewayService) codexHunterHasCandidateModel(a *Account, model string) bool {
	for _, entry := range s.openaiCodexTurnStateCandidates.Snapshot(codexTurnStateCandidateScope(a, a), time.Now()) {
		if entry.Model == model && entry.ObservedCount > 0 {
			return true
		}
	}
	return false
}
