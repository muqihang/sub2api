package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractOpenAIEmbeddingsUsage_ImageInputTokens(t *testing.T) {
	usage := extractOpenAIEmbeddingsUsage([]byte(`{
		"usage": {
			"prompt_tokens": 1340,
			"total_tokens": 1340,
			"prompt_tokens_details": {
				"text_tokens": 1312,
				"image_tokens": 28
			}
		}
	}`))

	require.Equal(t, 1340, usage.InputTokens)
	require.Equal(t, 28, usage.ImageInputTokens)
	require.Zero(t, usage.OutputTokens)
}

func TestExtractOpenAIEmbeddingsUsage_InputDetailsImageTokensFallback(t *testing.T) {
	usage := extractOpenAIEmbeddingsUsage([]byte(`{
		"usage": {
			"input_tokens": 20,
			"input_tokens_details": {
				"image_tokens": 3
			}
		}
	}`))

	require.Equal(t, 20, usage.InputTokens)
	require.Equal(t, 3, usage.ImageInputTokens)
}
