package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeCodeBridgeStreamTextProducesAnthropicMessagesEventOrder(t *testing.T) {
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}
	result, err := BuildClaudeCodeBridgeSkeletonSSE(decision, []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	require.NoError(t, err)
	body := string(result.Body)
	assertSSEOrder(t, body, []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	})
	require.Contains(t, body, `"type":"text_delta"`)
	require.Contains(t, body, `"stop_reason":"end_turn"`)
	require.NotContains(t, body, "response.created")
	require.NotContains(t, body, "response.output_text.delta")
	require.NotContains(t, body, "native-attestation")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
	require.Equal(t, "claude_code_bridge_deepseek", result.Audit.ClientType)
	require.Equal(t, "cp5-test-v1", result.Audit.CatalogVersion)
}

func TestClaudeCodeBridgeStreamToolUseProducesInputJSONDeltaAndToolUseStop(t *testing.T) {
	request := map[string]any{
		"model":      "gpt-5.5",
		"messages":   []any{map[string]any{"role": "user", "content": "weather"}},
		"stream":     true,
		"max_tokens": 16,
		"tools": []any{map[string]any{
			"name":         "get_weather",
			"description":  "weather lookup",
			"input_schema": map[string]any{"type": "object"},
		}},
		"tool_choice": map[string]any{"type": "tool", "name": "get_weather"},
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}

	result, err := BuildClaudeCodeBridgeSkeletonSSE(decision, body)

	require.NoError(t, err)
	stream := string(result.Body)
	assertSSEOrder(t, stream, []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	})
	require.Contains(t, stream, `"type":"tool_use"`)
	require.Contains(t, stream, `"name":"get_weather"`)
	require.Contains(t, stream, `"type":"input_json_delta"`)
	require.Contains(t, stream, `"stop_reason":"tool_use"`)
	require.NotContains(t, stream, "reasoning_content")
	require.NotContains(t, stream, "signature")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
	require.Equal(t, "bridge_pool", result.Audit.CredentialScope)
}

func TestCP6BridgeToolUseSSEMatchesGoldenFixture(t *testing.T) {
	request := map[string]any{
		"model":      "gpt-5.5",
		"messages":   []any{map[string]any{"role": "user", "content": "weather"}},
		"stream":     true,
		"max_tokens": 16,
		"tools": []any{map[string]any{
			"name":         "get_weather",
			"description":  "weather lookup",
			"input_schema": map[string]any{"type": "object"},
		}},
		"tool_choice": map[string]any{"type": "tool", "name": "get_weather"},
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp6-tool-use-golden-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}

	result, err := BuildClaudeCodeBridgeSkeletonSSE(decision, body)

	require.NoError(t, err)
	goldenPath := filepath.Join("testdata", "claude_code_bridge", "cp6_tool_use_sse_golden.sse")
	golden, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, normalizeBridgeSSEGolden(string(golden)), normalizeBridgeSSEGolden(string(result.Body)))
	require.NotContains(t, string(result.Body), "reasoning_content")
	require.NotContains(t, string(result.Body), "signature")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
}

func TestClaudeCodeBridgeStreamRejectsOpenAIFunctionToolShape(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"foo","parameters":{}}}],"tool_choice":{"type":"function","function":{"name":"foo"}}}`)
	_, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic messages")
}

func normalizeBridgeSSEGolden(stream string) string {
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestClaudeCodeBridgeStreamRejectsOpenAIFunctionToolTypeWithoutFunctionProperty(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","name":"leak","parameters":{}}]}`)
	_, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic messages")
}

func TestClaudeCodeBridgeStreamRejectsOpenAIChatTopLevelFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"n":2,"stop":["secret-stop"],"stream_options":{"include_usage":true},"user":"user-leak"}`)
	_, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic messages")
}

func TestClaudeCodeBridgeStreamRejectsOpenAIResponsesTopLevelFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"reasoning":{"effort":"low"},"text":{"format":{"type":"text"}},"include":["message.output_text.logprobs"],"previous_response_id":"resp_leak","truncation":"auto","prompt_cache_key":"cache-leak","max_output_tokens":128,"conversation":"conv_leak","background":false}`)
	_, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic messages")
}

func TestClaudeCodeBridgeStreamRejectsInvalidAnthropicToolShapes(t *testing.T) {
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "tools not array", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":{"name":"leak"}}`, want: "tool shape"},
		{name: "tool missing name", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"input_schema":{"type":"object"}}]}`, want: "tool shape"},
		{name: "tool missing input_schema", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"leak"}]}`, want: "tool shape"},
		{name: "tool_choice tool missing name", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool"}}`, want: "tool choice"},
		{name: "tool name slash not Anthropic compatible", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"unsafe/tool","input_schema":{"type":"object"}}]}`, want: "tool shape"},
		{name: "tool_choice string not object", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":"auto"}`, want: "tool choice"},
		{name: "tool_choice names unknown tool", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"unknown_tool"}}`, want: "tool choice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildClaudeCodeBridgeSkeletonSSE(decision, []byte(tt.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestClaudeCodeBridgeStreamRejectsBodyModelMismatch(t *testing.T) {
	_, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp5-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}, []byte(`{"model":"deepseek-v4-pro","messages":[]}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "model binding")
}

func TestClaudeCodeBridgeStreamRejectsNativeOrFormalPoolDecision(t *testing.T) {
	_, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_native",
		FormalPoolAllowed:        true,
		NativeAttestationAllowed: true,
		CredentialScope:          "formal_pool",
	}, []byte(`{"model":"gpt-5.5","messages":[]}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "bridge route decision")
}

func assertSSEOrder(t *testing.T, body string, markers []string) {
	t.Helper()
	last := -1
	for _, marker := range markers {
		idx := strings.Index(body, marker)
		require.NotEqual(t, -1, idx, marker)
		require.Greater(t, idx, last, marker)
		last = idx
	}
}

func TestCP6BridgeStreamMapsProviderFixtureReasoningUsageCacheAndErrors(t *testing.T) {
	request := map[string]any{
		"model":         "deepseek-v4-pro",
		"messages":      []any{map[string]any{"role": "user", "content": "think safely"}},
		"stream":        true,
		"max_tokens":    32,
		"thinking":      map[string]any{"type": "enabled"},
		"output_config": map[string]any{"effort": "max"},
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		CatalogVersion:           "cp6-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
		PreferredProtocol:        "anthropic_messages",
		FallbackProtocol:         "openai_chat_completions",
		CapabilitiesVerified:     true,
		SupportsText:             true,
		SupportsStreaming:        true,
		SupportsUsage:            true,
		SupportsCacheAudit:       true,
		SupportsReasoningMapping: true,
		SupportsErrorPassthrough: true,
		ReasoningEffortLevels:    []string{"high", "max"},
		CachePolicy:              "provider_prefix_kv_cache_automatic_full_prefix_unit_match",
	}
	fixture := ClaudeCodeBridgeProviderFixture{
		TextDeltas:       []string{"safe final"},
		ReasoningDeltas:  []string{"hidden chain"},
		InputTokens:      11,
		OutputTokens:     7,
		CacheReadTokens:  5,
		CacheWriteTokens: 3,
		StopReason:       "end_turn",
	}

	result, err := BuildClaudeCodeBridgeFixtureSSE(decision, body, fixture)

	require.NoError(t, err)
	stream := string(result.Body)
	assertSSEOrder(t, stream, []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	})
	require.Contains(t, stream, `"text":"safe final"`)
	require.NotContains(t, stream, "hidden chain")
	require.NotContains(t, stream, "reasoning_content")
	require.NotContains(t, stream, "signature")
	require.Equal(t, "anthropic_messages", result.Audit.PreferredProtocol)
	require.Equal(t, "openai_chat_completions", result.Audit.FallbackProtocol)
	require.Equal(t, "provider_prefix_kv_cache_automatic_full_prefix_unit_match", result.Audit.CachePolicy)
	require.Equal(t, 5, result.Audit.CacheReadTokens)
	require.Equal(t, 3, result.Audit.CacheWriteTokens)
	require.True(t, result.Audit.CapabilitiesVerified)
	require.True(t, result.Audit.SupportsReasoningMapping)
}

func TestCP6BridgeStreamErrorPassthroughIsSafeAnthropicError(t *testing.T) {
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp6-test-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
		PreferredProtocol:        "responses",
		CapabilitiesVerified:     true,
		SupportsErrorPassthrough: true,
	}
	fixture := ClaudeCodeBridgeProviderFixture{ErrorType: "rate_limit_error", ErrorMessage: "provider throttled request id req_secret req_123456789 api key sk-live-secret at https://api.openai.com/v1/responses"}

	result, err := BuildClaudeCodeBridgeFixtureSSE(decision, []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`), fixture)

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, "event: error")
	require.Contains(t, stream, `"type":"rate_limit_error"`)
	require.Contains(t, stream, "provider throttled")
	require.NotContains(t, stream, "req_secret")
	require.NotContains(t, stream, "req_123456789")
	require.NotContains(t, stream, "sk-live-secret")
	require.NotContains(t, stream, "api.openai.com")
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
}

func TestClaudeCodeBridgeAcceptsParallelToolUseToolName(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"launch agents"}],"tools":[{"name":"multi_tool_use.parallel","description":"parallel tools","input_schema":{"type":"object"}},{"name":"Agent","description":"subagent","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"multi_tool_use.parallel"},"stream":true}`)
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}

	err := validateClaudeCodeBridgeBodyBinding(decision, body)

	require.NoError(t, err)
}

func TestClaudeCodeBridgeSkeletonFailsClosedForParallelAgentTools(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"launch agents"}],"tools":[{"name":"multi_tool_use.parallel","description":"parallel tools","input_schema":{"type":"object"}},{"name":"Agent","description":"subagent","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"multi_tool_use.parallel"},"stream":true}`)
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}

	result, err := BuildClaudeCodeBridgeSkeletonSSE(decision, body)

	require.NoError(t, err)
	stream := string(result.Body)
	require.Contains(t, stream, `"type":"invalid_request_error"`)
	require.Contains(t, stream, "bridge live required")
	require.NotContains(t, stream, "content_block_start")
	require.NotContains(t, stream, `"name":"multi_tool_use.parallel"`)
	require.NotContains(t, stream, `"city":"San Francisco"`)
}

func TestClaudeCodeBridgeAnthropicRewriteAddsStableCacheControlForDeepSeek(t *testing.T) {
	for _, cachePolicy := range []string{
		"provider_prefix_kv_cache_automatic_full_prefix_unit_match",
		"provider_cache_audit_required",
	} {
		t.Run(cachePolicy, func(t *testing.T) {
			body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","system":[{"type":"text","text":"stable project instructions"}],"messages":[{"role":"user","content":[{"type":"text","text":"stable context"}]},{"role":"assistant","content":"ok"},{"role":"user","content":"latest turn"}],"tools":[{"name":"Agent","description":"subagent","input_schema":{"type":"object"}}],"stream":true}`)
			decision := ClaudeCodeBridgeRouteDecision{
				ModelID:                  "claude-code-bridge-deepseek-v4-pro",
				UpstreamModel:            "deepseek-v4-pro",
				Provider:                 "deepseek",
				Route:                    "deepseek_bridge",
				ClientType:               "claude_code_bridge_deepseek",
				FormalPoolAllowed:        false,
				NativeAttestationAllowed: false,
				CredentialScope:          "bridge_pool",
				PreferredProtocol:        "anthropic_messages",
				CachePolicy:              cachePolicy,
			}

			rewritten, err := rewriteClaudeCodeBridgeAnthropicBodyModel(decision, body)

			require.NoError(t, err)
			require.JSONEq(t, `{"type":"ephemeral"}`, gjson.GetBytes(rewritten, "system.0.cache_control").Raw)
			require.JSONEq(t, `{"type":"ephemeral"}`, gjson.GetBytes(rewritten, "messages.0.content.0.cache_control").Raw)
			require.JSONEq(t, `{"type":"ephemeral"}`, gjson.GetBytes(rewritten, "tools.0.cache_control").Raw)
			require.False(t, gjson.GetBytes(rewritten, "messages.2.content.cache_control").Exists(), "latest turn must not become the cache anchor")
			require.Equal(t, "deepseek-v4-pro", gjson.GetBytes(rewritten, "model").String())
		})
	}
}

func TestClaudeCodeBridgeAnthropicRewriteConvertsStableStringContentForDeepSeekCache(t *testing.T) {
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","system":"stable system prefix","messages":[{"role":"user","content":"stable user context"},{"role":"assistant","content":"stable assistant context"},{"role":"user","content":"latest turn must stay a string"}],"stream":true}`)
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "claude-code-bridge-deepseek-v4-pro",
		UpstreamModel:            "deepseek-v4-pro",
		Provider:                 "deepseek",
		Route:                    "deepseek_bridge",
		ClientType:               "claude_code_bridge_deepseek",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
		PreferredProtocol:        "anthropic_messages",
		CachePolicy:              "provider_cache_audit_required",
	}

	rewritten, err := rewriteClaudeCodeBridgeAnthropicBodyModel(decision, body)

	require.NoError(t, err)
	require.Equal(t, "stable system prefix", gjson.GetBytes(rewritten, "system.0.text").String())
	require.JSONEq(t, `{"type":"ephemeral"}`, gjson.GetBytes(rewritten, "system.0.cache_control").Raw)
	require.Equal(t, "stable user context", gjson.GetBytes(rewritten, "messages.0.content.0.text").String())
	require.JSONEq(t, `{"type":"ephemeral"}`, gjson.GetBytes(rewritten, "messages.0.content.0.cache_control").Raw)
	require.Equal(t, "stable assistant context", gjson.GetBytes(rewritten, "messages.1.content.0.text").String())
	require.JSONEq(t, `{"type":"ephemeral"}`, gjson.GetBytes(rewritten, "messages.1.content.0.cache_control").Raw)
	require.Equal(t, "latest turn must stay a string", gjson.GetBytes(rewritten, "messages.2.content").String())
	require.False(t, gjson.GetBytes(rewritten, "messages.2.content.0.cache_control").Exists())
}
