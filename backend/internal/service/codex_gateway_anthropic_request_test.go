package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildCodexGatewayAnthropicRequest_MapsMessagesToolsThinkingAndCache(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6-thinking",
		"instructions":"You are Codex.",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect repo"}]},
			{"type":"function_call","call_id":"call_1","name":"shell.exec","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		],
		"tools":[{
			"type":"namespace",
			"name":"shell",
			"tools":[{
				"type":"function",
				"name":"exec",
				"description":"run command",
				"parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}
			}]
		}],
		"reasoning":{"effort":"xhigh"},
		"max_output_tokens":128,
		"stream":true
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{
			Slug:                  "claude-opus-4-6-thinking",
			Provider:              "anthropic",
			ProviderVariant:       "antigravity_claude",
			UpstreamModel:         "claude-opus-4-6-thinking",
			UpstreamBaseModel:     "claude-opus-4-6",
			UpstreamThinkingModel: "claude-opus-4-6-thinking",
			DefaultReasoningLevel: "xhigh",
		},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{SessionKey: "session-a", IsolationKey: "isolation-a"},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-6-thinking", gjson.GetBytes(raw, "model").String())
	require.Equal(t, int64(128), gjson.GetBytes(raw, "max_tokens").Int())
	require.True(t, gjson.GetBytes(raw, "stream").Bool())
	require.Equal(t, "ephemeral", gjson.GetBytes(raw, "cache_control.type").String())
	require.Equal(t, "1h", gjson.GetBytes(raw, "cache_control.ttl").String())
	require.Equal(t, "adaptive", gjson.GetBytes(raw, "thinking.type").String())
	require.Equal(t, "max", gjson.GetBytes(raw, "output_config.effort").String())
	require.Equal(t, "You are Codex.", gjson.GetBytes(raw, "system.0.text").String())
	require.Equal(t, "ephemeral", gjson.GetBytes(raw, "system.0.cache_control.type").String())
	require.Equal(t, "1h", gjson.GetBytes(raw, "system.0.cache_control.ttl").String())
	require.Equal(t, "shell__exec", gjson.GetBytes(raw, "tools.0.name").String())
	require.Equal(t, "ephemeral", gjson.GetBytes(raw, "tools.0.cache_control.type").String())
	require.Equal(t, "user", gjson.GetBytes(raw, "messages.0.role").String())
	require.Equal(t, "inspect repo", gjson.GetBytes(raw, "messages.0.content.0.text").String())
	require.Equal(t, "assistant", gjson.GetBytes(raw, "messages.1.role").String())
	require.Equal(t, "tool_use", gjson.GetBytes(raw, "messages.1.content.0.type").String())
	require.Equal(t, "shell__exec", gjson.GetBytes(raw, "messages.1.content.0.name").String())
	require.Equal(t, "user", gjson.GetBytes(raw, "messages.2.role").String())
	require.Equal(t, "tool_result", gjson.GetBytes(raw, "messages.2.content.0.type").String())
	require.Equal(t, "call_1", gjson.GetBytes(raw, "messages.2.content.0.tool_use_id").String())
}

func TestBuildCodexGatewayAnthropicRequest_DisablesThinkingForPlainOpus(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6",
		"input":[{"type":"message","role":"user","content":"hello"}],
		"reasoning":{"effort":"none"}
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{
			Slug:                  "claude-opus-4-6",
			Provider:              "anthropic",
			ProviderVariant:       "kiro_claude",
			UpstreamModel:         "claude-opus-4-6",
			UpstreamBaseModel:     "claude-opus-4-6",
			UpstreamThinkingModel: "claude-opus-4-6-thinking",
			DefaultReasoningLevel: "high",
		},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "disabled", gjson.GetBytes(raw, "thinking.type").String())
	require.False(t, gjson.GetBytes(raw, "output_config.effort").Exists())
}

func TestBuildCodexGatewayAnthropicRequest_UsesBaseModelForHighReasoning(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6",
		"input":[{"type":"message","role":"user","content":"hello"}],
		"reasoning":{"effort":"high"}
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{
			Slug:                  "claude-opus-4-6",
			Provider:              "anthropic",
			ProviderVariant:       "kiro_claude",
			UpstreamModel:         "claude-opus-4-6",
			UpstreamBaseModel:     "claude-opus-4-6",
			UpstreamThinkingModel: "claude-opus-4-6-thinking",
			DefaultReasoningLevel: "high",
		},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-6", gjson.GetBytes(raw, "model").String())
	require.Equal(t, "disabled", gjson.GetBytes(raw, "thinking.type").String())
	require.False(t, gjson.GetBytes(raw, "output_config.effort").Exists())
}

func TestBuildCodexGatewayAnthropicRequest_KeepsPlainModelForXHighReasoning(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6",
		"input":[{"type":"message","role":"user","content":"hello"}],
		"reasoning":{"effort":"xhigh"}
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{
			Slug:                  "claude-opus-4-6",
			Provider:              "anthropic",
			ProviderVariant:       "kiro_claude",
			UpstreamModel:         "claude-opus-4-6",
			UpstreamBaseModel:     "claude-opus-4-6",
			UpstreamThinkingModel: "claude-opus-4-6-thinking",
			DefaultReasoningLevel: "high",
		},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-6", gjson.GetBytes(raw, "model").String())
	require.Equal(t, "disabled", gjson.GetBytes(raw, "thinking.type").String())
	require.False(t, gjson.GetBytes(raw, "output_config.effort").Exists())
}

func TestBuildCodexGatewayAnthropicRequest_ThinkingVariantSupportsLowHighAndXHigh(t *testing.T) {
	tests := []struct {
		name         string
		effort       string
		wantUpstream string
		wantEffort   string
	}{
		{name: "low", effort: "low", wantUpstream: "claude-opus-4-7-thinking", wantEffort: "low"},
		{name: "high", effort: "high", wantUpstream: "claude-opus-4-7-thinking", wantEffort: "high"},
		{name: "xhigh", effort: "xhigh", wantUpstream: "claude-opus-4-7-thinking", wantEffort: "max"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
				"model":"claude-opus-4-7-thinking",
				"input":[{"type":"message","role":"user","content":"hello"}],
				"reasoning":{"effort":"` + tt.effort + `"}
			}`))
			require.NoError(t, err)

			prepared, err := BuildCodexGatewayAnthropicRequest(
				CodexGatewayModel{
					Slug:                  "claude-opus-4-7-thinking",
					Provider:              "anthropic",
					ProviderVariant:       "kiro_claude_thinking",
					UpstreamModel:         "claude-opus-4-7-thinking",
					UpstreamBaseModel:     "claude-opus-4-7-thinking",
					UpstreamThinkingModel: "claude-opus-4-7-thinking",
					DefaultReasoningLevel: "high",
				},
				req,
				nil,
				CodexGatewayAnthropicRequestContext{},
				CodexGatewayAnthropicRequestConfig{},
			)
			require.NoError(t, err)

			raw, err := json.Marshal(prepared.Body)
			require.NoError(t, err)
			require.Equal(t, tt.wantUpstream, gjson.GetBytes(raw, "model").String())
			require.Equal(t, "adaptive", gjson.GetBytes(raw, "thinking.type").String())
			require.Equal(t, tt.wantEffort, gjson.GetBytes(raw, "output_config.effort").String())
		})
	}
}

func TestBuildCodexGatewayAnthropicRequest_ForwardsHostedWebSearchAsServerHandledFunctionTool(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7-thinking",
		"input":[{"type":"message","role":"user","content":"search the web"}],
		"tools":[{"type":"web_search"}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-7-thinking"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "web_search", gjson.GetBytes(raw, "tools.0.name").String())
	require.Equal(t, "object", gjson.GetBytes(raw, "tools.0.input_schema.type").String())
	require.NotContains(t, string(raw), "OpenAI hosted tools are not available")
	require.NotContains(t, string(raw), "web_search_20250305")
}

func TestBuildCodexGatewayAnthropicRequest_ForwardsMixedHostedWebSearchAsServerHandledFunctionTool(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7-thinking",
		"input":[{"type":"message","role":"user","content":"use tools"}],
		"tools":[
			{"type":"web_search"},
			{"type":"computer_use_preview"},
			{"type":"file_search"},
			{"type":"image_generation"}
		]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-7-thinking"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "web_search", gjson.GetBytes(raw, "tools.0.name").String())
	require.Equal(t, "computer_use_preview", gjson.GetBytes(raw, "tools.1.name").String())
	require.Equal(t, "file_search", gjson.GetBytes(raw, "tools.2.name").String())
	require.Equal(t, "image_generation", gjson.GetBytes(raw, "tools.3.name").String())
	require.Equal(t, "object", gjson.GetBytes(raw, "tools.0.input_schema.type").String())
	require.NotContains(t, string(raw), "OpenAI hosted tools are not available")
	require.NotContains(t, string(raw), "web_search_20250305")
}

func TestBuildCodexGatewayAnthropicRequest_NormalizesInvalidFunctionCallArgumentsBeforeMarshaling(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7-thinking",
		"input":[{"type":"function_call","call_id":"call_1","name":"shell__exec","arguments":"*"}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-7-thinking"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"call_1"`)
	require.NotContains(t, string(raw), `"arguments":"*"`)
}

func TestBuildCodexGatewayAnthropicRequest_PreservesThinkingForLargeToolReplay(t *testing.T) {
	largeOutput := strings.Repeat("Forward branch line\n", 12000)
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6-thinking",
		"input":[
			{"type":"message","role":"user","content":"inspect gateway"},
			{"type":"function_call","call_id":"call_1","name":"shell.exec","arguments":"{\"cmd\":\"sed -n '3988,4200p' gateway_service.go\"}"},
			{"type":"function_call_output","call_id":"call_1","output":` + strconv.Quote(largeOutput) + `}
		],
		"reasoning":{"effort":"xhigh"},
		"tools":[{
			"type":"namespace",
			"name":"shell",
			"tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]
		}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "adaptive", gjson.GetBytes(raw, "thinking.type").String())
	require.Equal(t, "max", gjson.GetBytes(raw, "output_config.effort").String())
}

func TestBuildCodexGatewayAnthropicRequest_DisablesThinkingForForcedToolChoice(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6-thinking",
		"input":[{"type":"message","role":"user","content":"run pwd"}],
		"tools":[{
			"type":"namespace",
			"name":"shell",
			"tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]
		}],
		"tool_choice":{"type":"function","name":"shell.exec"},
		"reasoning":{"effort":"xhigh"}
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "tool", gjson.GetBytes(raw, "tool_choice.type").String())
	require.Equal(t, "disabled", gjson.GetBytes(raw, "thinking.type").String())
	require.False(t, gjson.GetBytes(raw, "output_config.effort").Exists())
}

func TestBuildCodexGatewayAnthropicRequest_NormalizesLegacyToolChoiceNames(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6-thinking",
		"input":[{"type":"message","role":"user","content":"edit files"}],
		"tools":[
			{
				"type":"custom",
				"name":"edit",
				"description":"edit files",
				"format":{"type":"grammar","syntax":"lark","definition":"start: value\nvalue: /.+/"}
			},
			{
				"type":"function",
				"name":"todowrite",
				"parameters":{"type":"object"}
			},
			{
				"type":"function",
				"name":"read",
				"parameters":{"type":"object"}
			},
			{
				"type":"function",
				"name":"write",
				"parameters":{"type":"object"}
			},
			{
				"type":"function",
				"name":"bash",
				"parameters":{"type":"object"}
			}
		],
		"tool_choice":"apply_patch",
		"reasoning":{"effort":"xhigh"}
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "tool", gjson.GetBytes(raw, "tool_choice.type").String())
	require.Equal(t, "custom__edit", gjson.GetBytes(raw, "tool_choice.name").String())
	require.Equal(t, "disabled", gjson.GetBytes(raw, "thinking.type").String())

	cases := []struct {
		name       string
		toolChoice string
		wantAlias  string
	}{
		{name: "update_plan", toolChoice: "update_plan", wantAlias: "todowrite"},
		{name: "read_file", toolChoice: "read_file", wantAlias: "read"},
		{name: "write_file", toolChoice: "write_file", wantAlias: "write"},
		{name: "execute_bash", toolChoice: "execute_bash", wantAlias: "bash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
				"model":"claude-opus-4-6-thinking",
				"input":[{"type":"message","role":"user","content":"legacy tool choice"}],
				"tools":[
					{"type":"function","name":"todowrite","parameters":{"type":"object"}},
					{"type":"function","name":"read","parameters":{"type":"object"}},
					{"type":"function","name":"write","parameters":{"type":"object"}},
					{"type":"function","name":"bash","parameters":{"type":"object"}}
				],
				"tool_choice":"` + tc.toolChoice + `"
			}`))
			require.NoError(t, err)

			prepared, err := BuildCodexGatewayAnthropicRequest(
				CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
				req,
				nil,
				CodexGatewayAnthropicRequestContext{},
				CodexGatewayAnthropicRequestConfig{},
			)
			require.NoError(t, err)

			raw, err := json.Marshal(prepared.Body)
			require.NoError(t, err)
			require.Equal(t, tc.wantAlias, gjson.GetBytes(raw, "tool_choice.name").String())
		})
	}

	t.Run("object_tool_choice_and_nested_function_name", func(t *testing.T) {
		req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
			"model":"claude-opus-4-6-thinking",
			"input":[{"type":"message","role":"user","content":"legacy object tool choice"}],
			"tools":[
				{
					"type":"custom",
					"name":"edit",
					"description":"edit files",
					"format":{"type":"grammar","syntax":"lark","definition":"start: value\nvalue: /.+/"}
				}
			],
			"tool_choice":{"type":"custom","function":{"name":"apply_patch"}}
		}`))
		require.NoError(t, err)

		prepared, err := BuildCodexGatewayAnthropicRequest(
			CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
			req,
			nil,
			CodexGatewayAnthropicRequestContext{},
			CodexGatewayAnthropicRequestConfig{},
		)
		require.NoError(t, err)

		raw, err := json.Marshal(prepared.Body)
		require.NoError(t, err)
		require.Equal(t, "tool", gjson.GetBytes(raw, "tool_choice.type").String())
		require.Equal(t, "custom__edit", gjson.GetBytes(raw, "tool_choice.name").String())
	})
}

func TestCodexGatewayAnthropicMapErrorBody_SanitizesCloudflareHTML(t *testing.T) {
	raw := []byte(`<!DOCTYPE html><html><head><title>zivv.pro | 524: A timeout occurred</title></head><body>Error code 524</body></html>`)
	body := codexGatewayAnthropicMapErrorBody(524, raw)

	require.Equal(t, CodexGatewayErrorTypeAPI, gjson.GetBytes(body, "error.type").String())
	require.Equal(t, "upstream_timeout", gjson.GetBytes(body, "error.code").String())
	require.Contains(t, gjson.GetBytes(body, "error.message").String(), "Cloudflare 524 timeout")
	require.NotContains(t, string(body), "<!DOCTYPE html>")
}

func TestBuildCodexGatewayAnthropicRequest_ReplaysPreviousToolUseBeforeToolResult(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_prev",
			SessionKey:    "session-a",
			IsolationKey:  "isolation-a",
			Provider:      "anthropic",
			UpstreamModel: "claude-sonnet-4-6",
		},
		AssistantContent:        "I will call the tool.",
		AssistantContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:        "toolu_1",
			Alias:     "get_weather",
			Name:      "get_weather",
			Arguments: `{"city":"Shanghai"}`,
		}},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"get_weather": {Alias: "get_weather", Kind: CodexGatewayToolKindFunction, Name: "get_weather"},
		},
	}))
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-sonnet-4-6",
		"previous_response_id":"resp_prev",
		"input":[{"type":"function_call_output","call_id":"toolu_1","output":"Shanghai is cloudy."}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-sonnet-4-6", Provider: "anthropic", UpstreamModel: "claude-sonnet-4-6"},
		req,
		store,
		CodexGatewayAnthropicRequestContext{SessionKey: "session-a", IsolationKey: "isolation-a"},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "assistant", gjson.GetBytes(raw, "messages.0.role").String())
	require.Equal(t, "text", gjson.GetBytes(raw, "messages.0.content.0.type").String())
	require.Equal(t, "tool_use", gjson.GetBytes(raw, "messages.0.content.1.type").String())
	require.Equal(t, "toolu_1", gjson.GetBytes(raw, "messages.0.content.1.id").String())
	require.Equal(t, "get_weather", gjson.GetBytes(raw, "messages.0.content.1.name").String())
	require.Equal(t, "Shanghai", gjson.GetBytes(raw, "messages.0.content.1.input.city").String())
	require.Equal(t, "user", gjson.GetBytes(raw, "messages.1.role").String())
	require.Equal(t, "tool_result", gjson.GetBytes(raw, "messages.1.content.0.type").String())
	require.Equal(t, "toolu_1", gjson.GetBytes(raw, "messages.1.content.0.tool_use_id").String())
}

func TestBuildCodexGatewayAnthropicRequest_DropsStaleToolChoiceWhenReplayRequestHasNoTools(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_prev",
			SessionKey:    "session-a",
			IsolationKey:  "isolation-a",
			Provider:      "anthropic",
			UpstreamModel: "claude-opus-4-7-thinking",
		},
		AssistantContent:        "I will edit the file.",
		AssistantContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:        "toolu_1",
			Type:      CodexGatewayToolKindCustom,
			Alias:     "custom__edit",
			Name:      "edit",
			Arguments: `{"input":"*** Begin Patch\n*** End Patch\n"}`,
		}},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"custom__edit": {Alias: "custom__edit", Kind: CodexGatewayToolKindCustom, Name: "edit"},
		},
	}))
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7-thinking",
		"previous_response_id":"resp_prev",
		"tool_choice":"edit",
		"input":[{"type":"function_call_output","call_id":"toolu_1","output":"patch applied"}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-7-thinking"},
		req,
		store,
		CodexGatewayAnthropicRequestContext{SessionKey: "session-a", IsolationKey: "isolation-a"},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "tool_choice").Exists())
	require.Equal(t, "custom__edit", gjson.GetBytes(raw, "messages.0.content.1.name").String())
}

func TestBuildCodexGatewayAnthropicRequest_ReplaysLegacyCustomToolCallWithoutCurrentTools(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7-thinking",
		"tool_choice":"edit",
		"input":[
			{"type":"custom_tool_call","call_id":"toolu_legacy","name":"edit","input":"*** Begin Patch\n*** End Patch\n"},
			{"type":"custom_tool_call_output","call_id":"toolu_legacy","output":"patch applied"}
		]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-7-thinking"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "tool_choice").Exists())
	require.Equal(t, "assistant", gjson.GetBytes(raw, "messages.0.role").String())
	require.Equal(t, "tool_use", gjson.GetBytes(raw, "messages.0.content.0.type").String())
	require.Equal(t, "custom__edit", gjson.GetBytes(raw, "messages.0.content.0.name").String())
	require.Equal(t, "user", gjson.GetBytes(raw, "messages.1.role").String())
	require.Equal(t, "tool_result", gjson.GetBytes(raw, "messages.1.content.0.type").String())
}
