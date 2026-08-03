package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexVersionConstantsConsistency(t *testing.T) {
	const expectedVersion = "0.144.1"

	require.Equal(t, expectedVersion, codexCLIVersion)
	require.Equal(t, codexCLIVersion, openAICodexProbeVersion)
	require.True(t, strings.Contains(codexCLIUserAgent, "codex_cli_rs/"+codexCLIVersion))
	require.True(t, strings.Contains(DefaultOpenAICodexUserAgent, codexCLIVersion))
}
