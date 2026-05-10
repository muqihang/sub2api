package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func TestCleanGeminiNativeThoughtSignatures_ReplacesNestedThoughtSignatures(t *testing.T) {
	input := []byte(`{
		"contents": [
			{
				"role": "user",
				"parts": [{"text": "hello"}]
			},
			{
				"role": "model",
				"parts": [
					{"text": "thinking", "thought": true, "thoughtSignature": "sig_1"},
					{"functionCall": {"name": "toolA", "args": {"k": "v"}}, "thoughtSignature": "sig_2"}
				]
			}
		],
		"cachedContent": {
			"parts": [{"text": "cached", "thoughtSignature": "sig_3"}]
		},
		"signature": "keep_me"
	}`)

	cleaned := CleanGeminiNativeThoughtSignatures(input)

	var got map[string]any
	require.NoError(t, json.Unmarshal(cleaned, &got))

	require.NotContains(t, string(cleaned), `"thoughtSignature":"sig_1"`)
	require.NotContains(t, string(cleaned), `"thoughtSignature":"sig_2"`)
	require.Contains(t, string(cleaned), `"thoughtSignature":"sig_3"`)
	require.Contains(t, string(cleaned), `"thoughtSignature":"`+antigravity.DummyThoughtSignature+`"`)
	require.Contains(t, string(cleaned), `"signature":"keep_me"`)
}

func TestCleanGeminiNativeThoughtSignatures_InvalidJSONReturnsOriginal(t *testing.T) {
	input := []byte(`{"contents":[invalid-json]}`)

	cleaned := CleanGeminiNativeThoughtSignatures(input)

	require.Equal(t, input, cleaned)
}

func TestCleanGeminiNativeThoughtSignaturesDetailed_OnlyReplacesContentPartSignatures(t *testing.T) {
	input := []byte(`{
		"contents":[
			{"role":"model","parts":[{"thoughtSignature":"sig_part","text":"thinking"}]}
		],
		"tool_schema":{"thoughtSignature":"sig_schema"},
		"signature":"keep_signature"
	}`)

	result := CleanGeminiNativeThoughtSignaturesDetailed(input)
	require.Equal(t, 1, result.ReplacedCount)
	require.Contains(t, string(result.Body), `"thoughtSignature":"`+antigravity.DummyThoughtSignature+`"`)
	require.Contains(t, string(result.Body), `"thoughtSignature":"sig_schema"`)
	require.Contains(t, string(result.Body), `"signature":"keep_signature"`)
}
