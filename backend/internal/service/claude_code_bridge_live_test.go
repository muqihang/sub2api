package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodeBridgeAnthropicLivePostsRawBodyAndPassesThroughSSE(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	var gotBody string
	var gotPath string
	var gotAuth string
	var gotClientType string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("x-api-key")
		gotClientType = r.Header.Get(ClaudeCodeNativeClientTypeHeader)
		require.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		require.Equal(t, "2023-06-01", r.Header.Get("Anthropic-Version"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, "/anthropic/v1/messages", gotPath)
	require.Equal(t, string(body), gotBody)
	require.Equal(t, "sk-deepseek-test-key", gotAuth)
	require.Empty(t, gotClientType)
	require.Contains(t, string(result.Body), "message_stop")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
	require.Equal(t, "claude_code_bridge_deepseek", result.Audit.ClientType)
}

func TestClaudeCodeBridgeAnthropicLiveLabBypassRejectsExternalProviderBaseURL(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	decision := cp6LiveDeepSeekDecision("https://api.deepseek.com/anthropic")
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)

	require.False(t, ClaudeCodeBridgeAnthropicLiveEligible(decision))
	_, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), http.DefaultClient, decision, body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.Error(t, err)
	require.Contains(t, err.Error(), "external providers require production billing/concurrency guard")
}

func TestClaudeCodeBridgeAnthropicLiveLabBypassLoopbackHostParsing(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")

	allowed := cp6LiveDeepSeekDecision("http://127.42.0.1:9/anthropic")
	rejected := cp6LiveDeepSeekDecision("http://127.0.0.1.evil.example/anthropic")
	badScheme := cp6LiveDeepSeekDecision("file://127.0.0.1/anthropic")

	require.True(t, ClaudeCodeBridgeAnthropicLiveEligible(allowed))
	require.False(t, ClaudeCodeBridgeAnthropicLiveEligible(rejected))
	require.False(t, ClaudeCodeBridgeAnthropicLiveEligible(badScheme))
}

func TestClaudeCodeBridgeAnthropicLiveRejectsFormalPoolNativeAndOpenAI(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)
	base := cp6LiveDeepSeekDecision("http://127.0.0.1:9/anthropic")
	tests := []struct {
		name   string
		mutate func(*ClaudeCodeBridgeRouteDecision)
	}{
		{name: "native client", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.ClientType = ClaudeCodeNativeClientType }},
		{name: "formal pool", mutate: func(d *ClaudeCodeBridgeRouteDecision) {
			d.FormalPoolAllowed = true
			d.CredentialScope = ClaudeCodeNativeCredentialScope
		}},
		{name: "openai bridge", mutate: func(d *ClaudeCodeBridgeRouteDecision) {
			d.ModelID = "gpt-5.5"
			d.Provider = "openai"
			d.Route = "openai_bridge"
			d.ClientType = "claude_code_bridge_openai"
			d.PreferredProtocol = "responses"
			d.OpenAIBaseURL = "https://api.openai.com/v1"
		}},
		{name: "missing key", mutate: func(d *ClaudeCodeBridgeRouteDecision) {
			t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
			decision := base
			tt.mutate(&decision)

			_, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), http.DefaultClient, decision, body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), "live") || strings.Contains(err.Error(), "api key") || strings.Contains(err.Error(), "formal pool"))
		})
	}
}

func TestClaudeCodeBridgeAnthropicLiveSanitizesProviderErrors(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"provider throttled req_secret req_123456789 sk-live-secret https://api.deepseek.com/anthropic/v1/messages"}`))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)

	_, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.Error(t, err)
	message := err.Error()
	require.Contains(t, message, "provider throttled")
	require.NotContains(t, message, "req_secret")
	require.NotContains(t, message, "req_123456789")
	require.NotContains(t, message, "sk-live-secret")
	require.NotContains(t, message, "api.deepseek.com")
}

func cp6LiveDeepSeekDecision(baseURL string) ClaudeCodeBridgeRouteDecision {
	return ClaudeCodeBridgeRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		ProviderOwner:            "zhumeng_managed",
		CredentialScope:          ClaudeCodeBridgeCredentialScope,
		GatewayLocation:          "cloud",
		CatalogVersion:           "cp6-live-test",
		RuntimeHash:              "sha256:" + strings.Repeat("1", 64),
		OverlayHash:              "sha256:" + strings.Repeat("2", 64),
		CatalogHash:              "sha256:" + strings.Repeat("3", 64),
		PreferredProtocol:        "anthropic_messages",
		AnthropicBaseURL:         baseURL,
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsTools:            true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsErrorPassthrough: true,
	}
}
