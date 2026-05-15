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
		CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
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
		CodexGatewayModel{Slug: "claude-opus-4-6", Provider: "anthropic", UpstreamModel: "claude-opus-4-6"},
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
