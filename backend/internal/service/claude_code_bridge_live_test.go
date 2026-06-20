package service

import (
	"context"
	"encoding/json"
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
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, "/anthropic/v1/messages", gotPath)
	require.Contains(t, gotBody, `"model":"deepseek-v4-pro"`)
	require.NotContains(t, gotBody, `"model":"claude-code-bridge-deepseek-v4-pro"`)
	require.Equal(t, "sk-deepseek-test-key", gotAuth)
	require.Empty(t, gotClientType)
	require.Contains(t, string(result.Body), "message_stop")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
	require.Equal(t, "claude_code_bridge_deepseek", result.Audit.ClientType)
}

func TestClaudeCodeBridgeAnthropicLiveInjectsDeepSeekCacheControlInUpstreamBody(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","system":"stable system prefix","messages":[{"role":"user","content":"stable context"},{"role":"user","content":"latest turn"}],"tools":[{"name":"Agent","description":"subagent","input_schema":{"type":"object"}}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Contains(t, gotBody, `"model":"deepseek-v4-pro"`)
	require.Contains(t, gotBody, `"system":[{"cache_control":{"type":"ephemeral"},"text":"stable system prefix","type":"text"}]`)
	require.Contains(t, gotBody, `"content":[{"cache_control":{"type":"ephemeral"},"text":"stable context","type":"text"}]`)
	require.Contains(t, gotBody, `"tools":[{"cache_control":{"type":"ephemeral"}`)
	require.Contains(t, gotBody, `"content":"latest turn"`)
}

func TestClaudeCodeBridgeAnthropicLiveAuditsDeepSeekCacheTruthWithoutRawBody(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "deepseek-unit-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY", "local-cache-audit-unit-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID", "cache-test-v1")
	var gotBody string
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_cache","type":"message","role":"assistant","content":[],"model":"deepseek-v4-pro","usage":{"input_tokens":20,"prompt_cache_hit_tokens":7,"prompt_cache_miss_tokens":13}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","system":"stable system prefix sentinel","messages":[{"role":"user","content":"stable context sentinel"},{"role":"user","content":"latest turn sentinel"}],"tools":[{"name":"Agent","description":"subagent","input_schema":{"type":"object"}}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, "/anthropic/v1/messages", gotPath)
	require.Contains(t, gotBody, `"model":"deepseek-v4-pro"`)
	require.Equal(t, "anthropic_messages", result.Audit.PreferredProtocol)
	require.Equal(t, "anthropic_messages", result.Audit.SelectedProtocol)
	require.False(t, result.Audit.FallbackUsed)
	require.Equal(t, "/anthropic/v1/messages", result.Audit.UpstreamPathKind)
	require.Equal(t, "deepseek_prefix_kv", result.Audit.ProviderCacheMechanism)
	require.True(t, result.Audit.CacheControlPresent)
	require.ElementsMatch(t, []string{"history", "system", "tools", "top_level"}, result.Audit.CacheControlLocations)
	require.True(t, result.Audit.CacheControlProviderIgnored)
	require.Regexp(t, `^hmac-sha256:cache-test-v1:[a-f0-9]{64}$`, result.Audit.StablePrefixHMAC)
	require.Equal(t, "lt_1k", result.Audit.StablePrefixTokenBucket)
	require.Equal(t, 7, result.Audit.CacheReadTokens)
	require.Equal(t, 13, result.Audit.CacheMissTokens)
	rawAudit, err := json.Marshal(result.Audit)
	require.NoError(t, err)
	require.NotContains(t, string(rawAudit), "stable system prefix sentinel")
	require.NotContains(t, string(rawAudit), "stable context sentinel")
	require.NotContains(t, string(rawAudit), "latest turn sentinel")
}

func TestClaudeCodeBridgeAnthropicLiveAuditsCurrentTurnCacheControlLocation(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"stable history"},{"role":"user","content":[{"type":"text","text":"latest turn","cache_control":{"type":"ephemeral"}}]}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Contains(t, result.Audit.CacheControlLocations, "current")
}

func TestClaudeCodeBridgeAnthropicLiveAuditsDeepSeekPromptCacheFields(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_cache","type":"message","role":"assistant","content":[],"model":"deepseek-v4-pro","usage":{"input_tokens":20,"prompt_cache_hit_tokens":7,"prompt_cache_miss_tokens":13}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"cache audit"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, 7, result.Audit.CacheReadTokens)
	require.Equal(t, 13, result.Audit.CacheMissTokens)
	require.Contains(t, string(result.Body), "prompt_cache_hit_tokens")
}

func TestCP6DeepSeekAnthropicLiveStripsForeignThinkingAndSignatureSSE(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-code-bridge-deepseek-v4-pro","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"hidden chain","signature":"sig_provider_private"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"more hidden"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_more"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_stop","index":0}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"visible answer"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_stop","index":1}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "visible answer")
	require.Contains(t, stream, `"index":0`)
	require.Contains(t, stream, `"type":"content_block_start"`)
	require.NotContains(t, stream, "hidden chain")
	require.NotContains(t, stream, "more hidden")
	require.NotContains(t, stream, "sig_provider_private")
	require.NotContains(t, stream, "sig_more")
	require.NotContains(t, stream, "thinking_delta")
	require.NotContains(t, stream, "signature_delta")
	require.NotContains(t, stream, `"index":1`)
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
}

func TestCP6DeepSeekAnthropicLivePreservesToolUseInputFieldsNamedThinking(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","content":[],"model":"claude-code-bridge-deepseek-v4-pro","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_keep","name":"inspect","input":{"type":"thinking","thinking":"visible tool argument","signature":"visible signature argument"}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_stop","index":0}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":1}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"use a tool"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, `"type":"tool_use"`)
	require.Contains(t, stream, `"type":"thinking"`)
	require.Contains(t, stream, "visible tool argument")
	require.Contains(t, stream, "visible signature argument")
	require.Contains(t, stream, `"stop_reason":"tool_use"`)
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
}

func TestClaudeCodeBridgeAnthropicLiveLabBypassRejectsExternalProviderBaseURL(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	decision := cp6LiveDeepSeekDecision("https://api.deepseek.com/anthropic")
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)

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
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)
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
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"raw body"}],"stream":true}`)

	_, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.Error(t, err)
	message := err.Error()
	require.Contains(t, message, "provider throttled")
	require.NotContains(t, message, "req_secret")
	require.NotContains(t, message, "req_123456789")
	require.NotContains(t, message, "sk-live-secret")
	require.NotContains(t, message, "api.deepseek.com")
}

func TestCP6DeepSeekAnthropicLiveMissingTerminalEmitsSafeStreamError(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_missing_terminal","type":"message","role":"assistant","content":[],"model":"deepseek-v4-pro","usage":{"input_tokens":11}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial answer"}}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"missing terminal sentinel"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, "event: error")
	require.Contains(t, stream, `"type":"upstream_stream_closed"`)
	require.NotContains(t, stream, "event: message_stop")
	require.NotContains(t, stream, "missing terminal sentinel")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
}

func TestCP6DeepSeekAnthropicLiveSSEErrorIsSanitizedAndTerminal(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	const rawPromptSentinel = "raw prompt body sentinel must not leak"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: error\n"))
		_, _ = w.Write([]byte(`data: {"type":"error","error":{"type":"rate_limit_error","message":"request req_sse_123 failed with sk-live-secret at https://api.deepseek.com/anthropic/v1/messages while handling prompt: raw prompt body sentinel must not leak"}}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"raw prompt body sentinel must not leak"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: error")
	require.Contains(t, stream, `"type":"rate_limit_error"`)
	require.NotContains(t, stream, "req_sse_123")
	require.NotContains(t, stream, "sk-live-secret")
	require.NotContains(t, stream, "api.deepseek.com")
	require.NotContains(t, stream, rawPromptSentinel)
	require.NotContains(t, stream, "event: message_stop")
	require.NotContains(t, stream, `"type":"upstream_stream_closed"`)
}

func TestCP6DeepSeekAnthropicLiveMismatchedTerminalEventFailsClosed(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_mismatch","type":"message","role":"assistant","content":[],"model":"deepseek-v4-pro","usage":{"input_tokens":11}}}` + "\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"mismatch terminal sentinel"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, "event: error")
	require.Contains(t, stream, `"type":"upstream_stream_closed"`)
	require.NotContains(t, stream, "event: message_stop")
	require.NotContains(t, stream, "mismatch terminal sentinel")
}

func TestCP6DeepSeekAnthropicLiveRejectsDeferredToolSearchBeforeUpstreamDispatch(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Read","tool_reference":{"id":"native-ref"},"defer_loading":true,"input_schema":{"type":"object"}}],"stream":true}`)

	_, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), cp6LiveDeepSeekDecision(upstream.URL+"/anthropic"), body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.Error(t, err)
	require.Contains(t, err.Error(), "unresolved deferred tool")
	require.False(t, upstreamCalled)
}

func TestCP6DeepSeekOpenAICompatibleFallbackPostsChatCompletionsAndMapsSSE(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	var gotPath string
	var gotAuth string
	var gotClientType string
	var gotUserAgent string
	var gotOriginator string
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotClientType = r.Header.Get(ClaudeCodeNativeClientTypeHeader)
		gotUserAgent = r.Header.Get("User-Agent")
		gotOriginator = r.Header.Get("originator")
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
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"fallback"}],"stream":true}`)

	result, err := ExecuteClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(context.Background(), upstream.Client(), decision, body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, "/v1/chat/completions", gotPath)
	require.Equal(t, "Bearer sk-deepseek-test-key", gotAuth)
	require.Equal(t, "claude_code_bridge_deepseek", gotClientType)
	require.Contains(t, gotUserAgent, "codex_cli_rs/")
	require.Equal(t, "codex_cli_rs", gotOriginator)
	require.Contains(t, gotBody, `"model":"deepseek-v4-pro"`)
	require.NotContains(t, gotBody, `"model":"claude-code-bridge-deepseek-v4-pro"`)
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
	require.Equal(t, "openai_chat_completions", result.Audit.SelectedProtocol)
	require.True(t, result.Audit.FallbackUsed)
	require.Equal(t, "anthropic_cache_fixture_failed", result.Audit.FallbackReason)
	require.Equal(t, "/v1/chat/completions", result.Audit.UpstreamPathKind)
	require.Equal(t, "deepseek_prefix_kv", result.Audit.ProviderCacheMechanism)
	require.False(t, result.Audit.CacheControlPresent)
	require.True(t, result.Audit.CacheControlProviderIgnored)
	require.Equal(t, 7, result.Audit.CacheReadTokens)
	require.Equal(t, 4, result.Audit.CacheMissTokens)
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
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"reasoning only"}],"stream":true}`)

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
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"fallback"}],"stream":true}`)
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
		ModelID:                  "claude-code-bridge-deepseek-v4-pro",
		UpstreamModel:            "deepseek-v4-pro",
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
		SupportsCacheAudit:       true,
		SupportsErrorPassthrough: true,
		CachePolicy:              "provider_cache_audit_required",
	}
}

func TestCP6DeepSeekOpenAICompatibleFallbackRejectsDeferredToolSearchBeforeUpstreamDispatch(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.Header().Set("Content-Type", "text/event-stream")
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
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"ok","defer_loading":true}]}]}],"stream":true}`)

	_, err := ExecuteClaudeCodeBridgeDeepSeekOpenAICompatibleFallbackLive(context.Background(), upstream.Client(), decision, body, ClaudeCodeBridgeDeepSeekAPIKeyFromEnv())

	require.Error(t, err)
	require.Contains(t, err.Error(), "unresolved deferred tool")
	require.False(t, upstreamCalled)
}

func TestClaudeCodeBridgeOpenAILiveFallsBackToChatCompletionsOnlyWhenExplicitlyEnabled(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_CHAT_COMPLETIONS_FALLBACK_ENABLED", "1")
	var paths []string
	var chatBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/v1/responses" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"responses unsupported"}}`))
			return
		}
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		chatBody = string(body)
		require.Equal(t, "Bearer sk-openai-test-key", r.Header.Get("Authorization"))
		require.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		require.Contains(t, r.Header.Get("User-Agent"), "codex_cli_rs/")
		require.NotContains(t, strings.ToLower(r.Header.Get("User-Agent")), "claude")
		require.Equal(t, "codex_cli_rs", r.Header.Get("originator"))
		require.Equal(t, "claude_code_bridge_openai", r.Header.Get("X-Sub2API-Client-Type"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4-mini","choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4-mini","choices":[{"index":0,"delta":{"content":"OK"}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8,"prompt_tokens_details":{"cached_tokens":3}}}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-gpt-5.4-mini","messages":[{"role":"user","content":"reply ok"}],"stream":true,"max_tokens":8}`)

	decision := cp6LiveOpenAIDecision(upstream.URL)
	decision.ModelID = "claude-code-bridge-gpt-5.4-mini"
	decision.UpstreamModel = "gpt-5.4-mini"

	result, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), decision, body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, []string{"/v1/responses", "/v1/chat/completions"}, paths)
	require.Contains(t, chatBody, `"model":"gpt-5.4-mini"`)
	require.NotContains(t, chatBody, "claude-code-bridge-gpt-5.4-mini")
	stream := string(result.Body)
	require.Contains(t, stream, "message_start")
	require.Contains(t, stream, "OK")
	require.Contains(t, stream, "message_stop")
	require.Equal(t, 3, result.Audit.CacheReadTokens)
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
	require.Equal(t, "claude_code_bridge_openai", result.Audit.ClientType)
}

func TestClaudeCodeBridgeOpenAIResponsesLiveMapsAnthropicToolsAndPromptCacheKey(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test-key")
	var responsesBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		responsesBody = string(body)
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer sk-openai-test-key", r.Header.Get("Authorization"))
		require.Contains(t, responsesBody, `"type":"function"`)
		require.Contains(t, responsesBody, `"name":"get_weather"`)
		require.Contains(t, responsesBody, `"prompt_cache_key"`)
		require.Contains(t, responsesBody, `"effort":"high"`)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"OK"}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.completed\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.5","output":[],"usage":{"input_tokens":1600,"output_tokens":2,"input_tokens_details":{"cached_tokens":1500}}}}` + "\n\n"))
	}))
	defer upstream.Close()
	body := []byte(`{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"weather?"}],"stream":true,"max_tokens":8,"output_config":{"effort":"high"},"tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],"tool_choice":{"type":"tool","name":"get_weather"}}`)
	decision := cp6LiveOpenAIDecision(upstream.URL)
	decision.ModelID = "claude-code-bridge-gpt-5.5"
	decision.UpstreamModel = "gpt-5.5"

	result, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), decision, body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Contains(t, string(result.Body), "OK")
	require.Equal(t, 1500, result.Audit.CacheReadTokens)
	require.NotContains(t, responsesBody, "claude-code-bridge-gpt-5.5")
}

func TestClaudeCodeBridgeAnthropicLivePostsGLMAndKimiToAnthropicMessages(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	tests := []struct {
		name          string
		modelID       string
		upstreamModel string
		provider      string
		route         string
		clientType    string
	}{
		{
			name:          "zai glm",
			modelID:       "claude-code-bridge-glm-5.2-1m",
			upstreamModel: "glm-5.2[1m]",
			provider:      "zai_glm",
			route:         "zai_glm_bridge",
			clientType:    "claude_code_bridge_zai_glm",
		},
		{
			name:          "kimi",
			modelID:       "claude-code-bridge-kimi-k2.7-code",
			upstreamModel: "kimi-k2.7-code",
			provider:      "kimi",
			route:         "kimi_bridge",
			clientType:    "claude_code_bridge_kimi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var gotBody string
			var gotAuth string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				gotPath = r.URL.Path
				gotBody = string(body)
				gotAuth = r.Header.Get("x-api-key")
				require.Equal(t, "text/event-stream", r.Header.Get("Accept"))
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message_stop\n"))
				_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
			}))
			defer upstream.Close()

			decision := cp6LiveDeepSeekDecision(upstream.URL + "/anthropic")
			decision.ModelID = tt.modelID
			decision.UpstreamModel = tt.upstreamModel
			decision.Provider = tt.provider
			decision.Route = tt.route
			decision.ClientType = tt.clientType
			decision.OpenAIBaseURL = ""
			body := []byte(`{"model":"` + tt.modelID + `","messages":[{"role":"user","content":"anthropic only"}],"stream":true}`)

			result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), decision, body, "sk-provider-test-key")

			require.NoError(t, err)
			require.Equal(t, http.StatusOK, result.StatusCode)
			require.Equal(t, "/anthropic/v1/messages", gotPath)
			require.Equal(t, "sk-provider-test-key", gotAuth)
			require.Contains(t, gotBody, `"model":"`+tt.upstreamModel+`"`)
			require.NotContains(t, gotBody, tt.modelID)
			require.Equal(t, "anthropic_messages", result.Audit.PreferredProtocol)
			require.Equal(t, tt.clientType, result.Audit.ClientType)
			require.False(t, result.Audit.FormalPoolAllowed)
			require.False(t, result.Audit.NativeAttested)
		})
	}
}

func TestClaudeCodeBridgeAnthropicLiveProviderKeysDoNotRequireDeepSeekGate(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_ZAI_GLM_API_KEY", "sk-glm-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_KIMI_API_KEY", "sk-kimi-test-key")
	tests := []struct {
		name          string
		modelID       string
		upstreamModel string
		provider      string
		route         string
		clientType    string
		wantKey       string
	}{
		{
			name:          "zai glm",
			modelID:       "claude-code-bridge-glm-5.2-1m",
			upstreamModel: "glm-5.2[1m]",
			provider:      "zai_glm",
			route:         "zai_glm_bridge",
			clientType:    "claude_code_bridge_zai_glm",
			wantKey:       "sk-glm-test-key",
		},
		{
			name:          "kimi",
			modelID:       "claude-code-bridge-kimi-k2.7-code",
			upstreamModel: "kimi-k2.7-code",
			provider:      "kimi",
			route:         "kimi_bridge",
			clientType:    "claude_code_bridge_kimi",
			wantKey:       "sk-kimi-test-key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("x-api-key")
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message_stop\n"))
				_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
			}))
			defer upstream.Close()

			decision := cp6LiveDeepSeekDecision(upstream.URL + "/anthropic")
			decision.ModelID = tt.modelID
			decision.UpstreamModel = tt.upstreamModel
			decision.Provider = tt.provider
			decision.Route = tt.route
			decision.ClientType = tt.clientType
			decision.OpenAIBaseURL = ""
			body := []byte(`{"model":"` + tt.modelID + `","messages":[{"role":"user","content":"anthropic only"}],"stream":true}`)

			require.True(t, ClaudeCodeBridgeAnthropicLiveEligible(decision))
			result, err := ExecuteClaudeCodeBridgeAnthropicLive(context.Background(), upstream.Client(), decision, body, ClaudeCodeBridgeAnthropicAPIKeyFromEnv(tt.provider))

			require.NoError(t, err)
			require.Equal(t, http.StatusOK, result.StatusCode)
			require.Equal(t, tt.wantKey, gotAuth)
			require.Equal(t, tt.clientType, result.Audit.ClientType)
		})
	}
}

func TestClaudeCodeProviderBridgeLiveRequestAllowedAcceptsAgnesWithDedicatedKey(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_AGNES_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_AGNES_API_KEY", "sk-agnes-test-key")

	decision := cp6LiveOpenAIDecision("http://127.0.0.1:9/v1")
	decision.ModelID = "claude-code-bridge-agnes-2.0-flash"
	decision.UpstreamModel = "agnes-2.0-flash"
	decision.Provider = "agnes"
	decision.Route = "agnes_bridge"
	decision.ClientType = "claude_code_bridge_agnes"
	decision.SupportsReasoningMapping = false

	require.True(t, ClaudeCodeProviderBridgeLiveRequestAllowed(ClaudeCodeProviderRouteDecision{
		ModelID:                  decision.ModelID,
		UpstreamModel:            decision.UpstreamModel,
		Provider:                 decision.Provider,
		Route:                    decision.Route,
		ClientType:               decision.ClientType,
		ProviderOwner:            decision.ProviderOwner,
		CredentialScope:          decision.CredentialScope,
		GatewayLocation:          decision.GatewayLocation,
		CatalogFresh:             true,
		CatalogVersion:           decision.CatalogVersion,
		RuntimeHash:              decision.RuntimeHash,
		OverlayHash:              decision.OverlayHash,
		CatalogHash:              decision.CatalogHash,
		PreferredProtocol:        decision.PreferredProtocol,
		OpenAIBaseURL:            decision.OpenAIBaseURL,
		CapabilitiesVerified:     decision.CapabilitiesVerified,
		SupportsText:             decision.SupportsText,
		SupportsTools:            decision.SupportsTools,
		SupportsStreaming:        decision.SupportsStreaming,
		SupportsUsage:            decision.SupportsUsage,
		SupportsCacheAudit:       decision.SupportsCacheAudit,
		SupportsReasoningMapping: decision.SupportsReasoningMapping,
		SupportsErrorPassthrough: decision.SupportsErrorPassthrough,
	}))
}

func TestClaudeCodeProviderBridgeLiveRequestAllowedDoesNotApplyDeepSeekFallbackToGLMOrKimi(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", "sk-deepseek-test-key")
	for _, provider := range []string{"zai_glm", "kimi"} {
		t.Run(provider, func(t *testing.T) {
			decision := cp6LiveDeepSeekDecision("http://127.0.0.1:9/anthropic")
			decision.Provider = provider
			if provider == "zai_glm" {
				decision.ModelID = "claude-code-bridge-glm-5.2-1m"
				decision.UpstreamModel = "glm-5.2[1m]"
				decision.Route = "zai_glm_bridge"
				decision.ClientType = "claude_code_bridge_zai_glm"
			} else {
				decision.ModelID = "claude-code-bridge-kimi-k2.7-code"
				decision.UpstreamModel = "kimi-k2.7-code"
				decision.Route = "kimi_bridge"
				decision.ClientType = "claude_code_bridge_kimi"
			}
			decision.PreferredProtocol = "openai_chat_completions"
			decision.AnthropicBaseURL = ""
			decision.OpenAIBaseURL = "http://127.0.0.1:9"
			decision.FallbackProtocol = "openai_chat_completions"
			decision.FallbackReason = "anthropic_cache_fixture_failed"
			decision.SupportsCacheAudit = true
			decision.SupportsReasoningMapping = true

			require.False(t, ClaudeCodeProviderBridgeLiveRequestAllowed(ClaudeCodeProviderRouteDecision{
				ModelID:                  decision.ModelID,
				UpstreamModel:            decision.UpstreamModel,
				Provider:                 decision.Provider,
				Route:                    decision.Route,
				ClientType:               decision.ClientType,
				ProviderOwner:            decision.ProviderOwner,
				CredentialScope:          decision.CredentialScope,
				GatewayLocation:          decision.GatewayLocation,
				CatalogFresh:             true,
				CatalogVersion:           decision.CatalogVersion,
				RuntimeHash:              decision.RuntimeHash,
				OverlayHash:              decision.OverlayHash,
				CatalogHash:              decision.CatalogHash,
				PreferredProtocol:        decision.PreferredProtocol,
				OpenAIBaseURL:            decision.OpenAIBaseURL,
				FallbackProtocol:         decision.FallbackProtocol,
				FallbackReason:           decision.FallbackReason,
				CapabilitiesVerified:     decision.CapabilitiesVerified,
				SupportsText:             decision.SupportsText,
				SupportsTools:            decision.SupportsTools,
				SupportsStreaming:        decision.SupportsStreaming,
				SupportsUsage:            decision.SupportsUsage,
				SupportsCacheAudit:       decision.SupportsCacheAudit,
				SupportsReasoningMapping: decision.SupportsReasoningMapping,
				SupportsErrorPassthrough: decision.SupportsErrorPassthrough,
			}))
		})
	}
}

func TestClaudeCodeBridgeAnthropicLiveDecisionAcceptsAnthropicCompatibleProvidersWithoutFormalPool(t *testing.T) {
	base := cp6LiveDeepSeekDecision("http://127.0.0.1:9/anthropic")
	tests := []struct {
		name          string
		modelID       string
		upstreamModel string
		provider      string
		route         string
		clientType    string
	}{
		{
			name:          "deepseek",
			modelID:       "claude-code-bridge-deepseek-v4-pro",
			upstreamModel: "deepseek-v4-pro",
			provider:      "deepseek",
			route:         "deepseek_bridge",
			clientType:    "claude_code_bridge_deepseek",
		},
		{
			name:          "zai glm",
			modelID:       "claude-code-bridge-glm-5.2-1m",
			upstreamModel: "glm-5.2[1m]",
			provider:      "zai_glm",
			route:         "zai_glm_bridge",
			clientType:    "claude_code_bridge_zai_glm",
		},
		{
			name:          "kimi",
			modelID:       "claude-code-bridge-kimi-k2.7-code",
			upstreamModel: "kimi-k2.7-code",
			provider:      "kimi",
			route:         "kimi_bridge",
			clientType:    "claude_code_bridge_kimi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := base
			decision.ModelID = tt.modelID
			decision.UpstreamModel = tt.upstreamModel
			decision.Provider = tt.provider
			decision.Route = tt.route
			decision.ClientType = tt.clientType
			decision.PreferredProtocol = "anthropic_messages"
			decision.AnthropicBaseURL = "http://127.0.0.1:9/anthropic"
			decision.OpenAIBaseURL = ""
			decision.FallbackProtocol = ""
			decision.FallbackReason = ""

			require.NoError(t, ClaudeCodeBridgeAnthropicLiveDecisionValid(decision))
			require.False(t, decision.FormalPoolAllowed)
			require.False(t, decision.NativeAttestationAllowed)
			require.Equal(t, ClaudeCodeBridgeCredentialScope, decision.CredentialScope)
		})
	}
}
