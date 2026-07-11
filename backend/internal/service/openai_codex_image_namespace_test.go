package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripOpenAIImageGenerationToolsRemovesCodexNamespaceDeclarations(t *testing.T) {
	body := map[string]any{
		"tools": []any{
			map[string]any{"type": "namespace", "name": "image_gen", "tools": []any{map[string]any{"type": "function", "name": "imagegen"}}},
			map[string]any{"type": "function", "name": "keep"},
		},
		"tool_choice": map[string]any{"type": "namespace", "name": "image_gen"},
	}

	require.True(t, stripOpenAIImageGenerationTools(body))
	require.Equal(t, []any{map[string]any{"type": "function", "name": "keep"}}, body["tools"])
	_, hasToolChoice := body["tool_choice"]
	require.False(t, hasToolChoice)
}
