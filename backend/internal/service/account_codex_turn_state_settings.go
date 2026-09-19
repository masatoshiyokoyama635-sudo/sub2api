package service

import (
	"encoding/json"
	"maps"
	"math"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	codexTurnStateModeExtraKey             = "codex_turn_state_mode"
	codexTurnStateCandidateLengthsExtraKey = "codex_turn_state_candidate_lengths"
	codexTurnStateActiveCollectionExtraKey = "codex_turn_state_active_collection"
)

// GetCodexTurnStateMode is opt-in and only effective for identity v2. Resolve
// credential shadows to their credential account before reading these settings.
func (a *Account) GetCodexTurnStateMode() string {
	if a != nil && codexIdentityV2Enabled(a) && !a.IsOpenAIAgentIdentity() {
		if mode, ok := a.Extra[codexTurnStateModeExtraKey].(string); ok && (mode == "observe" || mode == "reuse") {
			return mode
		}
	}
	return "off"
}

// GetCodexTurnStateActiveCollectionEnabled requires an explicit opt-in on the
// credential account. Preserved settings remain dormant outside v2 reuse mode.
func (a *Account) GetCodexTurnStateActiveCollectionEnabled() bool {
	if a == nil || !a.IsOpenAIOAuthLike() || a.GetCodexTurnStateMode() != "reuse" {
		return false
	}
	enabled, ok := a.Extra[codexTurnStateActiveCollectionExtraKey].(bool)
	return ok && enabled
}

// GetCodexTurnStateCandidateLengths selects candidates, not model quality.
// An explicit empty list disables candidate selection while retaining observation.
func (a *Account) GetCodexTurnStateCandidateLengths() []int {
	if a != nil {
		if a.IsOpenAIAgentIdentity() {
			return []int{}
		}
		if value, present := a.Extra[codexTurnStateCandidateLengthsExtraKey]; present {
			if lengths, ok := decodeCodexTurnStateCandidateLengths(value); ok {
				return lengths
			}
		}
	}
	return []int{292, 332}
}

func hasCodexTurnStateSettingsExtra(extra map[string]any) bool {
	_, mode := extra[codexTurnStateModeExtraKey]
	_, lengths := extra[codexTurnStateCandidateLengthsExtraKey]
	_, activeCollection := extra[codexTurnStateActiveCollectionExtraKey]
	return mode || lengths || activeCollection
}

func decodeCodexTurnStateCandidateLengths(value any) ([]int, bool) {
	var values []any
	switch typed := value.(type) {
	case []any:
		values = typed
	case []int:
		values = make([]any, len(typed))
		for i, length := range typed {
			values[i] = length
		}
	default:
		return nil, false
	}
	lengths := make([]int, 0, 8)
	seen := make(map[int]struct{}, 8)
	for _, raw := range values {
		var length int
		switch number := raw.(type) {
		case int:
			length = number
		case int64:
			if number < 100 || number > 2048 {
				return nil, false
			}
			length = int(number)
		case float64:
			if math.IsNaN(number) || number < 100 || number > 2048 || math.Trunc(number) != number {
				return nil, false
			}
			length = int(number)
		case json.Number:
			var err error
			length, err = strconv.Atoi(string(number))
			if err != nil {
				return nil, false
			}
		default:
			return nil, false
		}
		if length < 100 || length > 2048 {
			return nil, false
		}
		if _, exists := seen[length]; exists {
			continue
		}
		if len(lengths) == 8 {
			return nil, false
		}
		seen[length] = struct{}{}
		lengths = append(lengths, length)
	}
	return lengths, true
}

func validateCodexTurnStateSettingsExtra(extra map[string]any) error {
	if value, present := extra[codexTurnStateActiveCollectionExtraKey]; present {
		if _, ok := value.(bool); !ok {
			return infraerrors.BadRequest("INVALID_CODEX_TURN_STATE_ACTIVE_COLLECTION", "codex_turn_state_active_collection must be a boolean")
		}
	}
	if value, present := extra[codexTurnStateModeExtraKey]; present {
		mode, ok := value.(string)
		if !ok || (mode != "off" && mode != "observe" && mode != "reuse") {
			return infraerrors.BadRequest("INVALID_CODEX_TURN_STATE_MODE", "codex_turn_state_mode must be off, observe or reuse")
		}
	}
	if value, present := extra[codexTurnStateCandidateLengthsExtraKey]; present {
		if _, ok := decodeCodexTurnStateCandidateLengths(value); !ok {
			return infraerrors.BadRequest("INVALID_CODEX_TURN_STATE_LENGTHS", "codex_turn_state_candidate_lengths must be an array of at most 8 distinct integers between 100 and 2048; an empty array disables candidate selection")
		}
	}
	return nil
}

func validateCodexTurnStateSettingsTarget(account *Account, extra map[string]any) error {
	if !hasCodexTurnStateSettingsExtra(extra) {
		return nil
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsOpenAIAgentIdentity() {
		return infraerrors.BadRequest("CODEX_TURN_STATE_ACCOUNT_INVALID", "Codex turn-state settings require an OpenAI OAuth or setup-token account; agentIdentity is not supported")
	}
	if account.IsCredentialShadow() {
		return infraerrors.BadRequest("CODEX_TURN_STATE_INHERITED", "Codex turn-state settings are inherited from the credential account; edit the parent account")
	}
	if mode, present := extra[codexTurnStateModeExtraKey]; present && mode != "off" {
		version := account.GetCodexIdentityVersion()
		if requested, ok := extra[codexIdentityVersionExtraKey].(string); ok {
			version = requested
		}
		if version != "v2" {
			return infraerrors.BadRequest("CODEX_TURN_STATE_REQUIRES_V2", "Codex turn-state observation and reuse require identity v2")
		}
	}
	if enabled, ok := extra[codexTurnStateActiveCollectionExtraKey].(bool); ok && enabled {
		version := account.GetCodexIdentityVersion()
		if requested, ok := extra[codexIdentityVersionExtraKey].(string); ok {
			version = requested
		}
		mode, _ := account.Extra[codexTurnStateModeExtraKey].(string)
		if requested, ok := extra[codexTurnStateModeExtraKey].(string); ok {
			mode = requested
		}
		if version != "v2" || mode != "reuse" {
			return infraerrors.BadRequest("CODEX_TURN_STATE_ACTIVE_COLLECTION_REQUIRES_REUSE", "active turn-state collection requires identity v2 and reuse mode")
		}
	}
	return nil
}

func preserveCodexTurnStateSettingsForUpdate(account *Account, extra map[string]any) map[string]any {
	if account == nil {
		return extra
	}
	prepared := extra
	for _, key := range []string{codexTurnStateModeExtraKey, codexTurnStateCandidateLengthsExtraKey, codexTurnStateActiveCollectionExtraKey} {
		if _, present := extra[key]; present {
			continue
		}
		if current, exists := account.Extra[key]; exists {
			if prepared == nil {
				prepared = make(map[string]any, 3)
			} else {
				prepared = maps.Clone(prepared)
			}
			prepared[key] = current
		}
	}
	return prepared
}
