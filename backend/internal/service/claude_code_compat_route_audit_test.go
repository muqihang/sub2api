package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnthropicCompatAuditSummaryRecordsRouteNormalization(t *testing.T) {
	decision, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, "/v1/messages", []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`))
	require.NoError(t, err)

	audit := NewAnthropicCompatAuditSummary(decision)

	require.Equal(t, "/v1/messages", audit.InboundRoute)
	require.Equal(t, "/v1/messages?beta=true", audit.CCGatewayRoute)
	require.Equal(t, "claude_code_compat", audit.ClientType)
	require.Equal(t, "server_selected", audit.PersonaSource)
}

func TestSanitizeAnthropicCompatInboundHeadersDropsUntrustedPersona(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-beta", "client-beta")
	headers.Set("x-app", "fake-cli")
	headers.Set("x-claude-code-session-id", "00000000-0000-4000-8000-000000000000")
	headers.Set("x-stainless-runtime", "fake-runtime")
	headers.Set("x-sub2api-persona-trusted", "1")
	headers.Set("authorization", "Bearer RAW_TOKEN_SENTINEL")
	headers.Set("content-type", "application/json")
	headers.Set("accept", "application/json")

	sanitized := SanitizeAnthropicCompatInboundHeaders(headers)

	require.Empty(t, sanitized.Get("anthropic-beta"))
	require.Empty(t, sanitized.Get("x-app"))
	require.Empty(t, sanitized.Get("x-claude-code-session-id"))
	require.Empty(t, sanitized.Get("x-stainless-runtime"))
	require.Empty(t, sanitized.Get("x-sub2api-persona-trusted"))
	require.Empty(t, sanitized.Get("authorization"))
	require.Equal(t, "application/json", sanitized.Get("content-type"))
	require.Equal(t, "application/json", sanitized.Get("accept"))
}
