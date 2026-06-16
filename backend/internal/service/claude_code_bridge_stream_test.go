package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
		{name: "tool name dot not Anthropic compatible", body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"unsafe.tool","input_schema":{"type":"object"}}]}`, want: "tool shape"},
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
