package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func requireMapSlice(t *testing.T, value any) []map[string]any {
	t.Helper()
	result, ok := value.([]map[string]any)
	require.True(t, ok)
	return result
}

func TestPelicanPayloadsDoNotChangeDefaultAccountTestPayloads(t *testing.T) {
	defaultClaude, err := createTestPayload("claude-sonnet-4-6")
	require.NoError(t, err)
	defaultMessages := requireMapSlice(t, defaultClaude["messages"])
	defaultContent := requireMapSlice(t, defaultMessages[0]["content"])
	require.Equal(t, "hi", defaultContent[0]["text"])

	defaultOpenAI := createOpenAITestPayload("gpt-6-astra", true)
	defaultInput := requireMapSlice(t, defaultOpenAI["input"])
	defaultOpenAIContent := requireMapSlice(t, defaultInput[0]["content"])
	require.Equal(t, "hi", defaultOpenAIContent[0]["text"])
	require.NotContains(t, defaultOpenAI, "reasoning")

	pelicanClaude, err := createPelicanClaudePayload("claude-sonnet-4-6", "draw the pelican animation")
	require.NoError(t, err)
	pelicanMessages := requireMapSlice(t, pelicanClaude["messages"])
	pelicanContent := requireMapSlice(t, pelicanMessages[0]["content"])
	require.Equal(t, "draw the pelican animation", pelicanContent[0]["text"])

	pelicanOpenAI := createPelicanOpenAIPayload("gpt-6-astra", true, "draw the pelican animation", "medium")
	pelicanInput := requireMapSlice(t, pelicanOpenAI["input"])
	pelicanOpenAIContent := requireMapSlice(t, pelicanInput[0]["content"])
	require.Equal(t, "draw the pelican animation", pelicanOpenAIContent[0]["text"])
	require.Equal(t, map[string]any{"effort": "medium"}, pelicanOpenAI["reasoning"])
}
