package service

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var openAITurnStateHuntReasoningEfforts = map[string]struct{}{
	"minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {},
}

// OpenAITurnStateManagedExtraKeys are written only by the background services.
// Full account edits must merge the current database values under the row lock.
func OpenAITurnStateManagedExtraKeys() []string {
	return []string{openAITurnStateHuntExtraKey, openAITurnStateRecoveryStateExtraKey}
}

// StripOpenAITurnStateManagedExtra removes runtime history from portable account
// exports. Runtime observations are never accepted as imported configuration.
func StripOpenAITurnStateManagedExtra(extra map[string]any) map[string]any {
	result := maps.Clone(extra)
	for _, key := range OpenAITurnStateManagedExtraKeys() {
		delete(result, key)
	}
	return result
}

func hasOpenAITurnStateHunterSettingsExtra(extra map[string]any) bool {
	_, hunter := extra[openAITurnStateHunterExtraKey]
	_, recovery := extra[openAITurnStateRecoveryExtraKey]
	return hunter || recovery
}

// ValidateOpenAITurnStateHunterExtra validates both background probe policies at
// the shared service boundary, including bulk imports and older admin clients.
func ValidateOpenAITurnStateHunterExtra(extra map[string]any) error {
	for _, key := range OpenAITurnStateManagedExtraKeys() {
		if _, present := extra[key]; present {
			return infraerrors.BadRequest("CODEX_HUNTER_RUNTIME_READ_ONLY", key+" is managed by the background service and cannot be written by clients")
		}
	}
	if err := validateOpenAITurnStateRecoveryExtra(extra); err != nil {
		return err
	}
	table, present, err := openAITurnStateConfigTable(extra, openAITurnStateHunterExtraKey)
	if err != nil || !present {
		return err
	}
	for _, key := range []string{"enabled", "hold_when_degraded", "auto_models"} {
		if err := openAITurnStateConfigBool(table, openAITurnStateHunterExtraKey, key); err != nil {
			return err
		}
	}
	models, err := openAITurnStateHunterStringList(table, "models", openAITurnStateHunterMaxModels)
	if err != nil {
		return err
	}
	proxies, err := openAITurnStateHunterIDs(table, "proxy_ids", openAITurnStateHunterMaxProxies)
	if err != nil {
		return err
	}
	rotating, err := openAITurnStateHunterIDs(table, "rotating_proxy_ids", openAITurnStateHunterMaxProxies)
	if err != nil {
		return err
	}
	for id := range rotating {
		if _, ok := proxies[id]; !ok {
			return openAITurnStateConfigError(openAITurnStateHunterExtraKey, "rotating_proxy_ids must be a subset of proxy_ids")
		}
	}
	for key, maximum := range map[string]int{
		"max_per_hour":     openAITurnStateHunterMaxPerHourCap,
		"lead_minutes":     openAITurnStateHunterMaxLeadMinutes,
		"retry_minutes":    openAITurnStateHunterMaxMinutes,
		"gap_seconds":      openAITurnStateHunterMaxGapSeconds,
		"usage_api_key_id": math.MaxInt32,
	} {
		if err := openAITurnStateHunterIntField(table, key, 0, maximum); err != nil {
			return err
		}
	}
	if err := openAITurnStateHunterIntField(table, "idle_minutes", -1, openAITurnStateHunterMaxMinutes); err != nil {
		return err
	}
	if err := openAITurnStateConfigEffort(table, openAITurnStateHunterExtraKey); err != nil {
		return err
	}
	enabled, _ := table["enabled"].(bool)
	autoModels, _ := table["auto_models"].(bool)
	if enabled && ((models == 0 && !autoModels) || len(proxies) == 0) {
		return openAITurnStateConfigError(openAITurnStateHunterExtraKey, "enabled requires models (or auto_models) and proxy_ids")
	}
	return nil
}

func validateOpenAITurnStateRecoveryExtra(extra map[string]any) error {
	table, present, err := openAITurnStateConfigTable(extra, openAITurnStateRecoveryExtraKey)
	if err != nil || !present {
		return err
	}
	if err := openAITurnStateConfigBool(table, openAITurnStateRecoveryExtraKey, "enabled"); err != nil {
		return err
	}
	if model, present := table["model"]; present && model != nil {
		value, ok := model.(string)
		if !ok || len(strings.TrimSpace(value)) > openAICodexTurnStateCandidateMaxModelBytes {
			return openAITurnStateConfigError(openAITurnStateRecoveryExtraKey, "model must be a string of at most 128 bytes")
		}
	}
	for key, maximum := range map[string]int{
		"streak_target":    openAITurnStateRecoveryMaxStreak,
		"min_minutes":      openAITurnStateRecoveryMaxMinutes,
		"max_minutes":      openAITurnStateRecoveryMaxMinutes,
		"cooldown_hours":   openAITurnStateRecoveryMaxCooldownHours,
		"usage_api_key_id": math.MaxInt32,
	} {
		if err := openAITurnStateConfigInt(table, openAITurnStateRecoveryExtraKey, key, 0, maximum); err != nil {
			return err
		}
	}
	minimum, maximum := float64(defaultOpenAITurnStateRecoveryMinMinutes), float64(defaultOpenAITurnStateRecoveryMaxMinutes)
	if value, ok := openAITurnStateHunterNumber(table["min_minutes"]); ok && value > 0 {
		minimum = value
	}
	if value, ok := openAITurnStateHunterNumber(table["max_minutes"]); ok && value > 0 {
		maximum = value
	}
	if minimum > maximum {
		return openAITurnStateConfigError(openAITurnStateRecoveryExtraKey, "min_minutes must not exceed max_minutes")
	}
	return openAITurnStateConfigEffort(table, openAITurnStateRecoveryExtraKey)
}

func validateOpenAITurnStateHunterTarget(account *Account, extra map[string]any) error {
	if !hasOpenAITurnStateHunterSettingsExtra(extra) {
		return nil
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsOpenAIAgentIdentity() {
		return infraerrors.BadRequest("CODEX_HUNTER_ACCOUNT_INVALID", "background turn-state probes require an OpenAI OAuth or setup-token account without agentIdentity")
	}
	if account.IsCredentialShadow() {
		return infraerrors.BadRequest("CODEX_HUNTER_INHERITED", "background turn-state probe settings belong to the credential account; edit the parent account")
	}
	version := account.GetCodexIdentityVersion()
	if requested, ok := extra[codexIdentityVersionExtraKey].(string); ok {
		version = requested
	}
	mode, _ := account.Extra[codexTurnStateModeExtraKey].(string)
	if requested, ok := extra[codexTurnStateModeExtraKey].(string); ok {
		mode = requested
	}
	for _, key := range []string{openAITurnStateHunterExtraKey, openAITurnStateRecoveryExtraKey} {
		table, _, _ := openAITurnStateConfigTable(extra, key)
		enabled, _ := table["enabled"].(bool)
		if !enabled {
			continue
		}
		if version != "v2" || (key == openAITurnStateHunterExtraKey && mode != "reuse") || (key == openAITurnStateRecoveryExtraKey && mode != "observe" && mode != "reuse") {
			return infraerrors.BadRequest("CODEX_HUNTER_REQUIRES_V2", "background hunter requires identity v2 and reuse mode; recovery probes require identity v2 with observe or reuse mode")
		}
	}
	return nil
}

func preserveOpenAITurnStateHunterSettingsForUpdate(account *Account, extra map[string]any) map[string]any {
	if account == nil {
		return extra
	}
	prepared := extra
	for _, key := range []string{openAITurnStateHunterExtraKey, openAITurnStateRecoveryExtraKey} {
		if _, present := extra[key]; present {
			continue
		}
		if current, present := account.Extra[key]; present {
			prepared = maps.Clone(prepared)
			if prepared == nil {
				prepared = make(map[string]any)
			}
			prepared[key] = current
		}
	}
	return prepared
}

func openAITurnStateConfigError(key, message string) error {
	return infraerrors.BadRequest("INVALID_CODEX_HUNTER_SETTINGS", key+"."+message)
}

func openAITurnStateConfigTable(extra map[string]any, key string) (map[string]any, bool, error) {
	raw, present := extra[key]
	if !present || raw == nil {
		return nil, false, nil
	}
	table, ok := raw.(map[string]any)
	if !ok {
		return nil, false, openAITurnStateConfigError(key, "must be an object")
	}
	return table, true, nil
}

func openAITurnStateConfigBool(table map[string]any, prefix, key string) error {
	if value, present := table[key]; present && value != nil {
		if _, ok := value.(bool); !ok {
			return openAITurnStateConfigError(prefix, key+" must be a boolean")
		}
	}
	return nil
}

func openAITurnStateConfigEffort(table map[string]any, prefix string) error {
	value, present := table["reasoning_effort"]
	if !present || value == nil {
		return nil
	}
	effort, ok := value.(string)
	if !ok {
		return openAITurnStateConfigError(prefix, "reasoning_effort must be a string")
	}
	if effort = strings.TrimSpace(effort); effort != "" {
		if _, known := openAITurnStateHuntReasoningEfforts[effort]; !known {
			return openAITurnStateConfigError(prefix, "reasoning_effort is unsupported")
		}
	}
	return nil
}

func openAITurnStateHunterStringList(table map[string]any, key string, limit int) (int, error) {
	raw, present := table[key]
	if !present || raw == nil {
		return 0, nil
	}
	var values []any
	switch list := raw.(type) {
	case []any:
		values = list
	case []string:
		for _, value := range list {
			values = append(values, value)
		}
	default:
		return 0, openAITurnStateConfigError(openAITurnStateHunterExtraKey, key+" must be an array")
	}
	if len(values) > limit {
		return 0, openAITurnStateConfigError(openAITurnStateHunterExtraKey, fmt.Sprintf("%s exceeds %d entries", key, limit))
	}
	for _, value := range values {
		model, ok := value.(string)
		if !ok || strings.TrimSpace(model) == "" || len(model) > openAICodexTurnStateCandidateMaxModelBytes {
			return 0, openAITurnStateConfigError(openAITurnStateHunterExtraKey, key+" must contain non-empty model strings of at most 128 bytes")
		}
	}
	return len(values), nil
}

func openAITurnStateHunterIDs(table map[string]any, key string, limit int) (map[int64]struct{}, error) {
	result := map[int64]struct{}{}
	raw, present := table[key]
	if !present || raw == nil {
		return result, nil
	}
	var values []any
	switch list := raw.(type) {
	case []any:
		values = list
	case []int64:
		for _, value := range list {
			values = append(values, value)
		}
	case []int:
		for _, value := range list {
			values = append(values, value)
		}
	default:
		return nil, openAITurnStateConfigError(openAITurnStateHunterExtraKey, key+" must be an array")
	}
	if len(values) > limit {
		return nil, openAITurnStateConfigError(openAITurnStateHunterExtraKey, fmt.Sprintf("%s exceeds %d entries", key, limit))
	}
	for _, value := range values {
		id, ok := openAITurnStateHunterNumber(value)
		if !ok || id < 1 || id > math.MaxInt32 || math.Trunc(id) != id {
			return nil, openAITurnStateConfigError(openAITurnStateHunterExtraKey, key+" must contain positive integer IDs")
		}
		result[int64(id)] = struct{}{}
	}
	return result, nil
}

func openAITurnStateHunterIntField(table map[string]any, key string, minimum, maximum int) error {
	return openAITurnStateConfigInt(table, openAITurnStateHunterExtraKey, key, minimum, maximum)
}

func openAITurnStateConfigInt(table map[string]any, prefix, key string, minimum, maximum int) error {
	value, present := table[key]
	if !present || value == nil {
		return nil
	}
	number, ok := openAITurnStateHunterNumber(value)
	if !ok || math.Trunc(number) != number || number < float64(minimum) || number > float64(maximum) {
		return openAITurnStateConfigError(prefix, fmt.Sprintf("%s must be an integer in [%d, %d]", key, minimum, maximum))
	}
	return nil
}

func openAITurnStateHunterNumber(value any) (float64, bool) {
	var result float64
	switch number := value.(type) {
	case float64:
		result = number
	case float32:
		result = float64(number)
	case int:
		result = float64(number)
	case int64:
		result = float64(number)
	case json.Number:
		var err error
		result, err = number.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return result, !math.IsNaN(result) && !math.IsInf(result, 0)
}
