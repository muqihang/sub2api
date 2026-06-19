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

func TestCP6OpenAIBridgeResponsesStreamMapsToolCallUsageCacheAndCleansReasoning(t *testing.T) {
	var upstreamPath string
	var upstreamAuth string
	var upstreamAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamAuth = r.Header.Get("Authorization")
		upstreamAPIKey = r.Header.Get("x-api-key")
		body, _ := io.ReadAll(r.Body)
		require.Contains(t, string(body), `"input"`)
		require.NotContains(t, string(body), `"messages"`)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.created","response":{"id":"resp_tool","model":"gpt-5.5"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.reasoning_summary_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"foreign hidden reasoning"}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.output_item.added\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"toolu_openai_bridge","name":"get_weather","status":"in_progress"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.function_call_arguments.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"city\":\"SF\"}","item_id":"fc_1"}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.completed\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"id":"resp_tool","model":"gpt-5.5","status":"completed","usage":{"input_tokens":21,"output_tokens":8,"input_tokens_details":{"cached_tokens":9}}}}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"weather"}],"stream":true,"tools":[{"name":"get_weather","input_schema":{"type":"object"}}]}`)
	result, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), cp6LiveOpenAIDecision(upstream.URL+"/v1"), body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, "/v1/responses", upstreamPath)
	require.Equal(t, "Bearer sk-openai-test", upstreamAuth)
	require.Empty(t, upstreamAPIKey)
	stream := string(result.Body)
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, `"type":"tool_use"`)
	require.Contains(t, stream, `"type":"input_json_delta"`)
	require.Contains(t, stream, `"partial_json":"{\"city\":\"SF\"}"`)
	require.Contains(t, stream, `"stop_reason":"tool_use"`)
	require.Contains(t, stream, `"cache_read_input_tokens":9`)
	require.NotContains(t, stream, "foreign hidden reasoning")
	require.NotContains(t, stream, "response.reasoning")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
	require.Equal(t, "responses", result.Audit.PreferredProtocol)
}

func TestCP6OpenAIBridgeResponsesStreamErrorAfterCreatedDoesNotFinalizeAsSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.created","response":{"id":"resp_created_then_failed","model":"gpt-5.5"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.failed\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.failed","response":{"id":"resp_created_then_failed","model":"gpt-5.5","status":"failed","error":{"code":"api_error","message":"provider failed"}}}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	result, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), cp6LiveOpenAIDecision(upstream.URL+"/v1"), body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, "event: error")
	require.NotContains(t, stream, "event: message_stop")
	require.NotContains(t, stream, `"stop_reason":"end_turn"`)
}

func TestCP6OpenAIBridgeResponsesStreamErrorIsSafeAnthropicError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.failed\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.failed","response":{"id":"resp_secret","model":"gpt-5.5","status":"failed","error":{"code":"rate_limit_error","message":"request req_abc123 failed with sk-live-secret at https://api.openai.com/v1/responses"}}}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	result, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), cp6LiveOpenAIDecision(upstream.URL+"/v1"), body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: error")
	require.Contains(t, stream, `"type":"rate_limit_error"`)
	require.NotContains(t, stream, "req_abc123")
	require.NotContains(t, stream, "sk-live-secret")
	require.NotContains(t, stream, "api.openai.com")
	require.NotContains(t, stream, "event: message_stop")
}

func TestCP6OpenAIBridgeResponsesStreamMissingTerminalDoesNotFinalizeAsSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.created","response":{"id":"resp_missing_terminal","model":"gpt-5.5"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.reasoning_summary_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"hidden reasoning only"}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	result, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), cp6LiveOpenAIDecision(upstream.URL+"/v1"), body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: message_start")
	require.Contains(t, stream, "event: error")
	require.NotContains(t, stream, "hidden reasoning only")
	require.NotContains(t, stream, "event: message_stop")
	require.NotContains(t, stream, `"stop_reason":"end_turn"`)
}

func TestCP6OpenAIBridgeResponsesTopLevelErrorPassthroughIsSafe(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: error\n"))
		_, _ = w.Write([]byte(`data: {"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_error","message":"request req_top_123 failed with sk-live-secret at https://api.openai.com/v1/responses"}}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	result, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), cp6LiveOpenAIDecision(upstream.URL+"/v1"), body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: error")
	require.Contains(t, stream, `"type":"rate_limit_error"`)
	require.NotContains(t, stream, "req_top_123")
	require.NotContains(t, stream, "sk-live-secret")
	require.NotContains(t, stream, "api.openai.com")
	require.NotContains(t, stream, "event: message_stop")
}

func TestCP6OpenAIBridgeRequestsUseSyntheticCodexHeadersForLocalSub2API(t *testing.T) {
	var userAgent string
	var originator string
	var stainlessLang string
	var bridgeClient string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		originator = r.Header.Get("originator")
		stainlessLang = r.Header.Get("X-Stainless-Lang")
		bridgeClient = r.Header.Get("X-Sub2API-Client-Type")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.created","response":{"id":"resp_headers","model":"gpt-5.5"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.completed\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"id":"resp_headers","model":"gpt-5.5","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	_, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), cp6LiveOpenAIDecision(upstream.URL+"/v1"), body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.NoError(t, err)
	require.Contains(t, userAgent, "codex_cli_rs/")
	require.NotContains(t, strings.ToLower(userAgent), "claude")
	require.Equal(t, "codex_cli_rs", originator)
	require.Equal(t, "go", stainlessLang)
	require.Equal(t, "claude_code_bridge_openai", bridgeClient)
}

func TestCP6OpenAIBridgeResponsesBadGatewayDoesNotFallbackToChatByDefault(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream 502"}}`))
	}))
	defer upstream.Close()
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	_, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), upstream.Client(), cp6LiveOpenAIDecision(upstream.URL+"/v1"), body, ClaudeCodeBridgeOpenAIAPIKeyFromEnv())

	require.Error(t, err)
	require.Equal(t, []string{"/v1/responses"}, paths)
	require.NotContains(t, err.Error(), "chat completions")
}

func TestCP6OpenAIBridgeLiveRejectsExternalBaseURLAndNativeFormalPool(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB", "1")
	t.Setenv("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", "sk-openai-test")

	external := cp6LiveOpenAIDecision("https://api.openai.com/v1")
	require.False(t, ClaudeCodeBridgeOpenAILiveEligible(external))
	_, err := ExecuteClaudeCodeBridgeOpenAILive(context.Background(), http.DefaultClient, external, []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`), ClaudeCodeBridgeOpenAIAPIKeyFromEnv())
	require.Error(t, err)
	require.Contains(t, err.Error(), "loopback")

	native := cp6LiveOpenAIDecision("http://127.0.0.1:1234/v1")
	native.ClientType = ClaudeCodeNativeClientType
	native.FormalPoolAllowed = true
	native.NativeAttestationAllowed = true
	native.CredentialScope = ClaudeCodeNativeCredentialScope
	require.False(t, ClaudeCodeBridgeOpenAILiveEligible(native))
}

func cp6LiveOpenAIDecision(baseURL string) ClaudeCodeBridgeRouteDecision {
	return ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp6-test-v1",
		ProviderOwner:            "zhumeng_managed",
		CredentialScope:          ClaudeCodeBridgeCredentialScope,
		GatewayLocation:          "cloud",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		PreferredProtocol:        "responses",
		OpenAIBaseURL:            strings.TrimRight(baseURL, "/"),
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsTools:            true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsCacheAudit:       true,
		SupportsReasoningMapping: true,
		SupportsErrorPassthrough: true,
		CachePolicy:              "prompt_cache_key_required_or_recommended_for_coding_agents",
	}
}
