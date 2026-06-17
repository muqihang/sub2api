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

func TestCP6DeepSeekOpenAICompatibleFallbackPostsChatCompletionsAndMapsSSE(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	var gotPath string
	var gotAuth string
	var gotClientType string
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotClientType = r.Header.Get(ClaudeCodeNativeClientTypeHeader)
		gotBody = string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_deepseek\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"hidden fallback reasoning\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_deepseek\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"fallback ok\"}}],\"usage\":{\"prompt_cache_hit_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_deepseek\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":2,\"prompt_cache_hit_tokens\":7,\"prompt_cache_miss_tokens\":4,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	decision := cp6LiveDeepSeekDecision(upstream.URL + "/anthropic")
	decision.PreferredProtocol = "openai_chat_completions"
	decision.AnthropicBaseURL = ""
	decision.OpenAIBaseURL = upstream.URL
	decision.FallbackProtocol = "openai_chat_completions"
	decision.FallbackReason = "anthropic_cache_fixture_failed"
	decision.SupportsCacheAudit = true
	decision.SupportsReasoningMapping = true
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"fallback"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(context.Background(), upstream.Client(), decision, body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, "/v1/chat/completions", gotPath)
	require.Equal(t, "Bearer sk-deepseek-test-key", gotAuth)
	require.Empty(t, gotClientType)
	require.Contains(t, gotBody, `"messages"`)
	require.NotContains(t, gotBody, `"input"`)
	require.NotContains(t, gotBody, "claude_code_native")
	stream := string(result.Body)
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, "fallback ok")
	require.Contains(t, stream, "event: message_stop")
	require.Contains(t, stream, `"cache_read_input_tokens":7`)
	require.NotContains(t, stream, "hidden fallback reasoning")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
	require.Equal(t, "openai_chat_completions", result.Audit.PreferredProtocol)
	require.Equal(t, "anthropic_cache_fixture_failed", result.Audit.FallbackReason)
	require.Equal(t, 7, result.Audit.CacheReadTokens)
}

func TestCP6DeepSeekOpenAICompatibleFallbackReasoningOnlyDoesNotFinalizeAsVisibleText(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_reasoning_only\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"hidden-only reasoning\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_reasoning_only\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	decision := cp6LiveDeepSeekDecision(upstream.URL + "/anthropic")
	decision.PreferredProtocol = "openai_chat_completions"
	decision.AnthropicBaseURL = ""
	decision.OpenAIBaseURL = upstream.URL
	decision.FallbackProtocol = "openai_chat_completions"
	decision.FallbackReason = "anthropic_reasoning_fixture_failed"
	decision.SupportsCacheAudit = true
	decision.SupportsReasoningMapping = true
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"reasoning only"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(context.Background(), upstream.Client(), decision, body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, "event: error")
	require.NotContains(t, stream, "hidden-only reasoning")
	require.NotContains(t, stream, "event: message_stop")
}

func TestCP6DeepSeekOpenAICompatibleFallbackFailsClosedWithoutFixtureReasonOrWithNativeFormalPool(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"fallback"}],"stream":true}`)
	base := cp6LiveDeepSeekDecision("http://127.0.0.1:9/anthropic")
	base.PreferredProtocol = "openai_chat_completions"
	base.AnthropicBaseURL = ""
	base.OpenAIBaseURL = "http://127.0.0.1:9"
	base.FallbackProtocol = "openai_chat_completions"
	base.FallbackReason = "anthropic_cache_fixture_failed"
	base.SupportsCacheAudit = true
	base.SupportsReasoningMapping = true

	tests := []struct {
		name   string
		mutate func(*ClaudeCodeBridgeRouteDecision)
		want   string
	}{
		{name: "missing fallback reason", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.FallbackReason = "" }, want: "fixture-backed fallback reason"},
		{name: "non fixture fallback reason", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.FallbackReason = "manual_override" }, want: "fixture-backed fallback reason"},
		{name: "missing fallback protocol", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.FallbackProtocol = "" }, want: "fixture-backed fallback protocol"},
		{name: "missing cache audit capability", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.SupportsCacheAudit = false }, want: "cache and reasoning fixture capabilities"},
		{name: "missing reasoning mapping capability", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.SupportsReasoningMapping = false }, want: "cache and reasoning fixture capabilities"},
		{name: "native client spoof", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.ClientType = ClaudeCodeNativeClientType }, want: "deepseek bridge"},
		{name: "formal pool", mutate: func(d *ClaudeCodeBridgeRouteDecision) {
			d.FormalPoolAllowed = true
			d.CredentialScope = ClaudeCodeNativeCredentialScope
		}, want: "formal pool"},
		{name: "external base url", mutate: func(d *ClaudeCodeBridgeRouteDecision) { d.OpenAIBaseURL = "https://api.deepseek.com" }, want: "external providers require production billing/concurrency guard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := base
			tt.mutate(&decision)
			_, err := ExecuteClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(context.Background(), http.DefaultClient, decision, body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
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
