//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildOpenAIGatewayClientTemplates_WithGatewayToken(t *testing.T) {
	templates := BuildOpenAIGatewayClientTemplates("https://api.example.com", "sk-user", "gw-123")
	require.Equal(t, "https://api.example.com", templates.APIBaseURL)
	require.Equal(t, "https://api.example.com/openai", templates.ProbeBaseURL)
	require.Contains(t, templates.CurlExample, "X-OpenAI-Gateway-Token: gw-123")
	require.Contains(t, templates.PythonSDK, "default_headers")
	require.Contains(t, templates.NodeSDK, "defaultHeaders")
	require.Contains(t, templates.CodexWrapperSH, "OPENAI_GATEWAY_TOKEN")
}

func TestBuildOpenAIGatewayClientTemplates_WithoutGatewayToken(t *testing.T) {
	templates := BuildOpenAIGatewayClientTemplates("https://api.example.com/", "", "")
	require.Equal(t, "https://api.example.com", templates.APIBaseURL)
	require.False(t, strings.Contains(templates.CurlExample, "X-OpenAI-Gateway-Token"))
	require.Contains(t, templates.CodexAuthJSON, "<SUB2API_API_KEY>")
}
