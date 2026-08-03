package claude

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodePersonaDefaultsTrack2175(t *testing.T) {
	require.Equal(t, "2.1.175", CLICurrentVersion)
	require.Equal(t, "claude-cli/2.1.175 (external, sdk-cli)", DefaultHeaders["User-Agent"])
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

func TestDefaultModels_ExposeCurrentClaudeCodeNativeSetOnly(t *testing.T) {
	t.Parallel()

	ids := make([]string, 0, len(DefaultModels))
	byID := make(map[string]Model, len(DefaultModels))
	for _, model := range DefaultModels {
		ids = append(ids, model.ID)
		byID[model.ID] = model
	}

	require.Equal(t, []string{
		"claude-fable-5",
		"claude-opus-4-8",
		"claude-sonnet-5",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	}, ids)
	require.Equal(t, "Claude Fable 5", byID["claude-fable-5"].DisplayName)
	require.Equal(t, "Claude Opus 4.8", byID["claude-opus-4-8"].DisplayName)
	require.Equal(t, "Claude Sonnet 5", byID["claude-sonnet-5"].DisplayName)
	require.NotContains(t, byID, "claude-opus-4-7")
	require.NotContains(t, byID, "claude-opus-4-6")
	require.NotContains(t, byID, "claude-opus-4-5-20251101")
}

func TestDefaultModelAliasesDoNotResurrectStaleOpusOrSonnetModels(t *testing.T) {
	require.Equal(t, "claude-sonnet-4-6", DefaultTestModel)
	require.NotContains(t, ModelIDOverrides, "claude-opus-4-5")
	require.NotContains(t, ModelIDOverrides, "claude-sonnet-4-5")
	require.NotContains(t, ModelIDReverseOverrides, "claude-opus-4-5-20251101")
	require.NotContains(t, ModelIDReverseOverrides, "claude-sonnet-4-5-20250929")
	require.Equal(t, "claude-opus-4-5", NormalizeModelID("claude-opus-4-5"))
	require.Equal(t, "claude-sonnet-4-5", NormalizeModelID("claude-sonnet-4-5"))
}

func TestClaudeCode2175ProfileBetasAreExplicit(t *testing.T) {
	require.Equal(t, "mid-conversation-system-2026-04-07", BetaMidConversationSystem)
	require.Equal(t, []string{
		BetaClaudeCode,
		BetaContext1M,
		BetaInterleavedThinking,
		BetaContextManagement,
		BetaPromptCachingScope,
		BetaMidConversationSystem,
		BetaEffort,
	}, ClaudeCode2175Subscription1MBetas())
	require.Equal(t, []string{
		BetaClaudeCode,
		BetaInterleavedThinking,
		BetaContextManagement,
		BetaPromptCachingScope,
		BetaMidConversationSystem,
		BetaEffort,
	}, ClaudeCode2175APIKeyNon1MBetas())
}
