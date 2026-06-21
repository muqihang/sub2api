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

func TestClaudeCodeBridgeRejectsUnsupportedProviderEffort(t *testing.T) {
	tests := []struct {
		name     string
		decision ClaudeCodeBridgeRouteDecision
		body     string
		want     string
	}{
		{
			name:     "deepseek medium rejected",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-deepseek-v4-pro", Provider: "deepseek", Route: "deepseek_bridge", ClientType: "claude_code_bridge_deepseek", CredentialScope: "bridge_pool", ReasoningEffortLevels: []string{"high", "max"}},
			body:     `{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"medium"}}`,
			want:     "unsupported effort",
		},
		{
			name:     "openai max rejected",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-gpt-5.5", Provider: "openai", Route: "openai_bridge", ClientType: "claude_code_bridge_openai", CredentialScope: "bridge_pool", ReasoningEffortLevels: []string{"low", "medium", "high", "xhigh"}},
			body:     `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"max"}}`,
			want:     "unsupported effort",
		},
		{
			name:     "kimi effort rejected",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-kimi-k2.7-code", Provider: "kimi", Route: "kimi_bridge", ClientType: "claude_code_bridge_kimi", CredentialScope: "bridge_pool"},
			body:     `{"model":"claude-code-bridge-kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"high"}}`,
			want:     "does not support effort",
		},
		{
			name:     "deepseek effort without catalog levels rejected",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-deepseek-v4-pro", Provider: "deepseek", Route: "deepseek_bridge", ClientType: "claude_code_bridge_deepseek", CredentialScope: "bridge_pool"},
			body:     `{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"max"}}`,
			want:     "does not support effort",
		},
		{
			name:     "deepseek rejects catalog medium even when advertised",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-deepseek-v4-pro", Provider: "deepseek", Route: "deepseek_bridge", ClientType: "claude_code_bridge_deepseek", CredentialScope: "bridge_pool", ReasoningEffortLevels: []string{"medium", "high", "max"}},
			body:     `{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"medium"}}`,
			want:     "unsupported effort",
		},
		{
			name:     "agnes rejects catalog effort even when advertised",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-agnes-2.0-flash", Provider: "agnes", Route: "agnes_bridge", ClientType: "claude_code_bridge_agnes", CredentialScope: "bridge_pool", ReasoningEffortLevels: []string{"high"}},
			body:     `{"model":"claude-code-bridge-agnes-2.0-flash","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"high"}}`,
			want:     "does not support effort",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildClaudeCodeBridgeSkeletonSSE(tt.decision, []byte(tt.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestClaudeCodeBridgeAcceptsSupportedProviderEffort(t *testing.T) {
	tests := []struct {
		name     string
		decision ClaudeCodeBridgeRouteDecision
		body     string
	}{
		{
			name:     "deepseek max accepted",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-deepseek-v4-pro", Provider: "deepseek", Route: "deepseek_bridge", ClientType: "claude_code_bridge_deepseek", CredentialScope: "bridge_pool", ReasoningEffortLevels: []string{"high", "max"}},
			body:     `{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"max"}}`,
		},
		{
			name:     "openai xhigh accepted",
			decision: ClaudeCodeBridgeRouteDecision{ModelID: "claude-code-bridge-gpt-5.5", Provider: "openai", Route: "openai_bridge", ClientType: "claude_code_bridge_openai", CredentialScope: "bridge_pool", ReasoningEffortLevels: []string{"low", "medium", "high", "xhigh"}},
			body:     `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"xhigh"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildClaudeCodeBridgeSkeletonSSE(tt.decision, []byte(tt.body))
			require.NoError(t, err)
		})
	}
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

func TestClaudeCodeBridgeRejectsUnresolvedDeferredToolSearchShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "tool_reference and defer_loading on tool",
			body: `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Read","tool_reference":{"id":"native-ref"},"defer_loading":true,"input_schema":{"type":"object"}}]}`,
		},
		{
			name: "custom defer_loading on tool",
			body: `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Read","custom":{"defer_loading":true},"input_schema":{"type":"object"}}]}`,
		},
		{
			name: "top level tool_reference",
			body: `{"model":"claude-code-bridge-gpt-5.5","tool_reference":{"id":"top-ref"},"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Read","input_schema":{"type":"object"}}]}`,
		},
		{
			name: "message content tool_reference",
			body: `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":[{"type":"text","text":"hi","tool_reference":{"id":"nested-ref"}}]}],"tools":[{"name":"Read","input_schema":{"type":"object"}}]}`,
		},
		{
			name: "tool_result nested content tool_reference",
			body: `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"ok","tool_reference":{"id":"nested-ref"}}]}]}],"tools":[{"name":"Read","input_schema":{"type":"object"}}]}`,
		},
		{
			name: "system block defer_loading",
			body: `{"model":"claude-code-bridge-gpt-5.5","system":[{"type":"text","text":"stable","defer_loading":true}],"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Read","input_schema":{"type":"object"}}]}`,
		},
		{
			name: "native ToolSearchTool name",
			body: `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"ToolSearchTool","input_schema":{"type":"object"}}]}`,
		},
		{
			name: "native tool_search_tool type",
			body: `{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"native_search","type":"tool_search_tool_regex_20251119","input_schema":{"type":"object"}}]}`,
		},
	}
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:                  "claude-code-bridge-gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp6-toolsearch-gate-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildClaudeCodeBridgeSkeletonSSE(decision, []byte(tt.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), "unresolved deferred tool")
		})
	}
}

func TestClaudeCodeBridgeAllowsToolUseInputBusinessFieldsNamedLikeDeferredMarkers(t *testing.T) {
	body := []byte(`{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"tool_reference":"business-value","defer_loading":false}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}],"tools":[{"name":"lookup","description":"materialized tool","input_schema":{"type":"object","properties":{"tool_reference":{"type":"string"},"defer_loading":{"type":"boolean"}}}}]}`)

	result, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "claude-code-bridge-gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp6-toolsearch-gate-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}, body)

	require.NoError(t, err)
	require.Contains(t, string(result.Body), `"type":"tool_use"`)
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
}

func TestClaudeCodeBridgeAllowsMaterializedToolSchemasWithToolReferenceFieldNames(t *testing.T) {
	body := []byte(`{"model":"claude-code-bridge-gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","description":"materialized tool","input_schema":{"type":"object","properties":{"tool_reference":{"type":"string"},"defer_loading":{"type":"boolean"},"query":{"type":"string"}}}}]}`)

	result, err := BuildClaudeCodeBridgeSkeletonSSE(ClaudeCodeBridgeRouteDecision{
		ModelID:                  "claude-code-bridge-gpt-5.5",
		Provider:                 "openai",
		Route:                    "openai_bridge",
		ClientType:               "claude_code_bridge_openai",
		CatalogVersion:           "cp6-toolsearch-gate-v1",
		FormalPoolAllowed:        false,
		NativeAttestationAllowed: false,
		CredentialScope:          "bridge_pool",
	}, body)

	require.NoError(t, err)
	require.Contains(t, string(result.Body), `"type":"tool_use"`)
	require.False(t, result.Audit.NativeAttested)
	require.False(t, result.Audit.FormalPoolAllowed)
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

func TestClaudeCodeBridgeCacheAuditRowIsProviderTruthfulAndSafe(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY", "local-cache-audit-unit-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID", "cache-test-v1")
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:            "claude-code-bridge-deepseek-v4-pro",
		UpstreamModel:      "deepseek-v4-pro",
		Provider:           "deepseek",
		Route:              "deepseek_bridge",
		ClientType:         "claude_code_bridge_deepseek",
		CredentialScope:    "bridge_pool",
		PreferredProtocol:  "anthropic_messages",
		FallbackProtocol:   "openai_chat_completions",
		SupportsCacheAudit: true,
		CachePolicy:        "provider_prefix_kv_cache_automatic_full_prefix_unit_match",
	}
	body := []byte(`{"model":"claude-code-bridge-deepseek-v4-pro","system":"stable prefix must not leak","messages":[{"role":"user","content":"history must not leak"},{"role":"user","content":"latest turn must not leak"}],"tools":[{"name":"Agent","input_schema":{"type":"object"}}],"stream":true}`)
	requestAudit := buildClaudeCodeBridgeAnthropicRequestAudit(decision, body, "https://api.deepseek.com/anthropic/v1/messages?api_key=must-not-leak")
	summary := buildClaudeCodeBridgeAuditSummaryWithRequest(decision, ClaudeCodeBridgeProviderFixture{CacheReadTokens: 7, CacheMissTokens: 13}, requestAudit)

	row := summary.CacheAuditRow()

	require.Equal(t, "claude-code-bridge-cache-audit-row-v1", row.SchemaVersion)
	require.Equal(t, "deepseek", row.Provider)
	require.Equal(t, "deepseek_bridge", row.Route)
	require.Equal(t, "claude_code_bridge_deepseek", row.ClientType)
	require.Equal(t, "anthropic_messages", row.SelectedProtocol)
	require.Equal(t, "/anthropic/v1/messages", row.UpstreamPathKind)
	require.Equal(t, "deepseek_prefix_kv", row.ProviderCacheMechanism)
	require.True(t, row.CacheControlProviderIgnored)
	require.Equal(t, 7, row.CacheReadTokens)
	require.Equal(t, 13, row.CacheMissTokens)
	require.Regexp(t, `^hmac-sha256:cache-test-v1:[a-f0-9]{64}$`, row.StablePrefixHMAC)
	raw, err := json.Marshal(row)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "stable prefix must not leak")
	require.NotContains(t, string(raw), "history must not leak")
	require.NotContains(t, string(raw), "latest turn must not leak")
	require.NotContains(t, string(raw), "must-not-leak")
}

func TestClaudeCodeBridgeOpenAICacheAuditRowRedactsPromptCacheKey(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY", "local-cache-audit-unit-test-key")
	t.Setenv("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID", "cache-test-v1")
	decision := ClaudeCodeBridgeRouteDecision{
		ModelID:            "gpt-5.5",
		UpstreamModel:      "gpt-5.5",
		Provider:           "openai",
		Route:              "openai_bridge",
		ClientType:         "claude_code_bridge_openai",
		CredentialScope:    "bridge_pool",
		PreferredProtocol:  "responses",
		SupportsCacheAudit: true,
	}
	upstreamBody := []byte(`{"model":"gpt-5.5","prompt_cache_key":"session-cache-key-must-not-leak","input":[{"role":"user","content":[{"type":"input_text","text":"stable prompt must not leak"}]},{"role":"user","content":[{"type":"input_text","text":"latest turn must not leak"}]}],"stream":true}`)
	requestAudit := buildClaudeCodeBridgeOpenAIResponsesRequestAudit(decision, upstreamBody, "https://api.openai.com/v1/responses?prompt_cache_key=must-not-leak")
	summary := buildClaudeCodeBridgeAuditSummaryWithRequest(decision, ClaudeCodeBridgeProviderFixture{CacheReadTokens: 9}, requestAudit)

	row := summary.CacheAuditRow()

	require.Equal(t, "openai", row.Provider)
	require.Equal(t, "openai_bridge", row.Route)
	require.Equal(t, "claude_code_bridge_openai", row.ClientType)
	require.Equal(t, "responses", row.SelectedProtocol)
	require.Equal(t, "/v1/responses", row.UpstreamPathKind)
	require.Equal(t, "openai_prompt_cache", row.ProviderCacheMechanism)
	require.True(t, row.PromptCacheKeyPresent)
	require.Equal(t, "present_redacted", row.PromptCacheKeyStrategy)
	require.Equal(t, []string{"usage.prompt_tokens_details.cached_tokens"}, row.CacheUsageFields)
	require.Equal(t, 9, row.CachedTokens)
	require.Regexp(t, `^hmac-sha256:cache-test-v1:[a-f0-9]{64}$`, row.StablePrefixHMAC)
	raw, err := json.Marshal(row)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "session-cache-key-must-not-leak")
	require.NotContains(t, string(raw), "stable prompt must not leak")
	require.NotContains(t, string(raw), "latest turn must not leak")
	require.NotContains(t, string(raw), "must-not-leak")
}

func TestClaudeCodeBridgeCacheAuditRowFiltersUnsafeCacheControlLocations(t *testing.T) {
	summary := ClaudeCodeBridgeAuditSummary{
		Provider:               "deepseek",
		Route:                  "deepseek_bridge",
		ClientType:             "claude_code_bridge_deepseek",
		SelectedProtocol:       "anthropic_messages",
		ProviderCacheMechanism: "deepseek_prefix_kv",
		UpstreamPathKind:       "/anthropic/v1/messages",
		CacheControlLocations:  []string{"history", "raw prompt body", "Authorization", "prompt_cache_key_location_value", "system"},
	}

	row := summary.CacheAuditRow()
	raw, err := json.Marshal(row)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"history", "system"}, row.CacheControlLocations)
	require.NotContains(t, string(raw), "raw prompt body")
	require.NotContains(t, string(raw), "Authorization")
	require.NotContains(t, string(raw), "prompt_cache_key_location_value")
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
