package service

import (
	"encoding/json"
	"fmt"
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

func TestBuildCodexGatewayAnthropicRequest_DirectClaudeMapsHighAndXHighToSeparateUpstreams(t *testing.T) {
	tests := []struct {
		name       string
		effort     string
		wantModel  string
		wantEffort string
	}{
		{name: "high uses base upstream", effort: "high", wantModel: "claude-opus-4-8", wantEffort: "high"},
		{name: "xhigh uses thinking upstream", effort: "xhigh", wantModel: "claude-opus-4-8-thinking", wantEffort: "max"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
				"model":"claude-opus-4-8",
				"input":[{"type":"message","role":"user","content":"hello"}],
				"reasoning":{"effort":"` + tt.effort + `"}
			}`))
			require.NoError(t, err)

			prepared, err := BuildCodexGatewayAnthropicRequest(
				CodexGatewayModel{
					Slug:                     "claude-opus-4-8",
					Provider:                 "anthropic",
					ProviderVariant:          "anthropic_direct",
					UpstreamModel:            "claude-opus-4-8",
					UpstreamBaseModel:        "claude-opus-4-8",
					UpstreamThinkingModel:    "claude-opus-4-8-thinking",
					DefaultReasoningLevel:    "high",
					SupportedReasoningLevels: []string{"low", "high", "xhigh"},
				},
				req,
				nil,
				CodexGatewayAnthropicRequestContext{},
				CodexGatewayAnthropicRequestConfig{},
			)
			require.NoError(t, err)

			raw, err := json.Marshal(prepared.Body)
			require.NoError(t, err)
			require.Equal(t, tt.wantModel, gjson.GetBytes(raw, "model").String())
			require.Equal(t, "adaptive", gjson.GetBytes(raw, "thinking.type").String())
			require.Equal(t, tt.wantEffort, gjson.GetBytes(raw, "output_config.effort").String())
		})
	}
}

func TestBuildCodexGatewayAnthropicRequest_MapsDeferredToolFamilyMatrixFromToolSearchOutput(t *testing.T) {
	deferredTools := []any{
		map[string]any{
			"type": "namespace",
			"name": "browser",
			"tools": []any{map[string]any{
				"type":        "function",
				"name":        "navigate",
				"description": "open a local browser target",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"url": map[string]any{"type": "string"}},
					"required":   []any{"url"},
				},
			}},
		},
		map[string]any{
			"type": "namespace",
			"name": "computer_use",
			"tools": []any{map[string]any{
				"type":        "function",
				"name":        "list_apps",
				"description": "list controllable local apps",
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
			}},
		},
		map[string]any{
			"type": "namespace",
			"name": "documents",
			"tools": []any{map[string]any{
				"type":   "custom",
				"name":   "redline",
				"custom": map[string]any{"format": map[string]any{"type": "text"}},
			}},
		},
		map[string]any{
			"type": "namespace",
			"name": "multi_agent_v1",
			"tools": []any{map[string]any{
				"type": "function",
				"name": "spawn_agent",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"task": map[string]any{"type": "string"}, "model": map[string]any{"type": "string"}},
					"required":   []any{"task"},
				},
			}},
		},
	}
	deferredToolsJSON := string(mustMarshalRawMessage(t, deferredTools))
	req := CodexGatewayResponsesCreateRequest{
		Model: "claude-opus-4-7",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"classify deferred tools"}]},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"browser computer use documents subagent","limit":20}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":` + deferredToolsJSON + `},
			{"type":"function_call","call_id":"call_browser","name":"navigate","namespace":"browser","arguments":"{\"url\":\"http://localhost:3000\"}"},
			{"type":"custom_tool_call","call_id":"call_doc","name":"redline","namespace":"documents","input":"mark this change"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
		]`),
	}

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7", Provider: "anthropic", UpstreamModel: "claude-opus-4-7"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	tools := prepared.Body["tools"].([]any)
	for _, alias := range []string{"browser__navigate", "computer_use__list_apps", "documents__custom__redline", "multi_agent_v1__spawn_agent", "tool_search"} {
		require.NotNil(t, anthropicRequestToolByName(t, tools, alias), "missing Anthropic tool alias %s", alias)
	}

	browserEntry := prepared.ToolNameMap["browser__navigate"]
	require.Equal(t, CodexGatewayToolKindNamespace, browserEntry.Kind)
	require.Equal(t, "browser", browserEntry.NamespacePath)
	require.Equal(t, "navigate", codexGatewayClientVisibleToolName(browserEntry))
	docEntry := prepared.ToolNameMap["documents__custom__redline"]
	require.Equal(t, CodexGatewayToolKindCustom, docEntry.Kind)
	require.Equal(t, "documents", docEntry.NamespacePath)
	require.Equal(t, "redline", codexGatewayClientVisibleToolName(docEntry))

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "browser__navigate", gjson.GetBytes(raw, "messages.3.content.0.name").String())
	require.Equal(t, "documents__custom__redline", gjson.GetBytes(raw, "messages.3.content.1.name").String())
}

func TestBuildCodexGatewayAnthropicRequest_ConvertsNativeToolSearchReplay(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7",
		"input":[
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"spawn_agent","limit":10}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":[
				{"type":"namespace","name":"multi_agent_v1","tools":[
					{"type":"function","name":"spawn_agent","parameters":{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}}
				]}
			]},
			{"type":"function_call","call_id":"call_spawn","name":"spawn_agent","arguments":"{\"message\":\"status\"}"}
		],
		"tools":[{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7", Provider: "anthropic", UpstreamModel: "claude-opus-4-7"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "assistant", gjson.GetBytes(raw, "messages.0.role").String())
	require.Equal(t, "tool_search", gjson.GetBytes(raw, "messages.0.content.0.name").String())
	require.Equal(t, "call_search", gjson.GetBytes(raw, "messages.0.content.0.id").String())
	require.JSONEq(t, `{"query":"spawn_agent","limit":10}`, gjson.GetBytes(raw, "messages.0.content.0.input").Raw)
	require.Equal(t, "user", gjson.GetBytes(raw, "messages.1.role").String())
	require.Equal(t, "tool_result", gjson.GetBytes(raw, "messages.1.content.0.type").String())
	require.Equal(t, "call_search", gjson.GetBytes(raw, "messages.1.content.0.tool_use_id").String())
	require.Contains(t, gjson.GetBytes(raw, "messages.1.content.0.content").String(), "multi_agent_v1")
	require.Equal(t, "assistant", gjson.GetBytes(raw, "messages.2.role").String())
	require.Equal(t, "multi_agent_v1__spawn_agent", gjson.GetBytes(raw, "messages.2.content.0.name").String())
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
	require.Contains(t, anthropicToolNames(raw), "web_search")
	require.Contains(t, anthropicToolNames(raw), "computer_use_preview")
	require.Contains(t, anthropicToolNames(raw), "file_search")
	require.Contains(t, anthropicToolNames(raw), "image_generation")
	require.Equal(t, "object", anthropicToolByName(raw, "web_search").Get("input_schema.type").String())
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

func TestBuildCodexGatewayAnthropicRequest_AcceptsLocalShellCallInput(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7-thinking",
		"input":[{"type":"local_shell_call","call_id":"call_1","name":"shell__exec","action":{"type":"exec","command":["zsh","-lc","pwd"]}}]
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
	require.Equal(t, "tool_use", gjson.GetBytes(raw, "messages.0.content.0.type").String())
	require.Equal(t, "shell__exec", gjson.GetBytes(raw, "messages.0.content.0.name").String())
	require.Equal(t, "pwd", gjson.GetBytes(raw, "messages.0.content.0.input.cmd").String())
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

func TestBuildCodexGatewayAnthropicRequest_SummarizesLargeComputerUseToolOutput(t *testing.T) {
	largeAXTree := "Computer Use state (CUA App Version: 799)\n<app_state>\nApp=/Applications/Doubao.app\n" +
		strings.Repeat("0 标准窗口 豆包, Second Sidebar Group\n1 按钮 历史会话\n2 静态文本 推荐内容\n", 900) +
		"188 文本输入区 Ask anything enabled=true focused=true element_index=188\n" +
		"189 按钮 发送 enabled=true element_index=189\n" +
		"190 静态文本 胡辣汤、烩面、黄河鲤鱼都是郑州特色\n</app_state>"
	rawOutput, err := json.Marshal([]any{
		map[string]any{"type": "input_text", "text": "Wall time: 8.7346 seconds\nOutput:"},
		map[string]any{"type": "input_text", "text": largeAXTree},
		map[string]any{"type": "image_url", "image_url": "data:image/png;base64," + strings.Repeat("A", 40000)},
	})
	require.NoError(t, err)
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-8",
		"input":[
			{"type":"message","role":"user","content":"use Doubao"},
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"/Applications/Doubao.app\"}"},
			{"type":"function_call_output","call_id":"call_state","output":` + string(rawOutput) + `}
		],
		"tools":[{"type":"function","name":"mcp__computer_use__get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-8", Provider: "anthropic", UpstreamModel: "claude-opus-4-8"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	content := gjson.GetBytes(raw, "messages.2.content.0.content").String()
	require.Less(t, len(content), 12000)
	require.Contains(t, content, "accessibility_tree")
	require.Contains(t, content, "operable_lines")
	require.Contains(t, content, "188 文本输入区")
	require.Contains(t, content, "189 按钮")
	require.Contains(t, content, "binary_or_image")
	require.NotContains(t, content, "Second Sidebar Group")
	require.NotContains(t, content, strings.Repeat("A", 1024))
}

func TestBuildCodexGatewayAnthropicRequest_ComputerUseHighFidelityVisibleTextBudget(t *testing.T) {
	facts := []string{
		"合记烩面：国营老牌，汤鲜面厚，人均 22-30 元。",
		"萧记三鲜烩面：鸡汤骨汤羊肉汤，加海参鱿鱼。",
		"方中山胡辣汤：麻香够味，外地人建议微辣。",
		"京都老蔡记：老三记之首，蒸饺和馄饨适合早餐。",
		"葛记焖饼：百年老店，焖饼配绿豆沙解腻。",
		"阿五黄河大鲤鱼：红烧黄河大鲤鱼必点。",
		"二合馆：1912 年始创，清汤酸辣乌鱼蛋汤是豫菜名羹。",
		"谷雨春：黄河鲤鱼现做，配烩面更圆满。",
		"郑记粗粮人家：烙馍和小米粥免费无限续。",
		"巴奴毛肚火锅：毛肚脆嫩，菌汤锅底适合朋友小聚。",
		"马豫兴桶子鸡：皮脆肉嫩，适合打包伴手礼。",
		"梅园开封灌汤包：皮薄馅足，咬开爆汁。",
	}
	var app strings.Builder
	app.WriteString("Computer Use state (CUA App Version: 799)\n<app_state>\n")
	app.WriteString("App=/Applications/Doubao.app/ (bundleID com.bot.pc.doubao, pid 750)\n")
	app.WriteString(strings.Repeat("\t30 link Description: 历史对话, Value: chrome://doubao-chat/chat/sidebar\n", 60))
	for i, fact := range facts {
		fmt.Fprintf(&app, "\t%d text %s\n", 100+i, fact)
	}
	app.WriteString("\t501 文本输入区 (settable, string) 发消息...\n\t502 按钮 发送\n</app_state>")
	rawOutput, err := json.Marshal([]any{
		map[string]any{"type": "input_text", "text": "Wall time: 0.8 seconds\nOutput:"},
		map[string]any{"type": "input_text", "text": app.String()},
		map[string]any{"type": "image_url", "image_url": "data:image/png;base64," + strings.Repeat("A", 180000)},
	})
	require.NoError(t, err)
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-8",
		"input":[
			{"type":"message","role":"user","content":"use Doubao"},
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"com.bot.pc.doubao\"}"},
			{"type":"function_call_output","call_id":"call_state","output":` + string(rawOutput) + `}
		],
		"tools":[{"type":"function","name":"mcp__computer_use__get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-8", Provider: "anthropic", UpstreamModel: "claude-opus-4-8"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	content := gjson.GetBytes(raw, "messages.2.content.0.content").String()
	visibleText := gjson.Get(content, "1.text.visible_text").Raw
	if visibleText == "" {
		visibleText = gjson.Get(content, "0.text.visible_text").Raw
	}
	for _, want := range facts {
		require.Contains(t, visibleText, strings.Split(want, "：")[0], "Claude visible_text should preserve broad answer coverage")
	}
	require.Contains(t, gjson.Get(content, "1.text.operable_lines").Raw+gjson.Get(content, "0.text.operable_lines").Raw, "文本输入区")
	require.NotContains(t, content, strings.Repeat("A", 128))
	require.LessOrEqual(t, len(content), codexGatewayDeepSeekToolOutputMaxChars+512)
}

func TestCodexGatewayAnthropicStateReplayMessages_DoesNotAccumulateComputerUseToolResults(t *testing.T) {
	largeAXTree := "Computer Use state (CUA App Version: 799)\n<app_state>\n" +
		strings.Repeat("0 标准窗口 豆包, Second Sidebar Group\n1 按钮 历史会话\n", 1200) +
		"188 文本输入区 Ask anything enabled=true element_index=188\n" +
		"189 按钮 发送 enabled=true element_index=189\n</app_state>"
	base := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"Use Doubao with Computer Use and answer the weather question.` + strings.Repeat(" keep-context", 800) + `"}]}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"tool_use","id":"call_state","name":"mcp__computer_use__get_app_state","input":{"app":"/Applications/Doubao.app"}}]}`),
		json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_state","content":` + strconv.Quote(largeAXTree+"\nimage_url=data:image/png;base64,"+strings.Repeat("A", 40000)) + `}]}`),
	}
	replay := codexGatewayAnthropicStateReplayMessages(base, CodexGatewayResponseState{
		AssistantContentPresent: true,
		AssistantContent:        "Need to type into Doubao.",
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:        "call_set",
			Type:      CodexGatewayToolKindNamespace,
			Alias:     "mcp__computer_use__set_value",
			Name:      "set_value",
			Arguments: `{"app":"com.bot.pc.doubao","element_index":"188","value":"明天郑州天气怎么样啊"}`,
		}},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"mcp__computer_use__set_value": {Alias: "mcp__computer_use__set_value", Kind: CodexGatewayToolKindNamespace, NamespacePath: "mcp__computer_use__", Name: "set_value"},
		},
	})

	require.Len(t, replay, 2)
	require.Contains(t, string(replay[0]), `"role":"user"`)
	require.Contains(t, string(replay[0]), "Use Doubao with Computer Use")
	require.Less(t, len(replay[0]), 3000)
	require.Contains(t, string(replay[1]), `"type":"tool_use"`)
	require.Contains(t, string(replay[1]), "mcp__computer_use__set_value")
	rawReplay, err := json.Marshal(replay)
	require.NoError(t, err)
	require.NotContains(t, string(rawReplay), "Second Sidebar Group")
	require.NotContains(t, string(rawReplay), strings.Repeat("A", 1024))
}

func TestBuildCodexGatewayAnthropicRequest_ComputerUseAddsNativeOperationStrategy(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-8",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Use Doubao with Computer Use."}]}],
		"tools":[{"type":"namespace","name":"mcp__computer_use__","tools":[
			{"type":"function","name":"list_apps","parameters":{"type":"object","properties":{}}},
			{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}},
			{"type":"function","name":"set_value","parameters":{"type":"object","properties":{"app":{"type":"string"},"element_index":{"type":"string"},"value":{"type":"string"}},"required":["app","element_index","value"]}},
			{"type":"function","name":"press_key","parameters":{"type":"object","properties":{"app":{"type":"string"},"key":{"type":"string"}},"required":["app","key"]}}
		]}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-8", Provider: "anthropic", ProviderVariant: "anthropic_direct", UpstreamModel: "claude-opus-4-8"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	strategy := gjson.GetBytes(raw, "system.#.text").Array()
	joined := ""
	for _, item := range strategy {
		joined += item.String() + "\n"
	}
	require.Contains(t, joined, "Computer Use strategy")
	require.Contains(t, joined, "prefer bundle identifier")
	require.Contains(t, joined, "set_value")
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

func TestBuildCodexGatewayAnthropicRequest_StructuredFunctionOutputArrayInputImageUsesDeterministicFallback(t *testing.T) {
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-8",
		"input":[
			{"type":"function_call","call_id":"call_img","name":"capture_screen","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_img","output":[
				{"type":"input_text","text":"screenshot follows"},
				{"type":"input_image","image_url":"data:image/png;base64,QUJDRA==","detail":"high"}
			]}
		],
		"tools":[{"type":"function","name":"capture_screen","parameters":{"type":"object"}}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-8", Provider: "anthropic", UpstreamModel: "claude-opus-4-8"},
		req,
		nil,
		CodexGatewayAnthropicRequestContext{},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	content := gjson.GetBytes(raw, "messages.1.content.0.content").String()
	require.Contains(t, content, "screenshot follows")
	require.Contains(t, content, "binary_or_image")
	require.Contains(t, content, "sha256")
	require.NotContains(t, content, "QUJDRA==")
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

func TestBuildCodexGatewayAnthropicRequest_ReplaysPreviousToolSearchBeforeToolSearchOutput(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{})
	deferredTools := []any{map[string]any{
		"type": "namespace",
		"name": "multi_agent_v1",
		"tools": []any{map[string]any{
			"type":       "function",
			"name":       "spawn_agent",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
		}},
	}}
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_prev_search",
			SessionKey:    "session-a",
			IsolationKey:  "isolation-a",
			Provider:      "anthropic",
			UpstreamModel: "claude-opus-4-7",
		},
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:        "call_search",
			Type:      CodexGatewayToolKindFunction,
			Alias:     "tool_search",
			Name:      "tool_search",
			Arguments: `{"query":"spawn_agent","limit":10}`,
		}},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"tool_search": {Alias: "tool_search", Kind: CodexGatewayToolKindFunction, Name: "tool_search"},
		},
	}))
	deferredToolsJSON := string(mustMarshalRawMessage(t, deferredTools))
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7",
		"previous_response_id":"resp_prev_search",
		"input":[
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":` + deferredToolsJSON + `}
		],
		"tools":[{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7", Provider: "anthropic", ProviderVariant: "anthropic_direct", UpstreamModel: "claude-opus-4-7"},
		req,
		store,
		CodexGatewayAnthropicRequestContext{SessionKey: "session-a", IsolationKey: "isolation-a"},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "tool_search", gjson.GetBytes(raw, "messages.0.content.0.name").String())
	require.Equal(t, "tool_result", gjson.GetBytes(raw, "messages.1.content.0.type").String())
	require.Contains(t, gjson.GetBytes(raw, "messages.1.content.0.content").String(), "multi_agent_v1")
	require.Contains(t, prepared.ToolNameMap, "multi_agent_v1__spawn_agent")
}

func TestBuildCodexGatewayAnthropicRequest_ReplaysPreviousLocalShellBeforeLocalShellOutput(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_prev_shell",
			SessionKey:    "session-a",
			IsolationKey:  "isolation-a",
			Provider:      "anthropic",
			UpstreamModel: "claude-opus-4-7",
		},
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:        "call_shell",
			Type:      CodexGatewayToolKindFunction,
			Alias:     "shell__exec",
			Name:      "exec",
			Arguments: `{"cmd":"pwd"}`,
		}},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"shell__exec": {Alias: "shell__exec", Kind: CodexGatewayToolKindFunction, Namespace: "shell", NamespacePath: "shell", Name: "exec"},
		},
	}))
	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-7",
		"previous_response_id":"resp_prev_shell",
		"input":[{"type":"local_shell_call_output","call_id":"call_shell","output":"/tmp/repo"}]
	}`))
	require.NoError(t, err)

	prepared, err := BuildCodexGatewayAnthropicRequest(
		CodexGatewayModel{Slug: "claude-opus-4-7", Provider: "anthropic", ProviderVariant: "anthropic_direct", UpstreamModel: "claude-opus-4-7"},
		req,
		store,
		CodexGatewayAnthropicRequestContext{SessionKey: "session-a", IsolationKey: "isolation-a"},
		CodexGatewayAnthropicRequestConfig{},
	)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "shell__exec", gjson.GetBytes(raw, "messages.0.content.0.name").String())
	require.Equal(t, "tool_result", gjson.GetBytes(raw, "messages.1.content.0.type").String())
	require.Equal(t, "call_shell", gjson.GetBytes(raw, "messages.1.content.0.tool_use_id").String())
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

func anthropicToolNames(raw []byte) []string {
	tools := gjson.GetBytes(raw, "tools").Array()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Get("name").String())
	}
	return names
}

func anthropicToolByName(raw []byte, name string) gjson.Result {
	for _, tool := range gjson.GetBytes(raw, "tools").Array() {
		if tool.Get("name").String() == name {
			return tool
		}
	}
	return gjson.Result{}
}

func anthropicRequestToolByName(t *testing.T, tools []any, name string) map[string]any {
	t.Helper()
	for _, toolAny := range tools {
		tool, ok := toolAny.(map[string]any)
		if !ok {
			continue
		}
		if tool["name"] == name {
			return tool
		}
	}
	require.Failf(t, "anthropic request tool not found", "name=%s tools=%v", name, tools)
	return nil
}
