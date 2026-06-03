package claude

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodePersonaDefaultsTrack2150(t *testing.T) {
	require.Equal(t, "2.1.150", CLICurrentVersion)
	require.Equal(t, "claude-cli/2.1.150 (external, sdk-cli)", DefaultHeaders["User-Agent"])
	require.Equal(t, "0.94.0", DefaultHeaders["X-Stainless-Package-Version"])
	require.Equal(t, "v24.3.0", DefaultHeaders["X-Stainless-Runtime-Version"])
	_, hasTimeout := DefaultHeaders["X-Stainless-Timeout"]
	require.False(t, hasTimeout, "X-Stainless-Timeout should be set dynamically, not from DefaultHeaders")
}

func TestClaudeCodeEndpointSpecificBetas(t *testing.T) {
	shortMessagesBody := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[],"metadata":{"user_id":"{}"}}`)
	mainMessagesBody := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[],"thinking":{"type":"enabled","budget_tokens":1024},"context_management":{"edits":[]}}`)

	require.Equal(t, []string{
		BetaClaudeCode,
		BetaInterleavedThinking,
		BetaContextManagement,
		BetaPromptCachingScope,
	}, ClaudeCodeMessagesBetas())

	require.Equal(t, []string{
		BetaClaudeCode,
		BetaInterleavedThinking,
		BetaContextManagement,
		BetaPromptCachingScope,
		BetaOAuth,
	}, ClaudeCodeMessagesOAuthBetas())

	require.Equal(t, []string{
		BetaClaudeCode,
		BetaOAuth,
		BetaInterleavedThinking,
		BetaContextManagement,
		BetaPromptCachingScope,
		BetaAdvisorTool,
		BetaEffort,
		BetaStructuredOutputs,
	}, ClaudeCodeMessagesOAuthBetasForBody(shortMessagesBody))

	require.Equal(t, []string{
		BetaClaudeCode,
		BetaOAuth,
		BetaInterleavedThinking,
		BetaContextManagement,
		BetaPromptCachingScope,
		BetaAdvisorTool,
		BetaEffort,
		BetaExtendedCacheTTL,
	}, ClaudeCodeMessagesOAuthBetasForBody(mainMessagesBody))

	require.Equal(t, []string{
		BetaClaudeCode,
		BetaInterleavedThinking,
		BetaContextManagement,
		BetaOAuth,
		BetaTokenCounting,
	}, ClaudeCodeCountTokensOAuthBetas())

	require.Equal(t, strings.Join(ClaudeCodeMessagesBetas(), ","), DefaultBetaHeader)
	require.Equal(t, strings.Join(ClaudeCodeMessagesBetas(), ","), MessageBetaHeaderNoTools)
	require.Equal(t, strings.Join(ClaudeCodeMessagesBetas(), ","), MessageBetaHeaderWithTools)
	require.Equal(t, strings.Join(ClaudeCodeCountTokensOAuthBetas(), ","), CountTokensBetaHeader)
}

func TestDefaultModels_ContainsOpus48(t *testing.T) {
	t.Parallel()

	for _, model := range DefaultModels {
		if model.ID == "claude-opus-4-8" {
			if model.DisplayName != "Claude Opus 4.8" {
				t.Fatalf("unexpected display name: %q", model.DisplayName)
			}
			return
		}
	}
	t.Fatalf("expected claude-opus-4-8 in DefaultModels")
}
