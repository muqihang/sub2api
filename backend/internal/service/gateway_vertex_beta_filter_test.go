package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterVertexBetaTokensKeepsOnlyVertexSupportedTokens(t *testing.T) {
	drop := map[string]struct{}{
		"context-management-2025-06-27": {},
	}

	got := filterVertexBetaTokens("oauth-2025-04-20,context-management-2025-06-27,interleaved-thinking-2025-05-14,advisor-tool-2026-03-01,interleaved-thinking-2025-05-14", drop)

	require.Equal(t, "interleaved-thinking-2025-05-14", got)
}

func TestFilterVertexBetaTokensReturnsEmptyWhenNoSupportedTokensRemain(t *testing.T) {
	got := filterVertexBetaTokens("oauth-2025-04-20,advisor-tool-2026-03-01,thinking-token-count-2026-05-13", nil)

	require.Empty(t, got)
}
