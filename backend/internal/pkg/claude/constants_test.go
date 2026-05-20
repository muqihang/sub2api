package claude

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodePersonaDefaultsTrack2145(t *testing.T) {
	require.Equal(t, "2.1.145", CLICurrentVersion)
	require.Equal(t, "claude-cli/2.1.145 (external, sdk-cli)", DefaultHeaders["User-Agent"])
	require.Equal(t, "0.94.0", DefaultHeaders["X-Stainless-Package-Version"])
	require.Equal(t, "v24.3.0", DefaultHeaders["X-Stainless-Runtime-Version"])
	_, hasTimeout := DefaultHeaders["X-Stainless-Timeout"]
	require.False(t, hasTimeout, "X-Stainless-Timeout should be set dynamically, not from DefaultHeaders")
}

func TestClaudeCodeEndpointSpecificBetas(t *testing.T) {
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
