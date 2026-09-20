package service

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// A returned state is not evidence that the requested model actually ran.
// Commit a Hunter successor only after the generation has completed, and reject
// the exact borrowed value if the upstream explicitly reports another model.
func (s *OpenAIGatewayService) finishCodexHunterHTTP(ctx context.Context, c *gin.Context, account *Account, result *OpenAIForwardResult, snapshot CodexTurnStateRequestSnapshot) {
	raw, _ := c.Get(codexTurnStateHTTPContextKey)
	attempt, ok := raw.(*codexTurnStateHTTPAttempt)
	if !ok || attempt == nil || attempt.accountID != account.ID {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if snapshot.ModelMismatch {
		if attempt.injected == "" {
			s.resetOpenAITurnStateRecovery(ctx, codexAccountIdentitySource(c, account))
		}
		// A response may already have supplied a fresh header. Neither that
		// header nor the injected candidate can confirm the requested model.
		if attempt.responseState != "" {
			s.openaiCodexTurnStateCandidates.Invalidate(attempt.scope, attempt.model, attempt.responseState)
		}
		if attempt.injected != "" {
			s.openaiCodexTurnStateCandidates.Invalidate(attempt.scope, attempt.model, attempt.injected)
			if attempt.hunterKey != "" {
				s.openaiCodexTurnStateCandidates.Invalidate(attempt.hunterScope, attempt.model, attempt.injected)
				_ = s.hunterCandidateStore().DeleteCodexHunterCandidate(ctx, attempt.hunterKey, attempt.injected)
			}
		}
		return
	}
	if attempt.hunterCandidate == nil || attempt.responseState == "" || attempt.responseState == attempt.injected || snapshot.Failed || !snapshot.ResponseModelObserved || result == nil {
		return
	}
	source := codexAccountIdentitySource(c, account)
	if source == nil {
		return
	}
	if s.accountRepo != nil {
		var err error
		source, err = s.accountRepo.GetByID(ctx, source.ID)
		if err != nil {
			return
		}
	}
	if source == nil || source.Status != StatusActive || codexHunterScope(source) != attempt.hunterScope || !codexHunterValidState(source, attempt.responseState, time.Now()) {
		return
	}
	// Revalidate opt-in and the selected proxy after a potentially long stream.
	current := s.codexHunterCandidate(ctx, source, attempt.model)
	if current == nil || current.ProbeProxyID != attempt.hunterCandidate.ProbeProxyID || current.ProxyIdentity != attempt.hunterCandidate.ProxyIdentity {
		return
	}
	issued, expires, _ := parseOpenAICodexTurnStateCandidate(attempt.responseState, time.Now())
	candidate := *attempt.hunterCandidate
	candidate.Value, candidate.IssuedUnix, candidate.ExpiresUnix = attempt.responseState, issued.Unix(), expires.Unix()
	if err := s.hunterCandidateStore().PutCodexHunterCandidate(ctx, attempt.hunterKey, candidate); err == nil {
		stored, err := s.hunterCandidateStore().GetCodexHunterCandidate(ctx, attempt.hunterKey)
		if err == nil && stored != nil && stored.Value == candidate.Value {
			s.openaiCodexTurnStateCandidates.Observe(attempt.hunterScope, attempt.model, candidate.Value, attempt.lengths, time.Now())
		}
	}
}
