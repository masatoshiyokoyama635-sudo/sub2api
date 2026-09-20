package repository

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func mergeOpenAITurnStateRuntimeExtra(extra map[string]any, current []byte) error {
	for _, key := range service.OpenAITurnStateManagedExtraKeys() {
		delete(extra, key)
	}
	if len(current) == 0 || string(current) == "null" {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal(current, &values); err != nil {
		return err
	}
	for _, key := range service.OpenAITurnStateManagedExtraKeys() {
		if value, ok := values[key]; ok && value != nil {
			extra[key] = value
		}
	}
	return nil
}
