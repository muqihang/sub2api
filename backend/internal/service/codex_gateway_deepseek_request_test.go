package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func mustMarshalRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestCodexGatewayDeepSeekRequest_BuildsMessagesToolsAndUserID(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Act as a precise coding assistant."`),
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"inspect the repository"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
				]
			}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"shell","description":"run shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
		Reasoning:       json.RawMessage(`{"effort":"low"}`),
		ToolChoice:      json.RawMessage(`"auto"`),
		MaxOutputTokens: intPtr(512),
		RawFields: map[string]json.RawMessage{
			"temperature":       json.RawMessage(`0.7`),
			"top_p":             json.RawMessage(`0.9`),
			"presence_penalty":  json.RawMessage(`0.2`),
			"frequency_penalty": json.RawMessage(`0.1`),
		},
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{
		ImageInputMode: CodexGatewayDeepSeekImageInputModePlaceholder,
	})
	require.NoError(t, err)

	require.Equal(t, "deepseek-v4-pro", prepared.Body["model"])
	require.Equal(t, map[string]any{"type": "enabled"}, prepared.Body["thinking"])
	require.Equal(t, "high", prepared.Body["reasoning_effort"])
	require.Equal(t, float64(512), prepared.Body["max_tokens"])
	require.NotContains(t, prepared.Body, "tool_choice")
	require.NotContains(t, prepared.Body, "temperature")
	require.NotContains(t, prepared.Body, "top_p")

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
	require.Contains(t, messages[1].(map[string]any)["content"].(string), "inspect the repository")
	require.Contains(t, messages[1].(map[string]any)["content"].(string), "input_image")

	tools := prepared.Body["tools"].([]any)
	require.Len(t, tools, 1)
	require.Equal(t, "shell", tools[0].(map[string]any)["function"].(map[string]any)["name"])

	userID := prepared.Body["user_id"].(string)
	require.True(t, regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`).MatchString(userID))
}

func TestCodexGatewayAgnesRequest_PreservesNativeImagesAndOfficialThinking(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"describe this image"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"high"}
				]
			}
		]`),
		Reasoning: json.RawMessage(`{"effort":"xhigh"}`),
		RawFields: map[string]json.RawMessage{
			"temperature": json.RawMessage(`0.2`),
		},
	}
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_agnes",
		IsolationKey: "iso_agnes",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{
		Provider:             "agnes",
		ReasoningMode:        "openai",
		SupportsNativeImages: true,
	})
	require.NoError(t, err)

	require.Equal(t, "agnes-2.0-flash", prepared.Body["model"])
	require.Equal(t, "high", prepared.Body["reasoning_effort"])
	require.Equal(t, map[string]any{"enable_thinking": true}, prepared.Body["chat_template_kwargs"])
	require.NotContains(t, prepared.Body, "thinking")
	require.NotContains(t, prepared.Body, "temperature")

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	content := messages[0].(map[string]any)["content"].([]any)
	require.Equal(t, map[string]any{"type": "text", "text": "describe this image"}, content[0])
	require.Equal(t, "image_url", content[1].(map[string]any)["type"])
	require.Equal(t, "data:image/png;base64,AAAA", content[1].(map[string]any)["image_url"].(map[string]any)["url"])
}

func TestCodexGatewayAgnesRequest_DisablesOfficialThinkingForComputerUseTools(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"Use Computer Use to edit a text file."}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"list_apps","parameters":{"type":"object","properties":{}}},
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}},
				{"type":"function","name":"set_value","parameters":{"type":"object","properties":{"app":{"type":"string"},"element_index":{"type":"string"},"value":{"type":"string"}},"required":["app","element_index","value"]}}
			]}
		]`),
		Reasoning: json.RawMessage(`{"effort":"xhigh"}`),
	}
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_agnes_cu",
		IsolationKey: "iso_agnes_cu",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{
		Provider:             "agnes",
		ReasoningMode:        "openai",
		SupportsNativeImages: true,
	})
	require.NoError(t, err)

	require.NotContains(t, prepared.Body, "reasoning_effort")
	require.Equal(t, map[string]any{"enable_thinking": false}, prepared.Body["chat_template_kwargs"])
}

func TestCodexGatewayAgnesRequest_KeepsAssistantReplayContentStringWithNativeImagesEnabled(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"previous answer"}]},
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"now inspect this"},
				{"type":"input_image","image_url":{"url":"https://example.test/image.png","detail":"low"}}
			]}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{}, CodexGatewayDeepSeekRequestConfig{
		Provider:             "agnes",
		ReasoningMode:        "openai",
		SupportsNativeImages: true,
	})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	require.Equal(t, "previous answer", messages[0].(map[string]any)["content"])
	userContent := messages[1].(map[string]any)["content"].([]any)
	require.Equal(t, "https://example.test/image.png", userContent[1].(map[string]any)["image_url"].(map[string]any)["url"])
	require.Equal(t, "low", userContent[1].(map[string]any)["image_url"].(map[string]any)["detail"])
}

func TestCodexGatewayAgnesRequest_DisablesOfficialThinkingForNoneReasoning(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-1.5-flash", Provider: "agnes", UpstreamModel: "agnes-1.5-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:     "agnes-1.5-flash",
		Input:     json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}]`),
		Reasoning: json.RawMessage(`{"effort":"none"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{}, CodexGatewayDeepSeekRequestConfig{
		Provider:      "agnes",
		ReasoningMode: "openai",
	})
	require.NoError(t, err)

	require.NotContains(t, prepared.Body, "reasoning_effort", "AGNES upstream rejects reasoning_effort=none")
	require.Equal(t, map[string]any{"enable_thinking": false}, prepared.Body["chat_template_kwargs"])
	require.NotContains(t, prepared.Body, "thinking")
}

func TestCodexGatewayAgnesRequest_DefaultEnablesOfficialThinkingWithoutReasoningEffort(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: json.RawMessage(`"reply AGNES_OK"`),
	}, nil, CodexGatewayDeepSeekRequestContext{}, CodexGatewayDeepSeekRequestConfig{
		Provider:      "agnes",
		ReasoningMode: "openai",
	})
	require.NoError(t, err)

	require.NotContains(t, prepared.Body, "reasoning_effort")
	require.Equal(t, map[string]any{"enable_thinking": true}, prepared.Body["chat_template_kwargs"])
	require.NotContains(t, prepared.Body, "thinking")
}

func TestCodexGatewayAgnesRequest_ConvertsStringInputToUserMessage(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: json.RawMessage(`"hello from string input"`),
	}, nil, CodexGatewayDeepSeekRequestContext{}, CodexGatewayDeepSeekRequestConfig{
		Provider:      "agnes",
		ReasoningMode: "openai",
	})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].(map[string]any)["role"])
	require.Equal(t, "hello from string input", messages[0].(map[string]any)["content"])
}

func TestCodexGatewayAgnesRequest_PreservesNativeImagesForAgnes15Flash(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-1.5-flash", Provider: "agnes", UpstreamModel: "agnes-1.5-flash", InputModalities: []string{"text", "image"}}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "agnes-1.5-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"describe"},
				{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
			]}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{}, CodexGatewayDeepSeekRequestConfig{
		Provider:             "agnes",
		ReasoningMode:        "openai",
		SupportsNativeImages: true,
	})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	require.Equal(t, "image_url", content[1].(map[string]any)["type"])
	require.Equal(t, "data:image/png;base64,AAAA", content[1].(map[string]any)["image_url"].(map[string]any)["url"])
}

func TestCodexGatewayAgnesRequest_StripsDeepSeekReasoningContentFromReplayMessages(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	replayMessages := []json.RawMessage{
		json.RawMessage(`{"role":"assistant","content":"prev","reasoning_content":"stored plan","tool_calls":[{"id":"call_prev","type":"function","function":{"name":"shell","arguments":"{\"cmd\":\"pwd\"}"}}]}`),
	}
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_agnes_tool",
			SessionKey:    "session_agnes_tool",
			IsolationKey:  "iso_agnes_tool",
			Provider:      "agnes",
			UpstreamModel: "agnes-2.0-flash",
		},
		AssistantContent:            "prev",
		AssistantContentPresent:     true,
		ReasoningContent:            "stored plan",
		ReasoningContentPresent:     true,
		ReasoningContentSynthesized: false,
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:        "call_prev",
			Type:      CodexGatewayToolKindFunction,
			Alias:     "shell",
			Name:      "shell",
			Arguments: `{"cmd":"pwd"}`,
		}},
		ToolNameMap:    map[string]CodexGatewayToolNameMapEntry{"shell": {Alias: "shell", Kind: CodexGatewayToolKindFunction, Name: "shell"}},
		ReplayMessages: replayMessages,
	}))

	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash", InputModalities: []string{"text", "image"}}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "agnes-2.0-flash",
		PreviousResponseID: stringPtr("resp_agnes_tool"),
		Tools:              json.RawMessage(`[{"type":"function","name":"shell","description":"run shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]`),
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_prev","output":"ok"}]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_agnes_tool",
		IsolationKey: "iso_agnes_tool",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai"})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"thinking"`)
	require.NotContains(t, string(raw), `reasoning_content`)
}

func TestCodexGatewayAgnesRequest_StripsDeepSeekReasoningContentFromToolCallMessages(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash", InputModalities: []string{"text", "image"}}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Tools: json.RawMessage(`[
			{"type":"function","name":"shell","description":"run shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}},
			{"type":"custom","name":"apply_patch","description":"apply patch","input_format":{"type":"text"}}
		]`),
		Input: json.RawMessage(`[
			{"type":"reasoning","summary_text":"new hidden plan"},
			{"type":"function_call","call_id":"call_new","name":"shell","arguments":{"cmd":"ls"}},
			{"type":"custom_tool_call","call_id":"call_custom","name":"apply_patch","input":"patch body"}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{Provider: "agnes"}, CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai"})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"thinking"`)
	require.NotContains(t, string(raw), `reasoning_content`)
}

func TestCodexGatewayAgnesRequest_TextOnlyImageErrorNamesAgnes(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-custom-text", Provider: "agnes", UpstreamModel: "agnes-custom-text", InputModalities: []string{"text"}}

	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "agnes-custom-text",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"describe"},
				{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
			]}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{}, CodexGatewayDeepSeekRequestConfig{
		Provider:             "agnes",
		ReasoningMode:        "openai",
		SupportsNativeImages: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "codex agnes request does not support image input")
	require.NotContains(t, err.Error(), "deepseek")
}

func TestCodexGatewayAgnesRequest_TextOnlyModelRejectsImagesEvenIfCapabilityFlagsAreWrong(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-custom-text", Provider: "agnes", UpstreamModel: "agnes-custom-text", InputModalities: []string{"text"}, SupportsImageDetailOriginal: true}

	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "agnes-custom-text",
		Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{}, CodexGatewayDeepSeekRequestConfig{
		Provider:             "agnes",
		ReasoningMode:        "openai",
		SupportsNativeImages: true,
	})
	require.Error(t, err)
}

func TestCodexGatewayAgnesRequest_RestoresPreviousResponseStateUnderAgnesProvider(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_agnes_replay",
			SessionKey:    "session_agnes_replay",
			IsolationKey:  "iso_agnes_replay",
			Provider:      "agnes",
			UpstreamModel: "agnes-2.0-flash",
		},
		AssistantContent:        "previous agnes answer",
		AssistantContentPresent: true,
		ReasoningContent:        "previous agnes reasoning",
		ReasoningContentPresent: true,
	}))
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_deepseek_same_id",
			SessionKey:    "session_agnes_replay",
			IsolationKey:  "iso_agnes_replay",
			Provider:      "deepseek",
			UpstreamModel: "agnes-2.0-flash",
		},
		AssistantContent:        "wrong provider answer",
		AssistantContentPresent: true,
	}))
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "agnes-2.0-flash",
		PreviousResponseID: stringPtr("resp_agnes_replay"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_agnes_replay",
		IsolationKey: "iso_agnes_replay",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{
		Provider:      "agnes",
		ReasoningMode: "openai",
	})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "previous agnes answer", assistant["content"])
	require.NotContains(t, assistant, "reasoning_content")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "reasoning_content")
}

func TestCodexGatewayAgnesRequest_RejectsDeepSeekStateForSamePreviousResponseID(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_cross_provider",
			SessionKey:    "session_cross_provider",
			IsolationKey:  "iso_cross_provider",
			Provider:      "deepseek",
			UpstreamModel: "agnes-2.0-flash",
		},
		AssistantContent:        "deepseek answer",
		AssistantContentPresent: true,
	}))
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "agnes-2.0-flash",
		PreviousResponseID: stringPtr("resp_cross_provider"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_cross_provider",
		IsolationKey: "iso_cross_provider",
	}, CodexGatewayDeepSeekRequestConfig{
		Provider:      "agnes",
		ReasoningMode: "openai",
	})
	require.ErrorIs(t, err, ErrCodexGatewayStateConflict)
}

func TestCodexGatewayDeepSeekRequest_FlattensDeepSchemasAndAddsDeepSeekToolHints(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use tools carefully"}]}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"exec_command",
				"description":"run shell commands",
				"parameters":{
					"type":"object",
					"properties":{
						"cmd":{"type":"string"},
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{
										"include":{"type":"string"},
										"exclude":{"type":"string"}
									},
									"required":["include"]
								}
							},
							"required":["root","filters"]
						}
					},
					"required":["cmd","workspace"]
				}
			},
			{
				"type":"function",
				"name":"python",
				"description":"run python",
				"parameters":{"type":"object","properties":{"code":{"type":"string"}}}
			},
			{
				"type":"custom",
				"name":"apply_patch",
				"description":"edit files",
				"format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}
			}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_flatten",
		IsolationKey: "iso_flatten",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableDeepSeekSchemaFlattening: true,
			DeepSeekFlattenMinDepth:        3,
			DeepSeekFlattenMinLeaves:       4,
		},
	})
	require.NoError(t, err)

	tools := prepared.Body["tools"].([]any)
	require.Len(t, tools, 3)

	execTool := deepSeekRequestToolFunctionByName(t, tools, "exec_command")
	require.Contains(t, execTool["description"], "`cmd`")
	require.Contains(t, execTool["description"], "python3 - <<'PY'")
	execParams := execTool["parameters"].(map[string]any)
	execProperties := execParams["properties"].(map[string]any)
	require.Contains(t, execProperties, "workspace.root")
	require.Contains(t, execProperties, "workspace.filters.include")
	require.Contains(t, execProperties, "workspace.filters.exclude")
	require.NotContains(t, execProperties, "workspace")
	require.ElementsMatch(t, []any{"cmd", "workspace.root", "workspace.filters.include"}, execParams["required"].([]any))

	pythonTool := deepSeekRequestToolFunctionByName(t, tools, "python")
	require.Contains(t, pythonTool["description"], "python3 - <<'PY'")

	patchTool := deepSeekRequestToolFunctionByDescription(t, tools, "custom input field")
	require.Contains(t, patchTool["description"], "custom input field")
	require.Contains(t, patchTool["description"], "exact raw input")
}

func TestCodexGatewayDeepSeekRequest_AllowsDisablingSchemaFlatteningAndAvoidsFlatKeyCollisions(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use tool"}]}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"search_repo",
				"parameters":{
					"type":"object",
					"properties":{
						"query":{"type":"string"},
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{"include":{"type":"string"}}
								}
							}
						}
					}
				}
			}
		]`),
	}

	disabled, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_no_flatten",
		IsolationKey: "iso_no_flatten",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	disabledProps := disabled.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["properties"].(map[string]any)
	require.Contains(t, disabledProps, "workspace")
	require.NotContains(t, disabledProps, "workspace.root")

	collisionReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: req.Input,
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"collision_tool",
				"parameters":{
					"type":"object",
					"properties":{
						"a.b":{"type":"string"},
						"a":{"type":"object","properties":{"b":{"type":"string"}}, "required":["b"]},
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{
										"include":{"type":"string"},
										"exclude":{"type":"string"}
									}
								}
							}
						}
					}
				}
			}
		]`),
	}
	collisionPrepared, err := BuildCodexGatewayDeepSeekRequest(model, collisionReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_collision",
		IsolationKey: "iso_collision",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableDeepSeekSchemaFlattening: true,
			DeepSeekFlattenMinDepth:        3,
			DeepSeekFlattenMinLeaves:       4,
		},
	})
	require.NoError(t, err)
	collisionProps := collisionPrepared.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["properties"].(map[string]any)
	require.Contains(t, collisionProps, "a")
	require.Contains(t, collisionProps, "a.b")
	require.NotContains(t, collisionProps, "workspace.root")
}

func TestCodexGatewayDeepSeekRequest_StrictBetaIsOptInAndOnlySimpleSchemasQualify(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	simpleReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use strict tool"}]}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"weather",
				"parameters":{
					"type":"object",
					"properties":{
						"city":{"type":"string"},
						"units":{"type":"string"}
					},
					"required":["city"]
				},
				"strict":true
			}
		]`),
	}

	defaultPrepared, err := BuildCodexGatewayDeepSeekRequest(model, simpleReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_strict_default",
		IsolationKey: "iso_strict_default",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	defaultFn := defaultPrepared.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	require.NotContains(t, defaultFn, "strict")

	strictPrepared, err := BuildCodexGatewayDeepSeekRequest(model, simpleReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_strict_enabled",
		IsolationKey: "iso_strict_enabled",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableStrictBeta: true,
		},
	})
	require.NoError(t, err)
	strictFn := strictPrepared.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	require.Equal(t, true, strictFn["strict"])

	complexReq := CodexGatewayResponsesCreateRequest{
		Model: simpleReq.Model,
		Input: simpleReq.Input,
		Tools: json.RawMessage(`[
			{
				"type":"function",
				"name":"complex_search",
				"parameters":{
					"type":"object",
					"properties":{
						"workspace":{
							"type":"object",
							"properties":{
								"root":{"type":"string"},
								"filters":{
									"type":"object",
									"properties":{
										"include":{"type":"string"},
										"exclude":{"type":"string"}
									}
								}
							}
						}
					}
				},
				"strict":true
			},
			{
				"type":"custom",
				"name":"apply_patch",
				"strict":true,
				"format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}
			}
		]`),
	}
	complexPrepared, err := BuildCodexGatewayDeepSeekRequest(model, complexReq, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_strict_complex",
		IsolationKey: "iso_strict_complex",
	}, CodexGatewayDeepSeekRequestConfig{
		ToolMappingConfig: CodexGatewayToolMappingConfig{
			EnableStrictBeta: true,
		},
	})
	require.NoError(t, err)
	complexTools := complexPrepared.Body["tools"].([]any)
	require.NotContains(t, complexTools[0].(map[string]any)["function"].(map[string]any), "strict")
	require.NotContains(t, complexTools[1].(map[string]any)["function"].(map[string]any), "strict")
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_SkipsPlainInputImages(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"请看这张图"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
				]
			}
			]`),
	}
	called := false
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			called = true
			return "这是一张终端截图，主要内容是目录树。", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, nil, CodexGatewayDeepSeekRequestContext{}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	require.False(t, called)
	require.JSONEq(t, string(req.Input), string(rewritten.Input))
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_SkipsRequestsWithoutImages(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"只是一段文字"}
				]
			}
		]`),
	}
	called := false
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			called = true
			return "unexpected", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, nil, CodexGatewayDeepSeekRequestContext{}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	require.False(t, called)
	require.JSONEq(t, string(req.Input), string(rewritten.Input))
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_FallsBackToPlaceholderOnVisionError(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_text","text":"请看这张图"},
					{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
				]
			}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	cfg := CodexGatewayDeepSeekRequestConfig{
		ImageInputMode: CodexGatewayDeepSeekImageInputModePlaceholder,
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			return "", errors.New("vision unavailable")
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, nil, CodexGatewayDeepSeekRequestContext{}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, rewritten, nil, CodexGatewayDeepSeekRequestContext{}, cfg)
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	require.Contains(t, messages[0].(map[string]any)["content"].(string), codexGatewayDeepSeekImagePlaceholder())
}

func TestCodexGatewayDeepSeekRequest_KeepsWebSearchBridgeAndFiltersUnsupportedHostedTools(t *testing.T) {
	parallel := true
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"web_search"},
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}},
			{"type":"image_generation"},
			{"type":"computer_use_preview"},
			{"type":"file_search"},
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
		ToolChoice:        json.RawMessage(`"auto"`),
		ParallelToolCalls: &parallel,
	}
	model := CodexGatewayModel{
		Slug:                      "deepseek-v4-pro",
		Provider:                  "deepseek",
		UpstreamModel:             "deepseek-v4-pro",
		SupportsParallelToolCalls: false,
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.NotContains(t, prepared.Body, "parallel_tool_calls")
	require.NotContains(t, prepared.Body, "tool_choice")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "OpenAI hosted tools are not available")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "image_generation")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "computer_use_preview")
	require.Contains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "file_search")
	require.NotContains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "web_search")

	tools := prepared.Body["tools"].([]any)
	require.Len(t, tools, 3)
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, tools, "web_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, tools, "tool_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, tools, "exec_command"))
}

func TestCodexGatewayDeepSeekRequest_NativeParityFixtureToolSearch(t *testing.T) {
	failed := loadCodexGatewayDeepSeekNativeParityFixture(t, "failed_tool_search_function_call.json")
	require.Equal(t, "observed_deepseek_failure", gjson.GetBytes(failed, "source_baseline").String())
	require.Equal(t, "function_call", gjson.GetBytes(failed, "item.type").String())
	require.Equal(t, "tool_search", gjson.GetBytes(failed, "item.name").String())
	require.Equal(t, int64(7), gjson.GetBytes(failed, "observed_call_count").Int())
	require.False(t, gjson.GetBytes(failed, "item.matching_tool_search_output_present").Bool())

	native := loadCodexGatewayDeepSeekNativeParityFixture(t, "native_tool_search_call_output.json")
	require.Equal(t, "successful_codex_native_deepseek_bridge", gjson.GetBytes(native, "source_baseline").String())
	require.Equal(t, "tool_search_call", gjson.GetBytes(native, "tool_search_call.type").String())
	require.Equal(t, "call_fixture", gjson.GetBytes(native, "tool_search_call.call_id").String())
	require.Equal(t, "client", gjson.GetBytes(native, "tool_search_call.execution").String())
	require.Equal(t, "tool_search_output", gjson.GetBytes(native, "tool_search_output.type").String())
	require.Equal(t, "multi_agent_v1", gjson.GetBytes(native, "tool_search_output.tools.0.name").String())
	require.Equal(t, "spawn_agent", gjson.GetBytes(native, "tool_search_output.tools.0.tools.0.name").String())
	require.Equal(t, "string", gjson.GetBytes(native, "tool_search_output.tools.0.tools.0.input_schema.properties.model.type").String())
}

func TestCodexGatewayDeepSeekRequest_NativeParityFixtureComputerUseOutputSizes(t *testing.T) {
	fixture := loadCodexGatewayDeepSeekNativeParityFixture(t, "computer_use_output_sizes.json")
	require.Equal(t, "successful_codex_native_deepseek_bridge_child_session", gjson.GetBytes(fixture, "source_baseline").String())
	samples := gjson.GetBytes(fixture, "samples").Array()
	require.Len(t, samples, 2)
	for _, sample := range samples {
		require.GreaterOrEqual(t, sample.Get("raw_output_chars").Int(), int64(86000))
		require.GreaterOrEqual(t, sample.Get("app_state_chars").Int(), int64(4000))
		require.GreaterOrEqual(t, sample.Get("screenshot_chars").Int(), int64(81000))
		require.True(t, sample.Get("app_state_close_marker_present").Bool())
		require.True(t, sample.Get("deepseek_visible_normalized_output_retained_computer_screenshot").Bool())
		require.True(t, sample.Get("deepseek_visible_normalized_output_retained_operable_lines").Bool())
		require.True(t, sample.Get("deepseek_visible_normalized_output_retained_lower_screen_actionable_lines").Bool())
	}
}

func TestCodexGatewayDeepSeekRequest_NativeParityFixtureComputerUseSemanticCompression(t *testing.T) {
	fixture := loadCodexGatewayDeepSeekNativeParityFixture(t, "computer_use_semantic_compression_cases.json")
	require.Equal(t, codexGatewayDeepSeekComputerUseCompressionVersion, gjson.GetBytes(fixture, "compression_version").String())
	cases := gjson.GetBytes(fixture, "cases").Array()
	require.NotEmpty(t, cases)
	for _, tc := range cases {
		name := tc.Get("name").String()
		t.Run(name, func(t *testing.T) {
			var output any
			require.NoError(t, json.Unmarshal([]byte(tc.Get("output").Raw), &output))
			normalized, err := normalizeCodexGatewayDeepSeekToolOutput(output)
			require.NoError(t, err)
			for _, expected := range tc.Get("expect_contains").Array() {
				require.Contains(t, normalized, expected.String())
			}
			for _, forbidden := range tc.Get("expect_not_contains").Array() {
				require.NotContains(t, normalized, forbidden.String())
			}
			for _, path := range tc.Get("expect_paths").Array() {
				require.True(t, gjson.Get(normalized, path.String()).Exists(), "expected normalized output path %s in %s", path.String(), normalized)
			}
			require.LessOrEqual(t, len(normalized), codexGatewayDeepSeekToolOutputMaxChars+256)
		})
	}
}

func TestCodexGatewayDeepSeekRequest_ConvertsToolSearchCallToDeepSeekToolCall(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"tool_search_call",
				"call_id":"call_tool_search",
				"status":"completed",
				"execution":"client",
				"arguments":{"query":"spawn_agent","limit":10}
			}
		]`),
		Tools: json.RawMessage(`[
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
		]`),
	}
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_tool_search_call",
		IsolationKey: "iso_tool_search_call",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "", assistant["content"])
	calls := assistant["tool_calls"].([]any)
	require.Len(t, calls, 1)
	call := calls[0].(map[string]any)
	require.Equal(t, "call_tool_search", call["id"])
	require.Equal(t, "function", call["type"])
	function := call["function"].(map[string]any)
	require.Equal(t, "tool_search", function["name"])
	require.JSONEq(t, `{"query":"spawn_agent","limit":10}`, function["arguments"].(string))
}

func TestCodexGatewayDeepSeekRequest_ConvertsToolSearchOutputToToolMessage(t *testing.T) {
	fixture := loadCodexGatewayDeepSeekNativeParityFixture(t, "native_tool_search_call_output.json")
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"tool_search_call",
				"call_id":"call_fixture",
				"status":"completed",
				"execution":"client",
				"arguments":{"query":"sub-agent dispatch multi-agent DeepSeek V4 Flash model tool","limit":10}
			},
			` + gjson.GetBytes(fixture, "tool_search_output").Raw + `
		]`),
		Tools: json.RawMessage(`[
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
		]`),
	}
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_tool_search_output",
		IsolationKey: "iso_tool_search_output",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	toolMessage := messages[1].(map[string]any)
	require.Equal(t, "tool", toolMessage["role"])
	require.Equal(t, "call_fixture", toolMessage["tool_call_id"])
	content := toolMessage["content"].(string)
	require.NotEmpty(t, content)
	require.JSONEq(t, gjson.GetBytes(fixture, "tool_search_output.tools").Raw, content)
	require.Equal(t, `[{"name":"multi_agent_v1","tools":[{"description":"sanitized spawn-agent tool description","input_schema":{"properties":{"model":{"type":"string"},"task":{"type":"string"}},"required":["task"],"type":"object"},"name":"spawn_agent"}],"type":"namespace"}]`, content)
	require.Equal(t, "multi_agent_v1", gjson.Get(content, "0.name").String())
	require.Equal(t, "spawn_agent", gjson.Get(content, "0.tools.0.name").String())
	require.Equal(t, "string", gjson.Get(content, "0.tools.0.input_schema.properties.model.type").String())
}

func TestCodexGatewayDeepSeekRequest_MapsDeferredNamespaceToolsFromToolSearchOutputVariants(t *testing.T) {
	deferredTools := []any{
		map[string]any{
			"type": "namespace",
			"name": "multi_agent_v1",
			"tools": []any{
				map[string]any{
					"type":       "function",
					"name":       "spawn_agent",
					"parameters": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
				},
			},
		},
	}
	deferredToolsJSON := string(mustMarshalRawMessage(t, deferredTools))
	variantInputs := map[string]json.RawMessage{
		"top_level_tools": json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"spawn"}]},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"spawn_agent","limit":10}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":` + deferredToolsJSON + `}
		]`),
		"output_json_array_string": json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"spawn"}]},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"spawn_agent","limit":10}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","output":` + string(mustMarshalRawMessage(t, deferredToolsJSON)) + `}
		]`),
		"output_json_object_string": json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"spawn"}]},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"spawn_agent","limit":10}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","output":` + string(mustMarshalRawMessage(t, `{"tools":`+deferredToolsJSON+`}`)) + `}
		]`),
		"output_object": json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"spawn"}]},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"spawn_agent","limit":10}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","output":{"tools":` + deferredToolsJSON + `}}
		]`),
		"output_array": json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"spawn"}]},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"spawn_agent","limit":10}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","output":` + deferredToolsJSON + `}
		]`),
	}
	for name, input := range variantInputs {
		t.Run(name, func(t *testing.T) {
			req := CodexGatewayResponsesCreateRequest{
				Model: "deepseek-v4-pro",
				Input: input,
				Tools: json.RawMessage(`[
					{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
				]`),
			}
			model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}

			prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
				SessionKey:   "session_deferred_variant_" + name,
				IsolationKey: "iso_deferred_variant_" + name,
			}, CodexGatewayDeepSeekRequestConfig{})
			require.NoError(t, err)

			require.NotNil(t, deepSeekRequestToolFunctionByName(t, prepared.Body["tools"].([]any), "multi_agent_v1__spawn_agent"))
			entry := prepared.ToolNameMap["multi_agent_v1__spawn_agent"]
			require.Equal(t, CodexGatewayToolKindNamespace, entry.Kind)
			require.Equal(t, "multi_agent_v1", entry.Namespace)
			require.Equal(t, "spawn_agent", entry.Name)
		})
	}
}

func TestCodexGatewayDeepSeekRequest_MapsDeferredToolFamilyMatrixFromToolSearchOutput(t *testing.T) {
	deferredTools := []any{
		map[string]any{
			"type": "namespace",
			"name": "browser",
			"tools": []any{
				map[string]any{
					"type":        "function",
					"name":        "navigate",
					"description": "open a local browser target",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"url": map[string]any{"type": "string"}},
						"required":   []any{"url"},
					},
				},
			},
		},
		map[string]any{
			"type": "namespace",
			"name": "computer_use",
			"tools": []any{
				map[string]any{
					"type":        "function",
					"name":        "list_apps",
					"description": "list controllable local apps",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
		map[string]any{
			"type": "namespace",
			"name": "documents",
			"tools": []any{
				map[string]any{
					"type":   "custom",
					"name":   "redline",
					"custom": map[string]any{"format": map[string]any{"type": "text"}},
				},
			},
		},
		map[string]any{
			"type": "namespace",
			"name": "multi_agent_v1",
			"tools": []any{
				map[string]any{
					"type": "function",
					"name": "spawn_agent",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"task": map[string]any{"type": "string"}, "model": map[string]any{"type": "string"}},
						"required":   []any{"task"},
					},
				},
			},
		},
	}
	deferredToolsJSON := string(mustMarshalRawMessage(t, deferredTools))
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"classify deferred tools"}]},
			{"type":"tool_search_call","call_id":"call_search","status":"completed","execution":"client","arguments":{"query":"browser computer use documents subagent","limit":20}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":` + deferredToolsJSON + `}
		]`),
		Tools: json.RawMessage(`[
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
		]`),
	}
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_deferred_family_matrix",
		IsolationKey: "iso_deferred_family_matrix",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	tools := prepared.Body["tools"].([]any)
	for _, alias := range []string{"browser__navigate", "computer_use__list_apps", "documents__custom__redline", "multi_agent_v1__spawn_agent", "tool_search"} {
		require.NotNil(t, deepSeekRequestToolFunctionByName(t, tools, alias), "missing DeepSeek tool alias %s", alias)
	}

	cases := map[string]struct {
		kind          string
		namespacePath string
		visibleName   string
	}{
		"browser__navigate":           {kind: CodexGatewayToolKindNamespace, namespacePath: "browser", visibleName: "navigate"},
		"computer_use__list_apps":     {kind: CodexGatewayToolKindNamespace, namespacePath: "computer_use", visibleName: "list_apps"},
		"documents__custom__redline":  {kind: CodexGatewayToolKindCustom, namespacePath: "documents", visibleName: "redline"},
		"multi_agent_v1__spawn_agent": {kind: CodexGatewayToolKindNamespace, namespacePath: "multi_agent_v1", visibleName: "spawn_agent"},
	}
	for alias, want := range cases {
		entry := prepared.ToolNameMap[alias]
		require.Equal(t, alias, entry.Alias)
		require.Equal(t, want.kind, entry.Kind)
		require.Equal(t, want.namespacePath, entry.NamespacePath)
		require.Equal(t, want.visibleName, codexGatewayClientVisibleToolName(entry))
	}

	messages := prepared.Body["messages"].([]any)
	var toolMessage map[string]any
	for _, message := range messages {
		m, ok := message.(map[string]any)
		if !ok || m["role"] != "tool" || m["tool_call_id"] != "call_search" {
			continue
		}
		toolMessage = m
		break
	}
	require.NotNil(t, toolMessage)
	require.JSONEq(t, deferredToolsJSON, toolMessage["content"].(string))
}

func TestCodexGatewayDeepSeekAdapter_MapsDeferredToolFamilyMatrixCalls(t *testing.T) {
	families := map[string]struct {
		entry     CodexGatewayToolNameMapEntry
		arguments string
		wantType  string
		wantName  string
		wantNS    string
		wantInput string
	}{
		"browser__navigate": {
			entry:     CodexGatewayToolNameMapEntry{Alias: "browser__navigate", Kind: CodexGatewayToolKindNamespace, Namespace: "browser", NamespacePath: "browser", Name: "navigate"},
			arguments: `{"url":"http://localhost:3000"}`,
			wantType:  CodexGatewayOutputItemTypeFunctionCall,
			wantName:  "navigate",
			wantNS:    "browser",
		},
		"computer_use__list_apps": {
			entry:     CodexGatewayToolNameMapEntry{Alias: "computer_use__list_apps", Kind: CodexGatewayToolKindNamespace, Namespace: "computer_use", NamespacePath: "computer_use", Name: "list_apps"},
			arguments: `{}`,
			wantType:  CodexGatewayOutputItemTypeFunctionCall,
			wantName:  "list_apps",
			wantNS:    "computer_use",
		},
		"documents__custom__redline": {
			entry:     CodexGatewayToolNameMapEntry{Alias: "documents__custom__redline", Kind: CodexGatewayToolKindCustom, Namespace: "documents", NamespacePath: "documents", Name: "redline"},
			arguments: `{"input":"mark this change"}`,
			wantType:  CodexGatewayOutputItemTypeCustomToolCall,
			wantName:  "redline",
			wantInput: "mark this change",
		},
	}

	for alias, tt := range families {
		t.Run(alias, func(t *testing.T) {
			item, stored, ok := codexGatewayDeepSeekToolCallOutputItem(apicompat.ChatToolCall{
				ID: "call_" + strings.ReplaceAll(alias, "__", "_"),
				Function: apicompat.ChatFunctionCall{
					Name:      alias,
					Arguments: tt.arguments,
				},
			}, map[string]CodexGatewayToolNameMapEntry{alias: tt.entry}, nil)
			require.True(t, ok)
			require.Equal(t, tt.wantType, item["type"])
			require.Equal(t, tt.wantName, item["name"])
			require.Equal(t, alias, stored.Alias)
			require.Equal(t, tt.wantName, stored.Name)
			if tt.wantNS != "" {
				require.Equal(t, tt.wantNS, item["namespace"])
			}
			if tt.wantInput != "" {
				require.Equal(t, tt.wantInput, item["input"])
			}
		})
	}
}

func TestCodexGatewayDeepSeekRequest_RejectsDeferredToolMappingCollisions(t *testing.T) {
	t.Run("alias collision", func(t *testing.T) {
		base := CodexGatewayToolMappingResult{
			Tools: []map[string]any{{"type": "function", "function": map[string]any{"name": "multi_agent_v1__spawn_agent"}}},
			NameMap: map[string]CodexGatewayToolNameMapEntry{
				"multi_agent_v1__spawn_agent": {Alias: "multi_agent_v1__spawn_agent", Kind: CodexGatewayToolKindFunction, Name: "multi_agent_v1__spawn_agent"},
			},
			originalToAlias: map[string]string{
				toolMappingOriginalKey(CodexGatewayToolKindFunction, "", "multi_agent_v1__spawn_agent"): "multi_agent_v1__spawn_agent",
			},
		}
		extra := CodexGatewayToolMappingResult{
			Tools: []map[string]any{{"type": "function", "function": map[string]any{"name": "multi_agent_v1__spawn_agent"}}},
			NameMap: map[string]CodexGatewayToolNameMapEntry{
				"multi_agent_v1__spawn_agent": {Alias: "multi_agent_v1__spawn_agent", Kind: CodexGatewayToolKindNamespace, Namespace: "multi_agent_v1", NamespacePath: "multi_agent_v1", Name: "spawn_agent"},
			},
			originalToAlias: map[string]string{
				toolMappingOriginalKey(CodexGatewayToolKindNamespace, "multi_agent_v1", "spawn_agent"): "multi_agent_v1__spawn_agent",
			},
		}

		_, err := mergeCodexGatewayToolMappings(base, extra)
		require.Error(t, err)
		require.Contains(t, err.Error(), `deferred tool alias collision for "multi_agent_v1__spawn_agent"`)
	})

	t.Run("original path collision", func(t *testing.T) {
		key := toolMappingOriginalKey(CodexGatewayToolKindNamespace, "multi_agent_v1", "spawn_agent")
		base := CodexGatewayToolMappingResult{
			Tools: []map[string]any{{"type": "function", "function": map[string]any{"name": "multi_agent_v1__spawn_agent"}}},
			NameMap: map[string]CodexGatewayToolNameMapEntry{
				"multi_agent_v1__spawn_agent": {Alias: "multi_agent_v1__spawn_agent", Kind: CodexGatewayToolKindNamespace, Namespace: "multi_agent_v1", NamespacePath: "multi_agent_v1", Name: "spawn_agent"},
			},
			originalToAlias: map[string]string{key: "multi_agent_v1__spawn_agent"},
		}
		extra := CodexGatewayToolMappingResult{
			Tools: []map[string]any{{"type": "function", "function": map[string]any{"name": "multi_agent_v1__spawn_agent_v2"}}},
			NameMap: map[string]CodexGatewayToolNameMapEntry{
				"multi_agent_v1__spawn_agent_v2": {Alias: "multi_agent_v1__spawn_agent_v2", Kind: CodexGatewayToolKindNamespace, Namespace: "multi_agent_v1", NamespacePath: "multi_agent_v1", Name: "spawn_agent"},
			},
			originalToAlias: map[string]string{key: "multi_agent_v1__spawn_agent_v2"},
		}

		_, err := mergeCodexGatewayToolMappings(base, extra)
		require.Error(t, err)
		require.Contains(t, err.Error(), `deferred tool original path collision for "namespace|multi_agent_v1|spawn_agent"`)
	})
}

func TestCodexGatewayDeepSeekRequest_ParallelToolCallsFalseAddsSerialToolInstruction(t *testing.T) {
	parallel := false
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use the available tool if needed"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"inspect_state","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}
		]`),
		ParallelToolCalls: &parallel,
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_serial_tools",
		IsolationKey: "user_serial_tools",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.NotContains(t, prepared.Body, "parallel_tool_calls")

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	instruction, ok := messages[0].(map[string]any)["content"].(string)
	require.True(t, ok)
	require.Equal(t, "Serial tool calling is required for this request: before receiving tool output, emit at most one tool call. After the tool output is provided, you may decide whether another tool call is needed.", instruction)
	require.Contains(t, instruction, "at most one tool call")
	require.Contains(t, instruction, "before receiving tool output")
	require.NotContains(t, instruction, "use the available tool if needed")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])

	parallel = true
	req.ParallelToolCalls = &parallel
	parallelPrepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_parallel_tools",
		IsolationKey: "user_parallel_tools",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	parallelMessages := parallelPrepared.Body["messages"].([]any)
	require.Len(t, parallelMessages, 1)
	require.NotContains(t, parallelMessages[0].(map[string]any)["content"], "Serial tool calling is required for this request")
}

func TestCodexGatewayDeepSeekRequest_ComputerUseAddsNativeOperationStrategy(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Use Doubao with Computer Use."}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"list_apps","parameters":{"type":"object","properties":{}}},
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}},
				{"type":"function","name":"set_value","parameters":{"type":"object","properties":{"app":{"type":"string"},"element_index":{"type":"string"},"value":{"type":"string"}},"required":["app","element_index","value"]}},
				{"type":"function","name":"press_key","parameters":{"type":"object","properties":{"app":{"type":"string"},"key":{"type":"string"}},"required":["app","key"]}},
				{"type":"function","name":"click","parameters":{"type":"object","properties":{"app":{"type":"string"},"element_index":{"type":"string"}},"required":["app"]}},
				{"type":"function","name":"type_text","parameters":{"type":"object","properties":{"app":{"type":"string"},"text":{"type":"string"}},"required":["app","text"]}},
				{"type":"function","name":"scroll","parameters":{"type":"object","properties":{"app":{"type":"string"},"element_index":{"type":"string"},"direction":{"type":"string"}},"required":["app","element_index","direction"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_computer_use_strategy",
		IsolationKey: "user_computer_use_strategy",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	instruction, ok := messages[0].(map[string]any)["content"].(string)
	require.True(t, ok)
	require.Contains(t, instruction, "Computer Use strategy")
	require.Contains(t, instruction, "bundle identifier")
	require.Contains(t, instruction, "localized display names")
	require.Contains(t, instruction, "set_value")
	require.Contains(t, instruction, "press_key Return")
	require.Contains(t, instruction, "get_app_state")
	require.Contains(t, instruction, "visible_text")
	require.Contains(t, instruction, "Avoid scrolling")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])

	tools := prepared.Body["tools"].([]any)
	getState := deepSeekRequestToolFunctionBySuffix(t, tools, "__get_app_state")
	require.Contains(t, getState["description"], "bundle identifier")
	require.Contains(t, getState["description"], "visible_text")
	getStateApp := getState["parameters"].(map[string]any)["properties"].(map[string]any)["app"].(map[string]any)
	require.Contains(t, getStateApp["description"], "bundle identifier")
	setValue := deepSeekRequestToolFunctionBySuffix(t, tools, "__set_value")
	require.Contains(t, setValue["description"], "Electron/chat")
	require.Contains(t, setValue["description"], "press_key Return")
	setValueIndex := setValue["parameters"].(map[string]any)["properties"].(map[string]any)["element_index"].(map[string]any)
	require.Contains(t, setValueIndex["description"], "get_app_state")
	scroll := deepSeekRequestToolFunctionBySuffix(t, tools, "__scroll")
	require.Contains(t, scroll["description"], "Avoid scrolling")
}

func TestCodexGatewayDeepSeekRequest_ComputerUseStrategyInjectedOnlyWhenToolsPresentAndDeduped(t *testing.T) {
	tools := json.RawMessage(`[
		{"type":"namespace","name":"mcp__computer_use__","tools":[
			{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
		]}
	]`)
	input := json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"Use Computer Use to inspect Codex."}]}
	]`)
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}

	noToolPrepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: input,
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_no_cu_tools",
		IsolationKey: "user_no_cu_tools",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	noToolMessages := mustMarshalJSON(t, noToolPrepared.Body["messages"])
	require.NotContains(t, string(noToolMessages), "bundle identifier values from list_apps")

	instructions, err := json.Marshal(codexGatewayDeepSeekComputerUseInstruction)
	require.NoError(t, err)
	dedupedPrepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: instructions,
		Input:        input,
		Tools:        tools,
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_cu_deduped",
		IsolationKey: "user_cu_deduped",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	dedupedMessages := mustMarshalJSON(t, dedupedPrepared.Body["messages"])
	require.Equal(t, 1, strings.Count(string(dedupedMessages), "Computer Use strategy"))
	require.Equal(t, 1, strings.Count(string(dedupedMessages), "bundle identifier values from list_apps"))

	agnesModel := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}
	agnesInstructions, err := json.Marshal(codexGatewayAgnesComputerUseInstruction)
	require.NoError(t, err)
	agnesPrepared, err := BuildCodexGatewayDeepSeekRequest(agnesModel, CodexGatewayResponsesCreateRequest{
		Model:        "agnes-2.0-flash",
		Instructions: agnesInstructions,
		Input:        input,
		Tools:        tools,
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_agnes_cu_deduped",
		IsolationKey: "user_agnes_cu_deduped",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai"})
	require.NoError(t, err)
	agnesMessages := mustMarshalJSON(t, agnesPrepared.Body["messages"])
	require.Equal(t, 1, strings.Count(string(agnesMessages), "Computer Use strategy"))
	require.Equal(t, 1, strings.Count(string(agnesMessages), "AGNES Computer Use continuation"))
}

func TestCodexGatewayDeepSeekRequest_ParallelToolCallsFalseDoesNotDuplicateSerialInstructionOnReplay(t *testing.T) {
	parallel := false
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}

	for _, tc := range []struct {
		name         string
		instructions json.RawMessage
	}{
		{name: "serial first"},
		{name: "serial after instructions", instructions: json.RawMessage(`"Follow developer instructions."`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
			fullReq := CodexGatewayResponsesCreateRequest{
				Model:             "deepseek-v4-pro",
				Instructions:      tc.instructions,
				ParallelToolCalls: &parallel,
				Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask one"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer one"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask two"}]}
		]`),
				Tools: json.RawMessage(`[
			{"type":"function","name":"inspect_state","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}
		]`),
			}
			fullPrepared, err := BuildCodexGatewayDeepSeekRequest(model, fullReq, nil, CodexGatewayDeepSeekRequestContext{
				SessionKey:   "session_serial_replay",
				IsolationKey: "iso_serial_replay",
			}, CodexGatewayDeepSeekRequestConfig{})
			require.NoError(t, err)

			replayReq := fullReq
			replayReq.Input = json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"ask one"}]}
	]`)
			firstPrepared, err := BuildCodexGatewayDeepSeekRequest(model, replayReq, nil, CodexGatewayDeepSeekRequestContext{
				SessionKey:   "session_serial_replay",
				IsolationKey: "iso_serial_replay",
			}, CodexGatewayDeepSeekRequestConfig{})
			require.NoError(t, err)
			firstReplay := append([]json.RawMessage(nil), firstPrepared.ReplayMessages...)
			assistant, err := json.Marshal(map[string]any{"role": "assistant", "content": "answer one"})
			require.NoError(t, err)
			firstReplay = append(firstReplay, assistant)
			require.NoError(t, store.Put(CodexGatewayResponseState{
				Key: CodexGatewayStateLookupKey{
					ResponseID:    "resp_serial_replay",
					SessionKey:    "session_serial_replay",
					IsolationKey:  "iso_serial_replay",
					Provider:      "deepseek",
					UpstreamModel: "deepseek-v4-pro",
				},
				AssistantContent:        "answer one",
				AssistantContentPresent: true,
				ReplayMessages:          firstReplay,
			}))

			deltaPrepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
				Model:              "deepseek-v4-pro",
				Instructions:       tc.instructions,
				ParallelToolCalls:  &parallel,
				PreviousResponseID: stringPtr("resp_serial_replay"),
				Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask two"}]}
		]`),
				Tools: fullReq.Tools,
			}, store, CodexGatewayDeepSeekRequestContext{
				SessionKey:   "session_serial_replay",
				IsolationKey: "iso_serial_replay",
			}, CodexGatewayDeepSeekRequestConfig{})
			require.NoError(t, err)

			require.Equal(t, fullPrepared.Body["messages"], deltaPrepared.Body["messages"])
			require.Equal(t, 1, countDeepSeekSerialToolInstructions(deltaPrepared.Body["messages"].([]any)))
			require.Equal(t, 1, countDeepSeekSerialToolInstructions(deltaPrepared.ReplayMessages))
		})
	}
}

func TestCodexGatewayDeepSeekRequest_StripsParallelToolCallsEvenWhenModelSupportsIt(t *testing.T) {
	parallel := true
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		ParallelToolCalls: &parallel,
	}
	model := CodexGatewayModel{
		Slug:                      "deepseek-v4-pro",
		Provider:                  "deepseek",
		UpstreamModel:             "deepseek-v4-pro",
		SupportsParallelToolCalls: true,
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_parallel",
		IsolationKey: "user_parallel",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.NotContains(t, prepared.Body, "parallel_tool_calls")
}

func TestCodexGatewayDeepSeekRequest_DefaultUserIDIsStableAcrossSessionsWithinIsolation(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	first, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_beta",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	third, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_2",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.Equal(t, first.Body["user_id"], second.Body["user_id"])
	require.NotEqual(t, first.Body["user_id"], third.Body["user_id"])
}

func TestCodexGatewayDeepSeekRequest_DefaultUserIDUsesWorkspaceScopeAndOptionalManagedSessionBucket(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	baseA, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	baseB, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_beta",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_alpha",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	otherWorkspace, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_1",
		WorkspaceKey: "workspace_beta",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	bucketA, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:           "session_alpha",
		IsolationKey:         "api_key_1",
		WorkspaceKey:         "workspace_alpha",
		ManagedSessionBucket: "managed_bucket_a",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	bucketB, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:           "session_alpha",
		IsolationKey:         "api_key_1",
		WorkspaceKey:         "workspace_alpha",
		ManagedSessionBucket: "managed_bucket_b",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.Equal(t, baseA.Body["user_id"], baseB.Body["user_id"])
	require.NotEqual(t, baseA.Body["user_id"], otherWorkspace.Body["user_id"])
	require.NotEqual(t, baseA.Body["user_id"], bucketA.Body["user_id"])
	require.NotEqual(t, bucketA.Body["user_id"], bucketB.Body["user_id"])
}

func TestCodexGatewayWorkspaceKeyIsStableAcrossMetadataOrderAndPathFormatting(t *testing.T) {
	headersA := http.Header{}
	headersA.Set("X-Codex-Turn-Metadata", `{
		"workspaces": {
			"/Users/example/project": {},
			"/Users/example/other/": {}
		}
	}`)
	headersB := http.Header{}
	headersB.Set("X-Codex-Turn-Metadata", `{
		"workspaces": {
			"  /Users/example/other  ": {},
			" /Users/example/project/ ": {}
		}
	}`)

	require.Equal(t, codexGatewayWorkspaceKey(headersA), codexGatewayWorkspaceKey(headersB))
}

func TestCodexGatewayWorkspaceKeyDiffersForDifferentWorkspaces(t *testing.T) {
	headersA := http.Header{}
	headersA.Set("X-Codex-Turn-Metadata", `{"workspaces":{"/Users/example/project":{}}}`)
	headersB := http.Header{}
	headersB.Set("X-Codex-Turn-Metadata", `{"workspaces":{"/Users/example/other":{}}}`)

	require.NotEqual(t, codexGatewayWorkspaceKey(headersA), codexGatewayWorkspaceKey(headersB))
}

func TestCodexGatewayManagedSessionBucketRequiresManagedHeadersAndKeepsSubagentsIsolated(t *testing.T) {
	metadata := `{"session_id":"logical-session-1","workspaces":{"/Users/example/project":{}}}`
	withoutManagedHeaders := http.Header{}
	withoutManagedHeaders.Set("X-Codex-Turn-Metadata", metadata)
	withParentHeader := http.Header{}
	withParentHeader.Set("X-Codex-Turn-Metadata", metadata)
	withParentHeader.Set("X-Codex-Parent-Thread-Id", "parent-thread-1")
	withSubagentHeader := http.Header{}
	withSubagentHeader.Set("X-Codex-Turn-Metadata", metadata)
	withSubagentHeader.Set("X-OpenAI-Subagent", "true")

	require.Empty(t, codexGatewayManagedSessionBucket(withoutManagedHeaders))
	require.NotEmpty(t, codexGatewayManagedSessionBucket(withParentHeader))
	require.Equal(t, codexGatewayManagedSessionBucket(withParentHeader), codexGatewayManagedSessionBucket(withSubagentHeader))
}

func TestCodexGatewayDeepSeekRequest_ManagedSessionDiagnosticsExposeScopeAndHashes(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	workspaceKey := codexGatewayWorkspaceKey(http.Header{
		"X-Codex-Turn-Metadata": []string{`{"workspaces":{"/Users/example/project":{}}}`},
	})
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "managed_scope_diag", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, trace)

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:           "logical-session-1",
		IsolationKey:         "api-key-1",
		WorkspaceKey:         workspaceKey,
		ManagedSessionBucket: "managed-bucket-1",
		CaptureTrace:         trace,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, prepared.Body["user_id"])

	deepseekDiag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	stable, ok := deepseekDiag["stable_serialization"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "actor_workspace_session_bucket", stable["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", stable["user_id_source"])
	require.Equal(t, "actor_workspace_session_bucket", deepseekDiag["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", deepseekDiag["user_id_source"])
	require.NotEmpty(t, deepseekDiag["user_id_hash"])
	require.NotEmpty(t, deepseekDiag["workspace_scope_hash"])
	require.NotEmpty(t, deepseekDiag["managed_session_bucket_hash"])
	require.Equal(t, deepseekDiag["user_id_hash"], trace.cacheUsage["user_id_hash"])
	require.Equal(t, deepseekDiag["workspace_scope_hash"], trace.cacheUsage["workspace_scope_hash"])
	require.Equal(t, deepseekDiag["managed_session_bucket_hash"], trace.cacheUsage["managed_session_bucket_hash"])
	require.Equal(t, "actor_workspace_session_bucket", trace.cacheUsage["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", trace.cacheUsage["user_id_source"])
}

func TestCodexGatewayDeepSeekRequest_ManagedSessionHeaderScopeIsDeterministicAndDiagnosed(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	metadata := `{"session_id":"logical-session-1","workspaces":{"/Users/example/project":{}}}`
	headersWithoutManagedScope := http.Header{}
	headersWithoutManagedScope.Set("X-Codex-Turn-Metadata", metadata)
	headersWithParentScope := http.Header{}
	headersWithParentScope.Set("X-Codex-Turn-Metadata", metadata)
	headersWithParentScope.Set("X-Codex-Parent-Thread-Id", "parent-thread-1")
	headersWithSubagentScope := http.Header{}
	headersWithSubagentScope.Set("X-Codex-Turn-Metadata", metadata)
	headersWithSubagentScope.Set("X-OpenAI-Subagent", "true")

	build := func(traceID string, headers http.Header) (CodexGatewayPreparedDeepSeekRequest, map[string]any) {
		trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: traceID, Provider: "deepseek", Model: "deepseek-v4-pro"})
		require.NotNil(t, trace)
		prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
			SessionKey:           "logical-session-1",
			IsolationKey:         "api-key-1",
			WorkspaceKey:         codexGatewayWorkspaceKey(headers),
			ManagedSessionBucket: codexGatewayManagedSessionBucket(headers),
			CaptureTrace:         trace,
		}, CodexGatewayDeepSeekRequestConfig{})
		require.NoError(t, err)
		deepseekDiag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
		require.True(t, ok)
		return prepared, deepseekDiag
	}

	basePrepared, baseDiag := build("managed_scope_without_headers", headersWithoutManagedScope)
	parentPrepared, parentDiag := build("managed_scope_parent", headersWithParentScope)
	subagentPrepared, subagentDiag := build("managed_scope_subagent", headersWithSubagentScope)

	require.NotEqual(t, basePrepared.Body["user_id"], parentPrepared.Body["user_id"])
	require.Equal(t, parentPrepared.Body["user_id"], subagentPrepared.Body["user_id"])
	require.Equal(t, "actor_workspace", baseDiag["user_id_scope"])
	require.Equal(t, "actor_workspace_session_bucket", parentDiag["user_id_scope"])
	require.Equal(t, "actor_workspace_session_bucket", subagentDiag["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", baseDiag["user_id_source"])
	require.Equal(t, "derived_actor_workspace", parentDiag["user_id_source"])
	require.Equal(t, "derived_actor_workspace", subagentDiag["user_id_source"])
	require.NotEmpty(t, baseDiag["workspace_scope_hash"])
	require.NotContains(t, baseDiag, "managed_session_bucket_hash")
	require.NotEmpty(t, parentDiag["managed_session_bucket_hash"])
	require.Equal(t, parentDiag["managed_session_bucket_hash"], subagentDiag["managed_session_bucket_hash"])
}

func TestCodexGatewayDeepSeekRequest_StateMissDiagnosticsIncludeUserScope(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	previousID := "resp_missing_scope_diag"
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "state_miss_scope_diag", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, trace)
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: &previousID,
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now}), CodexGatewayDeepSeekRequestContext{
		SessionKey:           "session_miss_diag",
		IsolationKey:         "api-key-1",
		WorkspaceKey:         "workspace_miss_diag",
		ManagedSessionBucket: "bucket_miss_diag",
		CaptureTrace:         trace,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.ErrorIs(t, err, ErrCodexGatewayStateNotFound)

	deepseekDiag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, deepseekDiag["previous_response_id_present"])
	require.Equal(t, "miss", deepseekDiag["state_lookup_status"])
	require.Equal(t, "none", deepseekDiag["previous_response_replay_mode"])
	require.NotEmpty(t, deepseekDiag["user_id_hash"])
	require.NotEmpty(t, deepseekDiag["workspace_scope_hash"])
	require.NotEmpty(t, deepseekDiag["managed_session_bucket_hash"])
	require.Equal(t, "actor_workspace_session_bucket", deepseekDiag["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", deepseekDiag["user_id_source"])
}

func TestCodexGatewayDeepSeekRequest_HostedToolMappingIsStableAcrossHostedToolOrder(t *testing.T) {
	reqA := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"web_search"},
			{"type":"image_generation"},
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	reqB := reqA
	reqB.Tools = json.RawMessage(`[
		{"type":"image_generation"},
		{"type":"web_search"},
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
	]`)
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	ctx := CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "api_key_1",
	}

	first, err := BuildCodexGatewayDeepSeekRequest(model, reqA, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayDeepSeekRequest(model, reqB, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	firstTools := first.Body["tools"].([]any)
	secondTools := second.Body["tools"].([]any)
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, firstTools, "web_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, secondTools, "web_search"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, firstTools, "exec_command"))
	require.NotNil(t, deepSeekRequestToolFunctionByName(t, secondTools, "exec_command"))
	require.Contains(t, first.Body["messages"].([]any)[0].(map[string]any)["content"], "image_generation")
	require.Contains(t, second.Body["messages"].([]any)[0].(map[string]any)["content"], "image_generation")
}

func TestCodexGatewayDeepSeekRequest_EquivalentToolOrderHasStableToolSchemaHash(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	baseReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
	}
	reqA := baseReq
	reqA.Tools = json.RawMessage(`[
		{"type":"namespace","name":"browser","tools":[
			{"type":"function","name":"open","parameters":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}},
			{"type":"function","name":"click","parameters":{"type":"object","properties":{"y":{"type":"number"},"x":{"type":"number"}},"required":["y","x"]}}
		]},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar"}},
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"},"cwd":{"type":"string"}},"required":["cwd","cmd"]}},
		{"type":"web_search"}
	]`)
	reqB := baseReq
	reqB.Tools = json.RawMessage(`[
		{"type":"web_search"},
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cwd":{"type":"string"},"cmd":{"type":"string"}},"required":["cmd","cwd"]}},
		{"type":"namespace","name":"browser","tools":[
			{"type":"function","name":"click","parameters":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}},
			{"type":"function","name":"open","parameters":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}}
		]},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
	]`)

	build := func(traceID string, req CodexGatewayResponsesCreateRequest) (CodexGatewayPreparedDeepSeekRequest, map[string]any) {
		trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: traceID, Provider: "deepseek", Model: "deepseek-v4-pro"})
		require.NotNil(t, trace)
		prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
			SessionKey:   "stable_tool_session",
			IsolationKey: "stable_tool_actor",
			CaptureTrace: trace,
		}, CodexGatewayDeepSeekRequestConfig{})
		require.NoError(t, err)
		diag, ok := trace.requestDiag["deepseek_cache"].(map[string]any)
		require.True(t, ok)
		return prepared, diag
	}

	first, firstDiag := build("stable_tools_a", reqA)
	second, secondDiag := build("stable_tools_b", reqB)

	require.Equal(t, mustMarshalJSON(t, first.Body["tools"]), mustMarshalJSON(t, second.Body["tools"]))
	require.NotEmpty(t, firstDiag["tool_schema_hash"])
	require.Equal(t, firstDiag["tool_schema_hash"], secondDiag["tool_schema_hash"])
}

func TestCodexGatewayDeepSeekRequest_BodyIsDeterministicForCachePrefix(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Act as a coding agent."`),
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Use local tools when useful."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect the project"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"computer-use","tools":[
				{"name":"click","parameters":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}}}},
				{"name":"press_key","parameters":{"type":"object","properties":{"key":{"type":"string"}}}}
			]},
			{"type":"custom","name":"apply_patch","format":{"type":"grammar"}},
			{"type":"web_search"}
		]`),
		Reasoning: json.RawMessage(`{"effort":"xhigh"}`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	ctx := CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "api_key_1",
	}

	first, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, ctx, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	firstJSON, err := json.Marshal(first.Body)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second.Body)
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(secondJSON))
	require.Equal(t, string(firstJSON), string(secondJSON))
}

func TestCodexGatewayDeepSeekRequest_CaptureDiagnosticsExcludeVolatileFields(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	streamTrue := true
	reqA := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Keep answers short."`),
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Use local tools when possible."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect the repository"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}},
			{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
		]`),
		Stream: &streamTrue,
		RawFields: map[string]json.RawMessage{
			"stream_options": json.RawMessage(`{"include_usage":true}`),
		},
	}
	traceA := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "diag_a", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, traceA)
	_, err := BuildCodexGatewayDeepSeekRequest(model, reqA, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_a",
		IsolationKey: "isolation_a",
		CaptureTrace: traceA,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	deepseekA, ok := traceA.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	stableA, ok := deepseekA["stable_serialization"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "deepseek-cache-v1", stableA["version"])
	require.ElementsMatch(t, []string{"stream", "stream_options", "user_id"}, stableA["request_shape_excluded_fields"])
	require.Equal(t, 8, stableA["message_prefix_limit"])
	require.Equal(t, "actor_workspace", stableA["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", stableA["user_id_source"])
	require.NotEmpty(t, deepseekA["request_prefix_hash"])
	require.NotEmpty(t, deepseekA["tool_schema_hash"])
	require.NotEmpty(t, deepseekA["message_prefix_hash"])
	require.NotEmpty(t, deepseekA["request_shape_hash"])
	require.NotEmpty(t, deepseekA["user_id_hash"])
	require.Equal(t, deepseekA["request_prefix_hash"], traceA.cacheUsage["request_prefix_hash"])
	require.Equal(t, deepseekA["tool_schema_hash"], traceA.cacheUsage["tool_schema_hash"])
	require.Equal(t, deepseekA["message_prefix_hash"], traceA.cacheUsage["message_prefix_hash"])
	require.Equal(t, deepseekA["request_shape_hash"], traceA.cacheUsage["request_shape_hash"])
	require.Equal(t, deepseekA["user_id_hash"], traceA.cacheUsage["user_id_hash"])
	require.Equal(t, "actor_workspace", traceA.cacheUsage["user_id_scope"])
	require.Equal(t, "derived_actor_workspace", traceA.cacheUsage["user_id_source"])

	streamFalse := false
	reqB := reqA
	reqB.Stream = &streamFalse
	reqB.RawFields = map[string]json.RawMessage{
		"stream_options": json.RawMessage(`{"include_usage":false,"reason":"client_toggle"}`),
	}
	traceB := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "diag_b", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, traceB)
	_, err = BuildCodexGatewayDeepSeekRequest(model, reqB, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_b",
		IsolationKey: "isolation_b",
		CaptureTrace: traceB,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	deepseekB, ok := traceB.requestDiag["deepseek_cache"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, deepseekA["request_shape_hash"], deepseekB["request_shape_hash"])
	require.Equal(t, deepseekA["tool_schema_hash"], deepseekB["tool_schema_hash"])
	require.Equal(t, deepseekA["message_prefix_hash"], deepseekB["message_prefix_hash"])
	require.NotEqual(t, deepseekA["user_id_hash"], deepseekB["user_id_hash"])
}

func TestCodexGatewayDeepSeekRequest_CaptureDiagnosticsRecordToolOutputSummary(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                true,
		BaseDir:                baseDir,
		HashKeyFile:            keyPath,
		CorrelationHashKeyFile: keyPath,
	})
	defer manager.Close()

	largeScreenshot := "data:image/png;base64," + strings.Repeat("E", 90000)
	accessibilityTree := strings.Repeat("staticText filler\n", 180) + strings.Join([]string{
		`text input "Reply to assistant" focused element_index=reply`,
		`button "Lower screen action" enabled element_index=lower-action`,
	}, "\n")
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"Codex\"}"},
			{"type":"function_call_output","call_id":"call_state","output":{"screenshot":%q,"accessibility_tree":%q,"status":"ok"}}
		]`, largeScreenshot, accessibilityTree)),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			return "Codex screenshot shows lower-screen reply controls.", nil
		},
	}
	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, nil, CodexGatewayDeepSeekRequestContext{}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "tool_output_summary", Provider: "deepseek", Model: "deepseek-v4-pro"})
	require.NotNil(t, trace)
	_, err = BuildCodexGatewayDeepSeekRequest(model, rewritten, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_tool_output_summary",
		IsolationKey: "isolation_tool_output_summary",
		CaptureTrace: trace,
	}, cfg)
	require.NoError(t, err)

	summary, ok := trace.requestDiag["deepseek_tool_output_summary"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "get_app_state", summary["tool_name"])
	require.Greater(t, int(summary["raw_chars"].(int)), 90000)
	require.Greater(t, int(summary["normalized_chars"].(int)), 0)
	require.ElementsMatch(t, []string{"accessibility_tree", "computer_screenshot"}, summary["classes"])
	require.GreaterOrEqual(t, summary["operable_line_count"], 1)
	require.Equal(t, false, summary["fallback_preview_only"])
	require.NotContains(t, fmtAny(summary), "Reply to assistant")
	require.NotContains(t, fmtAny(summary), strings.Repeat("E", 128))
}

func TestCodexGatewayDeepSeekRequest_NormalizesDeveloperRoleForChatCompletions(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Always be concise."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply OK."}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Contains(t, messages[0].(map[string]any)["content"], "Always be concise.")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_NormalizesLatestReminderRoleForChatCompletions(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"latest_reminder","content":[{"type":"input_text","text":"Keep the most recent user instruction in force."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply OK."}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Contains(t, messages[0].(map[string]any)["content"], "Keep the most recent user instruction")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekPersistState_AllowsOrdinaryAssistantTurns(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	err := codexGatewayDeepSeekPersistState(
		store,
		"resp_ordinary_persist",
		"deepseek-v4-pro",
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_ordinary_persist", IsolationKey: "iso_ordinary_persist"},
		"plain answer",
		true,
		"ordinary reasoning",
		true,
		false,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	state, err := store.Get(CodexGatewayStateLookupKey{
		ResponseID:    "resp_ordinary_persist",
		SessionKey:    "session_ordinary_persist",
		IsolationKey:  "iso_ordinary_persist",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	})
	require.NoError(t, err)
	require.Equal(t, "plain answer", state.AssistantContent)
	require.True(t, state.AssistantContentPresent)
	require.Equal(t, "ordinary reasoning", state.ReasoningContent)
	require.True(t, state.ReasoningContentPresent)
	require.Len(t, state.ReplayMessages, 1)
}

func TestCodexGatewayDeepSeekPersistState_SkipsEmptyAssistantTurns(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	err := codexGatewayDeepSeekPersistState(
		store,
		"resp_empty_persist",
		"deepseek-v4-pro",
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_empty_persist", IsolationKey: "iso_empty_persist"},
		"",
		true,
		"",
		false,
		false,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	_, err = store.Get(CodexGatewayStateLookupKey{
		ResponseID:    "resp_empty_persist",
		SessionKey:    "session_empty_persist",
		IsolationKey:  "iso_empty_persist",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	})
	require.ErrorIs(t, err, ErrCodexGatewayStateNotFound)
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousOrdinaryAssistantState(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_ordinary_replay",
			SessionKey:    "session_ordinary_replay",
			IsolationKey:  "iso_ordinary_replay",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "plain answer",
		AssistantContentPresent: true,
		ReasoningContent:        "ordinary reasoning",
		ReasoningContentPresent: true,
	}))

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_ordinary_replay"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{SessionKey: "session_ordinary_replay", IsolationKey: "iso_ordinary_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "plain answer", assistant["content"])
	require.Equal(t, "ordinary reasoning", assistant["reasoning_content"])
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousResponseFullMessagesPrefix(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	replay := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"follow developer instructions"}`),
		json.RawMessage(`{"role":"user","content":"first prompt"}`),
		json.RawMessage(`{"role":"assistant","content":"first answer","reasoning_content":"first reasoning"}`),
		json.RawMessage(`{"role":"user","content":"second prompt"}`),
		json.RawMessage(`{"role":"assistant","content":"second answer","reasoning_content":"second reasoning"}`),
	}
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_full_replay",
			SessionKey:    "session_full_replay",
			IsolationKey:  "iso_full_replay",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "second answer",
		AssistantContentPresent: true,
		ReasoningContent:        "second reasoning",
		ReasoningContentPresent: true,
		ReplayMessages:          replay,
	}))

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_full_replay"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"third prompt"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{SessionKey: "session_full_replay", IsolationKey: "iso_full_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 6)
	for i, expected := range replay {
		var want map[string]any
		require.NoError(t, json.Unmarshal(expected, &want))
		require.Equal(t, want, messages[i].(map[string]any))
	}
	require.Equal(t, "third prompt", messages[5].(map[string]any)["content"])
}

func TestCodexGatewayDeepSeekRequest_PreviousResponseDeltaMatchesFullReplayPrefix(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	replay := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"ask one"}`),
		json.RawMessage(`{"role":"assistant","content":"answer one","reasoning_content":"reason one"}`),
	}
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_delta_replay",
			SessionKey:    "session_delta_replay",
			IsolationKey:  "iso_delta_replay",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "answer one",
		AssistantContentPresent: true,
		ReasoningContent:        "reason one",
		ReasoningContentPresent: true,
		ReplayMessages:          replay,
	}))

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	fullReq := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask one"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer one"}],"reasoning_content":"reason one"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask two"}]}
		]`),
	}
	fullPrepared, err := BuildCodexGatewayDeepSeekRequest(model, fullReq, nil, CodexGatewayDeepSeekRequestContext{SessionKey: "session_delta_replay", IsolationKey: "iso_delta_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	deltaPrepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_delta_replay"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"ask two"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{SessionKey: "session_delta_replay", IsolationKey: "iso_delta_replay"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.Equal(t, fullPrepared.Body["messages"], deltaPrepared.Body["messages"])
}

func TestCodexGatewayDeepSeekRequest_StructuredFunctionOutputArrayInputImageUsesDeterministicPlaceholder(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{"type": "function_call", "call_id": "call_img", "name": "capture_screen", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": "call_img", "output": []any{
				map[string]any{"type": "input_text", "text": "screenshot follows"},
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64,QUJDRA==", "detail": "high"},
			}},
		}),
		Tools: json.RawMessage(`[{"type":"function","name":"capture_screen","parameters":{"type":"object"}}]`),
	}
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_structured_image_output",
		IsolationKey: "iso_structured_image_output",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	content := gjson.GetBytes(raw, "messages.1.content").String()
	require.Contains(t, content, "screenshot follows")
	require.Contains(t, content, "binary_or_image")
	require.Contains(t, content, "sha256")
	require.NotContains(t, content, "QUJDRA==")
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousToolLoopStateAndNormalizesOutputs(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_prev",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need tool result",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_1", Type: CodexGatewayToolKindNamespace, Name: "open-page", Arguments: `{"url":"https://example.com"}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"browser__open-page": {Alias: "browser__open-page", Kind: CodexGatewayToolKindNamespace, Namespace: "browser", Name: "open-page"},
		},
	}))

	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_prev"),
		Input: json.RawMessage(`[
			{"type":"function_call_output","call_id":"call_1","output":{"ok":true,"url":"https://example.com"}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"summarize the page"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
		UserID:       "stable_user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)

	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "", assistant["content"])
	require.Equal(t, "need tool result", assistant["reasoning_content"])
	require.Equal(t, "browser__open-page", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	toolMessage := messages[1].(map[string]any)
	require.Equal(t, "tool", toolMessage["role"])
	require.Equal(t, "call_1", toolMessage["tool_call_id"])
	require.Equal(t, `{"ok":true,"url":"https://example.com"}`, toolMessage["content"])
}

func TestCodexGatewayDeepSeekRequest_CoalescesConsecutiveFunctionCallsBeforeOutputs(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_00","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call","call_id":"call_01","name":"exec_command","arguments":"{\"cmd\":\"rg --files\"}"},
			{"type":"function_call","call_id":"call_02","name":"exec_command","arguments":"{\"cmd\":\"git status --short\"}"},
			{"type":"function_call_output","call_id":"call_00","output":"pwd output"},
			{"type":"function_call_output","call_id":"call_01","output":"rg output"},
			{"type":"function_call_output","call_id":"call_02","output":"git output"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 4)

	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "", assistant["content"])
	calls := assistant["tool_calls"].([]any)
	require.Len(t, calls, 3)
	require.Equal(t, "call_00", calls[0].(map[string]any)["id"])
	require.Equal(t, "call_01", calls[1].(map[string]any)["id"])
	require.Equal(t, "call_02", calls[2].(map[string]any)["id"])

	for i, expectedCallID := range []string{"call_00", "call_01", "call_02"} {
		toolMessage := messages[i+1].(map[string]any)
		require.Equal(t, "tool", toolMessage["role"])
		require.Equal(t, expectedCallID, toolMessage["tool_call_id"])
	}
}

func TestCodexGatewayDeepSeekRequest_CoalescesConsecutiveFunctionCallReasoning(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_00","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}","reasoning_content":"first tool reason"},
			{"type":"function_call","call_id":"call_01","name":"exec_command","arguments":"{\"cmd\":\"rg --files\"}","reasoning_content":"second tool reason"},
			{"type":"function_call_output","call_id":"call_00","output":"pwd output"},
			{"type":"function_call_output","call_id":"call_01","output":"rg output"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_tool_reasoning",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "first tool reason\nsecond tool reason", assistant["reasoning_content"])
}

func TestCodexGatewayDeepSeekRequest_PreservesResponsesReasoningItems(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"reasoning","summary_text":"first thought","content":[{"type":"summary_text","text":"second thought"},{"type":"text","text":"third thought"},{"text":"fourth thought"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_reasoning_item",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "", assistant["content"])
	require.Equal(t, "first thought\nsecond thought\nthird thought\nfourth thought", assistant["reasoning_content"])
	require.NotContains(t, assistant["content"], "thought")
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_IgnoresEmptyResponsesReasoningItems(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"reasoning","summary":[],"content":null},
			{"type":"reasoning","content":[{"type":"text","text":"   "}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_empty_reasoning_item",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].(map[string]any)["role"])
	require.NotContains(t, fmt.Sprint(messages), "reasoning_content")
}

func TestCodexGatewayDeepSeekRequest_PreservesReasoningBeforeFunctionCallOutput(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"reasoning","content":"need cwd before answering"},
			{"type":"function_call","call_id":"call_1","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"/tmp/project"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_reasoning_before_tool_output",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "need cwd before answering", assistant["reasoning_content"])
	require.NotContains(t, assistant["content"], "need cwd")
	require.Len(t, assistant["tool_calls"].([]any), 1)
	require.Equal(t, "tool", messages[1].(map[string]any)["role"])
}

func TestCodexGatewayDeepSeekRequest_ReplaysPreviousCustomToolLoopState(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_custom",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need to patch",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_patch", Type: CodexGatewayToolKindCustom, Name: "apply_patch", Arguments: "*** Begin Patch\n*** End Patch\n"},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"custom__apply_patch": {Alias: "custom__apply_patch", Kind: CodexGatewayToolKindCustom, Name: "apply_patch"},
		},
	}))

	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_custom"),
		Input: json.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_patch","name":"apply_patch","output":"patch applied"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)

	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "custom__apply_patch", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	toolMessage := messages[1].(map[string]any)
	require.Equal(t, "tool", toolMessage["role"])
	require.Equal(t, "call_patch", toolMessage["tool_call_id"])
	require.Equal(t, "patch applied", toolMessage["content"])
}

func TestCodexGatewayDeepSeekRequest_ConvertsInlineCustomToolCallsAndOutputs(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_patch","name":"apply_patch","input":"*** Begin Patch\n*** End Patch\n"},
			{"type":"custom_tool_call_output","call_id":"call_patch","output":{"ok":true}}
		]`),
		Tools: json.RawMessage(`[
			{"type":"custom","name":"apply_patch","description":"edit files","format":{"type":"grammar"}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "custom__edit", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])
	require.Equal(t, "*** Begin Patch\n*** End Patch\n", assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"])
	require.Equal(t, `{"ok":true}`, messages[1].(map[string]any)["content"])
}

func TestCodexGatewayDeepSeekRequest_DropsStaleToolChoiceWhenReplayRequestHasNoTools(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_custom",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need to patch",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_patch", Type: CodexGatewayToolKindCustom, Alias: "custom__edit", Name: "edit", Arguments: "*** Begin Patch\n*** End Patch\n"},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"custom__edit": {Alias: "custom__edit", Kind: CodexGatewayToolKindCustom, Name: "edit"},
		},
	}))

	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_custom"),
		ToolChoice:         json.RawMessage(`"edit"`),
		Input: json.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_patch","name":"edit","output":"patch applied"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "tool_choice").Exists())
	require.Equal(t, "custom__edit", gjson.GetBytes(raw, "messages.0.tool_calls.0.function.name").String())
}

func TestCodexGatewayDeepSeekRequest_RestoresReplayToolSchemasWhenCurrentRequestHasNoTools(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_computer",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need to click",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_click", Type: CodexGatewayToolKindNamespace, Alias: "mcp__computer_use__click", Name: "click", Arguments: `{"x":1,"y":2}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"mcp__computer_use__click": {Alias: "mcp__computer_use__click", Kind: CodexGatewayToolKindNamespace, NamespacePath: "mcp__computer_use__", Name: "click"},
		},
		ToolSchemas: []json.RawMessage{
			json.RawMessage(`{"type":"function","function":{"name":"mcp__computer_use__click","description":"click","parameters":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}}}`),
		},
	}))

	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_computer"),
		Input: json.RawMessage(`[
			{"type":"function_call_output","call_id":"call_click","output":"clicked"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "mcp__computer_use__click", gjson.GetBytes(raw, "tools.0.function.name").String())
	require.Equal(t, "x", gjson.GetBytes(raw, "tools.0.function.parameters.required.0").String())
	require.Equal(t, "y", gjson.GetBytes(raw, "tools.0.function.parameters.required.1").String())
}

func TestCodexGatewayDeepSeekRequest_DoesNotRestoreReplayToolSchemasForFreshRequests(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_previous",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "",
		AssistantContentPresent: true,
		ReasoningContent:        "need to click",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_click", Type: CodexGatewayToolKindNamespace, Alias: "mcp__computer_use__click", Name: "click", Arguments: `{"x":1,"y":2}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"mcp__computer_use__click": {Alias: "mcp__computer_use__click", Kind: CodexGatewayToolKindNamespace, NamespacePath: "mcp__computer_use__", Name: "click"},
		},
		ToolSchemas: []json.RawMessage{
			json.RawMessage(`{"type":"function","function":{"name":"mcp__computer_use__click","description":"click","parameters":{"type":"object"}}}`),
		},
	}))
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"fresh request without tools"}]}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "tools").Exists())
}

func TestCodexGatewayDeepSeekStateReplayMessages_DoesNotAccumulateFullHistoryForToolLoops(t *testing.T) {
	largeHistory := json.RawMessage(`{"role":"user","content":"task: click the Run button\n` + strings.Repeat("x", 20000) + `"}`)
	toolCalls := []CodexGatewayStoredToolCall{
		{ID: "call_click", Type: CodexGatewayToolKindNamespace, Alias: "mcp__computer_use__click", Name: "click", Arguments: `{"x":1,"y":2}`},
	}
	toolNameMap := map[string]CodexGatewayToolNameMapEntry{
		"mcp__computer_use__click": {Alias: "mcp__computer_use__click", Kind: CodexGatewayToolKindNamespace, NamespacePath: "mcp__computer_use__", Name: "click"},
	}

	replay := codexGatewayDeepSeekStateReplayMessages(
		[]json.RawMessage{largeHistory},
		"",
		true,
		"need to click",
		true,
		false,
		toolCalls,
		toolNameMap,
	)

	require.Len(t, replay, 2)
	require.Contains(t, string(replay[0]), `"role":"user"`)
	require.Contains(t, string(replay[0]), "task: click the Run button")
	require.Less(t, len(replay[0]), 3000)
	require.NotContains(t, string(replay[0]), strings.Repeat("x", 2048))
	require.Contains(t, string(replay[1]), `"tool_calls"`)
	require.Contains(t, string(replay[1]), `"mcp__computer_use__click"`)
}

func TestCodexGatewayAgnesRequest_ComputerUseAddsAgnesContinuationStrategy(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Use Computer Use to ask Doubao, then write Notes."}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"list_apps","parameters":{"type":"object","properties":{}}},
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}},
				{"type":"function","name":"set_value","parameters":{"type":"object","properties":{"app":{"type":"string"},"element_index":{"type":"string"},"value":{"type":"string"}},"required":["app","element_index","value"]}},
				{"type":"function","name":"press_key","parameters":{"type":"object","properties":{"app":{"type":"string"},"key":{"type":"string"}},"required":["app","key"]}},
				{"type":"function","name":"scroll","parameters":{"type":"object","properties":{"app":{"type":"string"},"element_index":{"type":"string"},"direction":{"type":"string"}},"required":["app","element_index","direction"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_agnes_cu_strategy",
		IsolationKey: "iso_agnes_cu_strategy",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai"})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.GreaterOrEqual(t, len(messages), 2)
	instruction := messages[0].(map[string]any)["content"].(string)
	require.Contains(t, instruction, "AGNES Computer Use continuation")
	require.Contains(t, instruction, "Do not stop after saying")
	require.Contains(t, instruction, "Every new turn or Continue")
	require.Contains(t, instruction, "get_app_state")
	require.Contains(t, instruction, "Electron")
	require.Contains(t, instruction, "set_value")
	require.Contains(t, instruction, "Return")
	require.Equal(t, "user", messages[len(messages)-1].(map[string]any)["role"])
	require.Equal(t, map[string]any{"enable_thinking": false}, prepared.Body["chat_template_kwargs"])
}

func TestCodexGatewayDeepSeekRequest_ComputerUseVisibleTextKeepsEnoughDoubaoAnswer(t *testing.T) {
	appState := "Computer Use state (CUA App Version: 799)\n<app_state>\n" +
		"App=/Applications/Doubao.app/ (bundleID com.bot.pc.doubao, pid 750)\n" +
		"Window: \"豆包\", App: 豆包.\n" +
		strings.Repeat("\t30 link Description: 历史对话, Value: chrome://doubao-chat/chat/sidebar\n", 30) +
		strings.Join([]string{
			"\t93 标题 一、特色小吃（市井烟火，必尝经典）, Value: 3",
			"\t107 text 合记烩面（人民路店） ：国营老牌，汤鲜面厚，本地人从小吃到大，人均 22-30 元 携程 。",
			"\t124 text 推荐店 ： 方中山胡辣汤（多分店） ：郑州顶流，麻香够味，外地人选微辣，人均 10 元左右 携程 。",
			"\t134 text 推荐店 ： 京都老蔡记（德化街店） ：“老三记” 之首，人均 35 元，非遗味道。",
			"\t144 text 推荐店 ： 葛记焖饼（伏牛路总店） ：百年老店，人均 30 元，配绿豆沙解腻。",
			"\t161 标题 二、经典豫菜餐厅（正统豫味，宴请首选）, Value: 3",
			"\t178 text 特色 ：1912 年始创，中华老字号，宫廷豫菜代表，清汤酸辣乌鱼蛋汤为豫菜五大名羹之一。",
			"\t216 text 亮点 ：平价豫菜天花板，烙馍、小米粥免费无限续，69 元整只烤鸭性价比炸裂。",
			"\t228 text 亮点 ：郑州本土火锅，火遍全国，毛肚脆嫩、菌汤锅底封神，绣球菌、乌鸡卷必点。",
			"\t240 text 亮点 ：郑州美食名片，改良版叫花鸡，卤制枣红，筋道清香。",
			"\t249 text 亮点 ：十余年老牌，灌汤包皮薄馅足，汤汁鲜甜，咬开爆汁，配小米粥，人均 30 元。",
			"\t258 text 早餐 ：方中山胡辣汤 + 油馍头 → 老蔡记蒸饺 + 鸡丝馄饨",
			"\t501 文本输入区 (settable, string) 发消息...",
			"\t502 按钮 发送",
		}, "\n") + "\n</app_state>"
	req := CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{"type": "function_call", "call_id": "call_state", "name": "mcp__computer_use__get_app_state", "arguments": `{"app":"com.bot.pc.doubao"}`},
			map[string]any{"type": "function_call_output", "call_id": "call_state", "output": []any{
				map[string]any{"type": "input_text", "text": appState},
				map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + strings.Repeat("A", 90000), "detail": "high"},
			}},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_doubao_visible_text",
		IsolationKey: "iso_doubao_visible_text",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai"})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	if toolContent == "" {
		toolContent = gjson.GetBytes(raw, "messages.2.content").String()
	}
	visibleText := gjson.Get(toolContent, "0.text.visible_text").Raw
	if visibleText == "" {
		visibleText = gjson.Get(toolContent, "1.text.visible_text").Raw
	}
	require.Contains(t, visibleText, "合记烩面")
	require.Contains(t, visibleText, "方中山胡辣汤")
	require.Contains(t, visibleText, "京都老蔡记")
	require.Contains(t, visibleText, "葛记焖饼")
	require.Contains(t, visibleText, "灌汤包")
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
	require.LessOrEqual(t, len(toolContent), codexGatewayDeepSeekToolOutputMaxChars+256)
}

func TestCodexGatewayDeepSeekRequest_ComputerUseHighFidelityVisibleTextBudget(t *testing.T) {
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
	req := CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{"type": "function_call", "call_id": "call_state", "name": "mcp__computer_use__get_app_state", "arguments": `{"app":"com.bot.pc.doubao"}`},
			map[string]any{"type": "function_call_output", "call_id": "call_state", "output": []any{
				map[string]any{"type": "input_text", "text": "Wall time: 0.8 seconds\nOutput:"},
				map[string]any{"type": "input_text", "text": app.String()},
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + strings.Repeat("A", 180000), "detail": "high"},
			}},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash"}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_high_fidelity_visible_text",
		IsolationKey: "iso_high_fidelity_visible_text",
		Provider:     "agnes",
	}, CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai"})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	visibleText := gjson.Get(toolContent, "1.text.visible_text").Raw
	if visibleText == "" {
		visibleText = gjson.Get(toolContent, "0.text.visible_text").Raw
	}
	for _, want := range facts {
		require.Contains(t, visibleText, strings.Split(want, "：")[0], "visible_text should preserve broad answer coverage")
	}
	require.Contains(t, gjson.Get(toolContent, "1.text.operable_lines").Raw+gjson.Get(toolContent, "0.text.operable_lines").Raw, "文本输入区")
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
	require.LessOrEqual(t, len(toolContent), codexGatewayDeepSeekToolOutputMaxChars+512)
}

func TestCodexGatewayDeepSeekRequest_SummarizesLargeComputerUseToolOutput(t *testing.T) {
	largeScreenshot := "data:image/png;base64," + strings.Repeat("A", 20000)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"Codex\"}"},
			{"type":"function_call_output","call_id":"call_state","output":{"screenshot":%q,"accessibility_tree":%q,"status":"ok"}}
		]`, largeScreenshot, strings.Repeat("node ", 5000))),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, largeScreenshot)
	require.NotContains(t, toolContent, strings.Repeat("node ", 128))
	require.Contains(t, toolContent, "truncated")
	require.Less(t, len(toolContent), 4096)
}

func TestCodexGatewayDeepSeekRequest_SummarizesPageTreeVariants(t *testing.T) {
	pageTree := strings.Repeat(`role=button name="Continue" enabled
role=textbox name="Search"
node node node node node node node node node node
`, 200)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_page","name":"mcp__browser__snapshot","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_page","output":{"page_tree":%q,"accessibilitySnapshot":%q,"domSnapshot":%q,"status":"ok"}}
		]`, pageTree, pageTree, pageTree)),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__browser__","tools":[
				{"type":"function","name":"snapshot","parameters":{"type":"object"}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, strings.Repeat("node ", 32))
	require.Contains(t, toolContent, "truncated")
	require.Contains(t, toolContent, "sha256")
	require.Less(t, len(toolContent), 4096)
}

func TestCodexGatewayDeepSeekRequest_SummarizesStructuredComputerUseState(t *testing.T) {
	nodes := make([]any, 0, 80)
	for i := 0; i < 80; i++ {
		nodes = append(nodes, map[string]any{
			"role":          "button",
			"name":          fmt.Sprintf("Run %02d", i),
			"enabled":       true,
			"element_index": fmt.Sprintf("el-%02d", i),
			"bounds":        map[string]any{"x": i, "y": i + 1, "width": 120, "height": 32},
			"description":   strings.Repeat("node ", 80),
		})
	}
	output := map[string]any{
		"accessibility_tree": nodes,
		"domSnapshot": map[string]any{
			"nodes": nodes,
		},
		"status": "ok",
	}
	outputText, err := json.Marshal(output)
	require.NoError(t, err)
	input, err := json.Marshal([]any{
		map[string]any{
			"type":      "function_call",
			"call_id":   "call_state",
			"name":      "mcp__computer_use__get_app_state",
			"arguments": `{"app":"Codex"}`,
		},
		map[string]any{
			"type":    "function_call_output",
			"call_id": "call_state",
			"output":  string(outputText),
		},
	})
	require.NoError(t, err)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: input,
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, strings.Repeat("node ", 16))
	require.Contains(t, toolContent, "accessibility_tree")
	require.Contains(t, toolContent, "visual_tree")
	require.Contains(t, toolContent, "operable_lines")
	require.Contains(t, toolContent, "Run 00")
	require.Contains(t, toolContent, "element_index=el-00")
	require.Contains(t, toolContent, "bounds=")
	require.Less(t, len(toolContent), 4096)
}

func TestCodexGatewayDeepSeekRequest_SummarizesStandaloneAccessibilityTreeOutput(t *testing.T) {
	tree := strings.Repeat(`AXWindow "Codex" bounds={0,0,900,700}
AXButton "Run" enabled element_index=run
AXTextField "Prompt" focused bounds={20,60,500,40}
AXButton "Stop" disabled
`, 80)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"Codex\"}"},
			{"type":"function_call_output","call_id":"call_state","output":%q}
		]`, tree)),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, strings.Repeat(`AXWindow "Codex"`, 2))
	require.Contains(t, toolContent, "accessibility_tree")
	require.Contains(t, toolContent, "operable_lines")
	require.Contains(t, toolContent, "AXButton")
	require.Less(t, len(toolContent), 4096)
}

func TestCodexGatewayDeepSeekRequest_SummarizesShortStandaloneAccessibilityTreeOutput(t *testing.T) {
	tree := `AXWindow "Codex" bounds={0,0,900,700}
AXButton "Run" enabled element_index=run
AXTextField "Prompt" focused bounds={20,60,500,40}
AXButton "Stop" disabled`
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"Codex\"}"},
			{"type":"function_call_output","call_id":"call_state","output":%q}
		]`, tree)),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotEqual(t, tree, toolContent)
	require.Contains(t, toolContent, "accessibility_tree")
	require.Contains(t, toolContent, "operable_lines")
	require.Contains(t, toolContent, "element_index=run")
}

func TestCodexGatewayDeepSeekRequest_DoesNotSummarizeOrdinaryMultilineTextOutput(t *testing.T) {
	output := strings.Repeat("build log line without UI tree markers\n", 20)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_log","name":"read_log","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_log","output":%q}
		]`, output)),
		Tools: json.RawMessage(`[
			{"type":"function","name":"read_log","parameters":{"type":"object"}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.Equal(t, output, toolContent)
	require.NotContains(t, toolContent, "accessibility_tree")
}

func TestCodexGatewayDeepSeekRequest_DoesNotSummarizeOrdinaryDomainFields(t *testing.T) {
	domainItems := make([]any, 0, 32)
	for i := 0; i < 32; i++ {
		domainItems = append(domainItems, map[string]any{
			"domain": fmt.Sprintf("service-%02d.example.com", i),
			"random": strings.Repeat("value ", 40),
		})
	}
	output := map[string]any{
		"domain_results": domainItems,
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{"type": "function_call", "call_id": "call_domains", "name": "domain_lookup", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": "call_domains", "output": output},
		}),
		Tools: json.RawMessage(`[
			{"type":"function","name":"domain_lookup","parameters":{"type":"object"}}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, "visual_tree")
	require.Contains(t, toolContent, "service-31.example.com")
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_RewritesComputerUseOutputForNativeOperation(t *testing.T) {
	largeScreenshot := "data:image/png;base64," + strings.Repeat("B", 20000)
	accessibilityTree := strings.Repeat(`window "Codex"
button "Run" enabled focused
textbox "Prompt" value ""
button "Stop" disabled
staticText "timeout: sidecar timed out after 5000ms"
node node node node node node node node node node
`, 200)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"Codex\"}"},
			{"type":"function_call_output","call_id":"call_state","output":{"screenshot":%q,"accessibility_tree":%q,"status":"ok"}}
		]`, largeScreenshot, accessibilityTree)),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	visionCalls := 0
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			visionCalls++
			require.Equal(t, largeScreenshot, imageURL)
			return "Codex app window is visible. The prompt box is empty. Run is focused and clickable; Stop is disabled. A timeout error is visible.", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, nil, CodexGatewayDeepSeekRequestContext{}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	require.Equal(t, 1, visionCalls)
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, rewritten, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, cfg)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, largeScreenshot)
	require.NotContains(t, toolContent, strings.Repeat("node ", 32))
	require.Contains(t, toolContent, "computer_screenshot")
	require.Contains(t, toolContent, "Codex app window is visible")
	require.Contains(t, toolContent, "operable_lines")
	require.Contains(t, toolContent, `button \"Run\" enabled focused`)
	require.Contains(t, toolContent, `textbox \"Prompt\" value`)
	require.Contains(t, toolContent, "timeout: sidecar timed out")
	require.Less(t, len(toolContent), 4096)
}

func TestCodexGatewayDeepSeekRequest_ComputerUseSecondPassKeepsSemanticFields(t *testing.T) {
	largeScreenshot := "data:image/png;base64," + strings.Repeat("D", 90000)
	accessibilityTree := strings.Repeat("staticText filler node without actionable words\n", 130) + strings.Join([]string{
		`text input "Reply to assistant" focused element_index=reply bounds={20,640,700,48}`,
		`button "Send reply" enabled element_index=send bounds={730,640,80,48}`,
		`button "Lower screen action" enabled element_index=lower-action bounds={20,690,180,44}`,
	}, "\n")
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_state","name":"mcp__computer_use__get_app_state","arguments":"{\"app\":\"Codex\"}"},
			{"type":"function_call_output","call_id":"call_state","output":{"screenshot":%q,"accessibility_tree":%q,"status":"ok"}}
		]`, largeScreenshot, accessibilityTree)),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			require.Equal(t, largeScreenshot, imageURL)
			return strings.Repeat("Codex screenshot shows the lower reply area and actionable controls. ", 80), nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, nil, CodexGatewayDeepSeekRequestContext{}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, rewritten, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_second_pass",
		IsolationKey: "user_second_pass",
	}, cfg)
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, largeScreenshot)
	require.NotContains(t, toolContent, strings.Repeat("D", 128))
	require.Contains(t, toolContent, "computer_screenshot")
	require.Contains(t, toolContent, "accessibility_tree")
	require.Contains(t, toolContent, "operable_lines")
	require.Contains(t, toolContent, "Lower screen action")
	require.Contains(t, toolContent, "sha256")
	require.Contains(t, toolContent, "original_chars")
	require.False(t, gjson.Get(toolContent, "preview").Exists(), "final fallback must not collapse Computer Use output to preview-only")
	require.Equal(t, "computer_screenshot", gjson.Get(toolContent, "screenshot.content_class").String())
	require.Equal(t, "accessibility_tree", gjson.Get(toolContent, "accessibility_tree.content_class").String())
	require.GreaterOrEqual(t, len(gjson.Get(toolContent, "accessibility_tree.operable_lines").Array()), 1)
	require.LessOrEqual(t, len(toolContent), codexGatewayDeepSeekToolOutputMaxChars+256)
}

func TestCodexGatewayDeepSeekRequest_ComputerUsePrioritizesLateInputAndVisibleText(t *testing.T) {
	appState := "Computer Use state (CUA App Version: 799)\n<app_state>\n" +
		"App=/Applications/Doubao.app/ (bundleID com.bot.pc.doubao, pid 6240)\n" +
		"Window: \"豆包\", App: 豆包.\n" +
		func() string {
			var b strings.Builder
			for i := 0; i < 24; i++ {
				fmt.Fprintf(&b, "\t%d link Description: 历史会话%d, Value: chrome://doubao-chat/chat/sidebar%d\n", 26+i, i, i)
			}
			return b.String()
		}() +
		"\t120 文本 豆包回答：郑州早餐可以吃胡辣汤、油馍头和豆腐脑。\n" +
		strings.Repeat("\t121 文本 普通说明内容 filler filler filler filler\n", 20) +
		"\t188 文本输入区 (settable, string) 发消息...\n" +
		"\t189 按钮 发送\n" +
		"The focused UI element is 188 文本输入区 (settable, string) 发消息...\n" +
		"</app_state>"
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_state",
				"name":      "mcp__computer_use__get_app_state",
				"arguments": `{"app":"com.bot.pc.doubao"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_state",
				"output": []any{
					map[string]any{"type": "input_text", "text": "Wall time: 0.3377 seconds\nOutput:"},
					map[string]any{"type": "input_text", "text": appState},
					map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + strings.Repeat("A", 90000), "detail": "high"},
				},
			},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_late_computer_use",
		IsolationKey: "user_late_computer_use",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	textSummary := gjson.Get(toolContent, "1.text")
	require.Equal(t, "accessibility_tree", textSummary.Get("content_class").String())
	require.Contains(t, textSummary.Get("operable_lines").Raw, "188 文本输入区")
	require.Contains(t, textSummary.Get("operable_lines").Raw, "189 按钮 发送")
	require.Contains(t, textSummary.Get("visible_text").Raw, "胡辣汤")
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
	require.LessOrEqual(t, len(toolContent), codexGatewayDeepSeekToolOutputMaxChars+256)
}

func TestCodexGatewayDeepSeekRequest_ComputerUsePrioritizesLateEnglishTextArea(t *testing.T) {
	appState := "Computer Use state (CUA App Version: 799)\n<app_state>\n" +
		"App=/Applications/Doubao.app/ (bundleID com.bot.pc.doubao, pid 6240)\n" +
		"Window: \"Doubao\", App: Doubao.\n" +
		func() string {
			var b strings.Builder
			for i := 0; i < 18; i++ {
				fmt.Fprintf(&b, "\t%d link Description: Previous chat %d, Value: chrome://doubao-chat/chat/sidebar%d\n", 30+i, i, i)
			}
			return b.String()
		}() +
		"\t501 AXTextArea (settable, string) Placeholder: Message...\n" +
		"\t502 AXButton Send\n" +
		"</app_state>"
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_state",
				"name":      "mcp__computer_use__get_app_state",
				"arguments": `{"app":"com.bot.pc.doubao"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_state",
				"output": []any{
					map[string]any{"type": "input_text", "text": appState},
					map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + strings.Repeat("A", 90000), "detail": "high"},
				},
			},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_english_text_area",
		IsolationKey: "user_english_text_area",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	operableLines := gjson.Get(toolContent, "0.text.operable_lines").Raw
	if operableLines == "" {
		operableLines = gjson.Get(toolContent, "1.text.operable_lines").Raw
	}
	require.Contains(t, operableLines, "501 AXTextArea")
	require.Contains(t, operableLines, "502 AXButton Send")
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
}

func TestCodexGatewayDeepSeekRequest_ComputerUsePrioritizesStructuredLateInput(t *testing.T) {
	children := make([]any, 0, 18)
	for i := 0; i < 14; i++ {
		children = append(children, map[string]any{
			"role":          "link",
			"name":          fmt.Sprintf("Previous chat %d", i),
			"element_index": fmt.Sprintf("%d", 30+i),
		})
	}
	children = append(children,
		map[string]any{
			"role":          "AXTextArea",
			"placeholder":   "Message",
			"settable":      true,
			"focused":       true,
			"element_index": "501",
		},
		map[string]any{
			"role":          "button",
			"name":          "Send",
			"enabled":       true,
			"element_index": "502",
		},
	)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_state",
				"name":      "mcp__computer_use__get_app_state",
				"arguments": `{"app":"com.bot.pc.doubao"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_state",
				"output": map[string]any{
					"screenshot": "data:image/png;base64," + strings.Repeat("A", 90000),
					"accessibility_tree": map[string]any{
						"role":     "window",
						"name":     "Doubao",
						"children": children,
					},
				},
			},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_structured_text_area",
		IsolationKey: "user_structured_text_area",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.Contains(t, gjson.Get(toolContent, "accessibility_tree.operable_lines").Raw, "element_index=501")
	require.Contains(t, gjson.Get(toolContent, "accessibility_tree.operable_lines").Raw, "Send")
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
}

func TestCodexGatewayDeepSeekRequest_ComputerUseVisibleTextKeepsShortUrlAndOperableWords(t *testing.T) {
	appState := "Computer Use state (CUA App Version: 799)\n<app_state>\n" +
		"App=/Applications/Doubao.app/ (bundleID com.bot.pc.doubao, pid 6240)\n" +
		"Window: \"Doubao\", App: Doubao.\n" +
		strings.Repeat("\t26 link Description: Previous chat, Value: chrome://doubao-chat/chat/sidebar\n", 12) +
		"\t120 text OK\n" +
		"\t121 text See https://example.com; click the button only if needed.\n" +
		"\t501 AXTextArea (settable, string) Placeholder: Message...\n" +
		"</app_state>"
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_state",
				"name":      "mcp__computer_use__get_app_state",
				"arguments": `{"app":"com.bot.pc.doubao"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_state",
				"output": []any{
					map[string]any{"type": "input_text", "text": appState},
					map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + strings.Repeat("A", 90000), "detail": "high"},
				},
			},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_visible_text",
		IsolationKey: "user_visible_text",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	visibleText := gjson.Get(toolContent, "0.text.visible_text").Raw
	if visibleText == "" {
		visibleText = gjson.Get(toolContent, "1.text.visible_text").Raw
	}
	require.Contains(t, visibleText, "OK")
	require.Contains(t, visibleText, "https://example.com")
	require.Contains(t, visibleText, "click the button")
	require.NotContains(t, visibleText, "chrome://doubao-chat")
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
}

func TestCodexGatewayDeepSeekRequest_ComputerUseCombinedStringKeepsAXOverBase64(t *testing.T) {
	combined := "Computer Use state (CUA App Version: 799)\n<app_state>\n" +
		"App=/Applications/Doubao.app/ (bundleID com.bot.pc.doubao, pid 6240)\n" +
		"\t501 AXTextArea (settable, string) Placeholder: Message...\n" +
		"\t502 AXButton Send\n" +
		"</app_state>\n" +
		"image_url=data:image/png;base64," + strings.Repeat("A", 90000)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_state",
				"name":      "mcp__computer_use__get_app_state",
				"arguments": `{"app":"com.bot.pc.doubao"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_state",
				"output":  combined,
			},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_combined_string",
		IsolationKey: "user_combined_string",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.Equal(t, "accessibility_tree", gjson.Get(toolContent, "content_class").String())
	require.Contains(t, gjson.Get(toolContent, "operable_lines").Raw, "501 AXTextArea")
	require.Contains(t, gjson.Get(toolContent, "operable_lines").Raw, "502 AXButton Send")
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
}

func TestCodexGatewayDeepSeekRequest_ComputerUseMixedContentOutputKeepsAXText(t *testing.T) {
	appState := "Computer Use state (CUA App Version: 799)\n<app_state>\n" +
		"App=/Applications/Doubao.app/ (bundleID com.bot.pc.doubao, pid 6240)\n" +
		"Window: \"豆包\", App: 豆包.\n" +
		strings.Repeat("\t100 文本 历史对话 filler filler filler filler filler filler filler\n", 80) +
		"\t371 文本输入区 (settable, string) 发消息...\n" +
		"\t372 按钮 发送\n" +
		"The focused UI element is 371 文本输入区 (settable, string) 发消息...\n" +
		"</app_state>"
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: mustMarshalRawMessage(t, []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_state",
				"name":      "mcp__computer_use__get_app_state",
				"arguments": `{"app":"/Applications/Doubao.app/"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_state",
				"output": []any{
					map[string]any{"type": "input_text", "text": "Wall time: 0.3377 seconds\nOutput:"},
					map[string]any{"type": "input_text", "text": appState},
					map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + strings.Repeat("A", 90000), "detail": "high"},
				},
			},
		}),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer_use__","tools":[
				{"type":"function","name":"get_app_state","parameters":{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}}
			]}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_mixed_computer_use",
		IsolationKey: "user_mixed_computer_use",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	toolContent := gjson.GetBytes(raw, "messages.1.content").String()
	require.NotContains(t, toolContent, strings.Repeat("A", 128))
	require.Contains(t, toolContent, "accessibility_tree")
	require.Contains(t, toolContent, "文本输入区")
	require.Contains(t, toolContent, "发消息")
	require.Contains(t, toolContent, "371")

	textSummary := gjson.Get(toolContent, "1.text")
	require.Equal(t, "accessibility_tree", textSummary.Get("content_class").String())
	require.Equal(t, "text", textSummary.Get("field").String())
	require.True(t, textSummary.Get("truncated").Bool())
	require.Contains(t, textSummary.Get("operable_lines").Raw, "文本输入区")
	require.Contains(t, textSummary.Get("operable_lines").Raw, "371")
	require.Equal(t, "binary_or_image", gjson.Get(toolContent, "2.image_url.content_class").String())
	require.Equal(t, "image_url", gjson.Get(toolContent, "2.image_url.field").String())
	require.True(t, gjson.Get(toolContent, "2.image_url.truncated").Bool())
	require.Equal(t, "data:image/jpeg;base64", gjson.Get(toolContent, "2.image_url.media_type").String())
	require.LessOrEqual(t, len(toolContent), codexGatewayDeepSeekToolOutputMaxChars+256)
}

func TestCodexGatewayDeepSeekRequest_ComputerUseSecondPassKeepsFieldsWhenCompactStillLarge(t *testing.T) {
	toolOutput := map[string]any{
		"screenshot": map[string]any{
			"content_class":  "computer_screenshot",
			"vision_summary": strings.Repeat("Lower-screen controls are visible. ", 200),
			"truncated":      true,
			"original_chars": 90022,
			"sha256":         strings.Repeat("a", 64),
		},
		"accessibility_tree": map[string]any{
			"content_class":  "accessibility_tree",
			"field":          "accessibility_tree",
			"truncated":      true,
			"original_chars": 6400,
			"sha256":         strings.Repeat("b", 64),
			"operable_lines": []string{
				strings.Repeat(`text input "Reply to assistant" focused element_index=reply `, 8),
				strings.Repeat(`button "Lower screen action" enabled element_index=lower-action `, 8),
			},
		},
		"large_irrelevant": strings.Repeat("irrelevant filler ", 500),
	}
	content, err := normalizeCodexGatewayDeepSeekToolOutput(toolOutput)
	require.NoError(t, err)

	require.Contains(t, content, "computer_screenshot")
	require.Contains(t, content, "accessibility_tree")
	require.Contains(t, content, "operable_lines")
	require.Contains(t, content, "Lower screen action")
	require.False(t, gjson.Get(content, "preview").Exists(), "semantic fallback must not collapse to preview-only even after compacting")
	require.Equal(t, "computer_screenshot", gjson.Get(content, "screenshot.content_class").String())
	require.Equal(t, "accessibility_tree", gjson.Get(content, "accessibility_tree.content_class").String())
	require.LessOrEqual(t, len(content), codexGatewayDeepSeekToolOutputMaxChars)
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_SkipsNonComputerUseToolOutputImages(t *testing.T) {
	imageURL := "data:image/png;base64," + strings.Repeat("C", 256)
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_asset","name":"asset_screenshot_lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_asset","output":{"image_url":%q,"status":"ok"}}
		]`, imageURL)),
		Tools: json.RawMessage(`[
			{"type":"function","name":"asset_lookup","parameters":{"type":"object"}}
		]`),
	}
	called := false
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			called = true
			return "unexpected", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, nil, CodexGatewayDeepSeekRequestContext{}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	require.False(t, called)
	require.JSONEq(t, string(req.Input), string(rewritten.Input))
}

func TestCodexGatewayDeepSeekRequestWithVisionProxy_RewritesPreviousComputerUseOutputFromState(t *testing.T) {
	largeScreenshot := "data:image/png;base64," + strings.Repeat("D", 20000)
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_state",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContentPresent: true,
		ReasoningContent:        "need to inspect Codex",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_state", Type: CodexGatewayToolKindNamespace, Alias: "mcp__computer_use__get_app_state", Name: "get_app_state", Arguments: `{"app":"Codex"}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"mcp__computer_use__get_app_state": {Alias: "mcp__computer_use__get_app_state", Kind: CodexGatewayToolKindNamespace, NamespacePath: "mcp__computer_use__", Name: "get_app_state"},
		},
	}))
	req := CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_state"),
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call_output","call_id":"call_state","output":{"screenshot":%q,"status":"ok"}}
		]`, largeScreenshot)),
	}
	visionCalls := 0
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			visionCalls++
			require.Equal(t, largeScreenshot, imageURL)
			return "Codex window after click", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, "deepseek-v4-pro", cfg)
	require.NoError(t, err)
	require.Equal(t, 1, visionCalls)
	require.NotContains(t, string(rewritten.Input), largeScreenshot)
	require.Contains(t, string(rewritten.Input), "Codex window after click")
}

func TestCodexGatewayAgnesRequestWithVisionProxy_UsesAgnesPreviousComputerUseState(t *testing.T) {
	largeScreenshot := "data:image/png;base64," + strings.Repeat("E", 20000)
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_agnes_state",
			SessionKey:    "session_agnes_state",
			IsolationKey:  "iso_agnes_state",
			Provider:      "agnes",
			UpstreamModel: "agnes-2.0-flash",
		},
		AssistantContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_agnes_state", Type: CodexGatewayToolKindNamespace, Alias: "mcp__computer_use__get_app_state", Name: "get_app_state", Arguments: `{"app":"Codex"}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"mcp__computer_use__get_app_state": {Alias: "mcp__computer_use__get_app_state", Kind: CodexGatewayToolKindNamespace, NamespacePath: "mcp__computer_use__", Name: "get_app_state"},
		},
	}))
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_deepseek_state",
			SessionKey:    "session_agnes_state",
			IsolationKey:  "iso_agnes_state",
			Provider:      "deepseek",
			UpstreamModel: "agnes-2.0-flash",
		},
		AssistantContentPresent: true,
		ReasoningContent:        "wrong provider reasoning",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_wrong_provider", Type: CodexGatewayToolKindFunction, Alias: "shell", Name: "shell", Arguments: `{}`},
		},
	}))
	req := CodexGatewayResponsesCreateRequest{
		Model:              "agnes-2.0-flash",
		PreviousResponseID: stringPtr("resp_agnes_state"),
		Input: json.RawMessage(fmt.Sprintf(`[
			{"type":"function_call_output","call_id":"call_agnes_state","output":{"screenshot":%q,"status":"ok"}}
		]`, largeScreenshot)),
	}
	visionCalls := 0
	cfg := CodexGatewayDeepSeekRequestConfig{
		Provider:      "agnes",
		ReasoningMode: "openai",
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			visionCalls++
			require.Equal(t, largeScreenshot, imageURL)
			return "Codex window for AGNES", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_agnes_state",
		IsolationKey: "iso_agnes_state",
		Provider:     "agnes",
	}, "agnes-2.0-flash", cfg)
	require.NoError(t, err)
	require.Equal(t, 1, visionCalls)
	require.NotContains(t, string(rewritten.Input), largeScreenshot)
	require.Contains(t, string(rewritten.Input), "Codex window for AGNES")
}

func TestCodexGatewayDeepSeekRequest_ReplaysLegacyCustomToolCallWithoutCurrentTools(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-pro",
		ToolChoice: json.RawMessage(`"edit"`),
		Input: json.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_legacy_custom","name":"edit","input":"*** Begin Patch\n*** End Patch\n"},
			{"type":"custom_tool_call_output","call_id":"call_legacy_custom","output":"patch applied"}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "tool_choice").Exists())
	require.Equal(t, "custom__edit", gjson.GetBytes(raw, "messages.0.tool_calls.0.function.name").String())
	require.Equal(t, "tool", gjson.GetBytes(raw, "messages.1.role").String())
	require.Equal(t, "call_legacy_custom", gjson.GetBytes(raw, "messages.1.tool_call_id").String())
}

func TestCodexGatewayDeepSeekRequest_NormalizesLegacyDottedNamespaceToolNames(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	preparedStringChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"function_call","call_id":"call_ns","name":"shell.exec","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}
		]`),
		ToolChoice: json.RawMessage(`"shell.exec"`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	rawStringChoice, err := json.Marshal(preparedStringChoice.Body)
	require.NoError(t, err)
	require.Equal(t, "shell__exec", gjson.GetBytes(rawStringChoice, "tool_choice.function.name").String())
	require.Equal(t, "shell__exec", gjson.GetBytes(rawStringChoice, "messages.0.tool_calls.0.function.name").String())

	preparedObjectChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"function_call","call_id":"call_ns_obj","name":"shell.exec","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}
		]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"shell.exec"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	rawObjectChoice, err := json.Marshal(preparedObjectChoice.Body)
	require.NoError(t, err)
	require.Equal(t, "shell__exec", gjson.GetBytes(rawObjectChoice, "tool_choice.function.name").String())
	require.Equal(t, "shell__exec", gjson.GetBytes(rawObjectChoice, "messages.0.tool_calls.0.function.name").String())
}

func TestCodexGatewayDeepSeekRequest_AcceptsLocalShellCallInput(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"local_shell_call","call_id":"call_ns","name":"shell.exec","action":{"type":"exec","command":["zsh","-lc","pwd"]}}]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	raw, err := json.Marshal(prepared.Body)
	require.NoError(t, err)
	require.Equal(t, "shell__exec", gjson.GetBytes(raw, "messages.0.tool_calls.0.function.name").String())
	require.Equal(t, `{"cmd":"pwd"}`, gjson.GetBytes(raw, "messages.0.tool_calls.0.function.arguments").String())
}

func TestCodexGatewayDeepSeekRequest_MapsAssistantToolCallsAndBackfillsReasoningContent(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"let me open that"}],
				"tool_calls":[
					{
						"id":"call_1",
						"type":"function",
						"function":{
							"name":"open-page",
							"arguments":"{\"url\":\"https://example.com\"}"
						}
					}
				]
			},
			{"type":"function_call_output","call_id":"call_1","output":"opened"},
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"waiting for tool"}]
			}
		]`),
		Tools: json.RawMessage(`[
			{
				"type":"namespace",
				"name":"browser",
				"tools":[
					{"name":"open-page","parameters":{"type":"object","properties":{"url":{"type":"string"}}}}
				]
			}
		]`),
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 3)

	first := messages[0].(map[string]any)
	require.Equal(t, "assistant", first["role"])
	require.Equal(t, "let me open that", first["content"])
	require.Equal(t, "", first["reasoning_content"])
	require.Equal(t, "browser__open-page", first["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	toolOutput := messages[1].(map[string]any)
	require.Equal(t, "tool", toolOutput["role"])
	require.Equal(t, "call_1", toolOutput["tool_call_id"])

	second := messages[2].(map[string]any)
	require.Equal(t, "assistant", second["role"])
	require.Equal(t, "", second["reasoning_content"])
}

func TestCodexGatewayDeepSeekRequest_RejectsDanglingCurrentToolCalls(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_dangling","name":"get_weather","arguments":"{}"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"get_weather","parameters":{"type":"object"}}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{SessionKey: "session_dangling", IsolationKey: "iso_dangling"}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dangling tool call")
}

func TestCodexGatewayDeepSeekRequest_CapturesDanglingToolCallFailCloseDiagnostics(t *testing.T) {
	baseDir := t.TempDir()
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID:  "dangling_tool_fail_close",
		Provider: "deepseek",
		Model:    "deepseek-v4-pro",
	})
	require.NotNil(t, trace)

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_private_dangling","name":"get_weather","arguments":"{}"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"PRIVATE_NEXT_PROMPT"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"get_weather","parameters":{"type":"object"}}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_dangling_diag",
		IsolationKey: "iso_dangling_diag",
		CaptureTrace: trace,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "error"})
	require.NoError(t, manager.Close())

	diagnostics := readCaptureJSONFile(t, filepath.Join(baseDir, time.Now().Format("2006-01-02"), "dangling_tool_fail_close", "client_request.diagnostics.json"))
	deepseek := diagnostics["deepseek_cache"].(map[string]any)
	require.Equal(t, true, deepseek["tool_pairing_invalid"])
	require.Equal(t, "fail_close", deepseek["tool_pairing_action"])
	require.Contains(t, fmtAny(deepseek["tool_pairing_reasons"]), "dangling_tool_call")
	require.NotContains(t, fmtAny(diagnostics), "call_private_dangling")
	require.NotContains(t, fmtAny(diagnostics), "PRIVATE_NEXT_PROMPT")
}

func TestCodexGatewayDeepSeekRequest_AllowsFinalAssistantToolCallWithoutOutput(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_pending","name":"get_weather","arguments":"{}"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"get_weather","parameters":{"type":"object"}}
		]`),
	}, nil, CodexGatewayDeepSeekRequestContext{SessionKey: "session_pending", IsolationKey: "iso_pending"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	assistant := messages[0].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	require.Equal(t, "call_pending", assistant["tool_calls"].([]any)[0].(map[string]any)["id"])
}

func TestCodexGatewayDeepSeekRequest_RejectsInvalidStateAndUnpairedToolOutputs(t *testing.T) {
	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 4,
		Now:      time.Now,
	})
	invalidState := CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_invalid",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContentPresent: true,
		ToolCalls:               []CodexGatewayStoredToolCall{{ID: "call_1", Name: "shell", Arguments: `{}`}},
	}
	store.entries[codexGatewayStateStorageKey(invalidState.Key)] = codexGatewayStateEntry{
		state:     invalidState,
		expiresAt: time.Now().Add(time.Minute),
	}

	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_invalid"),
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_parallel",
			SessionKey:    "session_1",
			IsolationKey:  "user_1",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContentPresent: true,
		ReasoningContent:        "need both tool results",
		ReasoningContentPresent: true,
		ToolCalls: []CodexGatewayStoredToolCall{
			{ID: "call_a", Name: "shell", Arguments: `{}`},
			{ID: "call_b", Name: "shell", Arguments: `{}`},
		},
		ToolNameMap: map[string]CodexGatewayToolNameMapEntry{
			"shell": {Alias: "shell", Kind: CodexGatewayToolKindFunction, Name: "shell"},
		},
	}))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_a","output":"ok"}]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input:              json.RawMessage(`[]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"before tool output"}]},
			{"type":"function_call_output","call_id":"call_a","output":"ok"}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_parallel"),
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_a","output":"ok"}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexGatewayStateInvalid))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"function_call_output","call_id":"call_missing","output":"ok"}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_dup","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call","call_id":"call_dup","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
			{"type":"function_call_output","call_id":"call_dup","output":"ok"}
		]`),
		Tools: json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_reuse","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_reuse","output":"ok"},
			{"type":"function_call","call_id":"call_reuse","name":"shell","arguments":"{\"cmd\":\"ls\"}"}
		]`),
		Tools: json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
}

func TestCodexGatewayDeepSeekRequest_StripsResponsesOnlyFieldsFromPreparedBody(t *testing.T) {
	store := true
	parallel := true
	req := CodexGatewayResponsesCreateRequest{
		Model:        "deepseek-v4-pro",
		Instructions: json.RawMessage(`"Keep answers short."`),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"get_goal","parameters":{
				"type":"object",
				"properties":{
					"filter":{"type":"object","properties":{"status":{"type":"string"}},"required":null},
					"required":null
				},
				"required":null
			}}
		]`),
		Include:           json.RawMessage(`["reasoning.encrypted_content"]`),
		Store:             &store,
		ParallelToolCalls: &parallel,
		ClientMetadata:    json.RawMessage(`{"trace_id":"client-side-only"}`),
		PromptCacheKey:    "session-only-cache-key",
	}
	model := CodexGatewayModel{
		Slug:                      "deepseek-v4-pro",
		Provider:                  "deepseek",
		UpstreamModel:             "deepseek-v4-pro",
		SupportsParallelToolCalls: true,
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.NotContains(t, prepared.Body, "include")
	require.NotContains(t, prepared.Body, "store")
	require.NotContains(t, prepared.Body, "parallel_tool_calls")
	require.NotContains(t, prepared.Body, "client_metadata")
	require.NotContains(t, prepared.Body, "prompt_cache_key")
	require.Equal(t, "deepseek-v4-pro", prepared.Body["model"])
	require.Equal(t, "user", prepared.Body["messages"].([]any)[1].(map[string]any)["role"])
	body := mustMarshalJSON(t, prepared.Body)
	require.NotContains(t, body, `"required":null`)
	require.Equal(t, map[string]any{}, gjson.Get(body, `tools.0.function.parameters.properties.required`).Value())
}

func TestCodexGatewayAgnesRequest_ForwardsScopedPromptCacheKey(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "agnes-2.0-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		PromptCacheKey: "raw-client-cache-key",
	}
	model := CodexGatewayModel{
		Slug:          "agnes-2.0-flash",
		Provider:      "agnes",
		UpstreamModel: "agnes-2.0-flash",
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey: "entity_scoped_session_hash",
		Provider:   "agnes",
	}, CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai"})
	require.NoError(t, err)

	require.Equal(t, "codex_agnes_entity_scoped_session_hash", prepared.Body["prompt_cache_key"])
	require.NotEqual(t, req.PromptCacheKey, prepared.Body["prompt_cache_key"])
}

func TestCodexGatewayDeepSeekStreamRequest_StripsResponsesOnlyFieldsFromUpstreamBody(t *testing.T) {
	store := true
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"get_goal","parameters":{
				"type":"object",
				"properties":{
					"filter":{"type":"object","properties":{"status":{"type":"string"}},"required":null},
					"required":null
				},
				"$defs":{"GoalFilter":{"type":"object","required":"bad"}},
				"anyOf":[{"type":"object","required":null}],
				"required":null
			}}
		]`),
		Include: json.RawMessage(`["reasoning.encrypted_content"]`),
		Store:   &store,
	}
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(body, "include").Exists())
		require.False(t, gjson.GetBytes(body, "store").Exists())
		require.True(t, gjson.GetBytes(body, "stream").Bool())
		require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())
		require.NotContains(t, string(body), `"required":null`)
		require.NotContains(t, string(body), `"required":"bad"`)
		require.Equal(t, map[string]any{}, gjson.GetBytes(body, `tools.0.function.parameters.properties.required`).Value())

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_stream_allowlist","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			"",
			`data: {"id":"chatcmpl_stream_allowlist","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":0,"total_tokens":4}}`,
			"",
			`data: [DONE]`,
			"",
		}, "\n"))
	}))
	defer server.Close()

	var buf bytes.Buffer
	_, err := ExecuteCodexGatewayDeepSeekStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		model,
		req,
		nil,
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_stream", IsolationKey: "iso_stream"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
}

func TestCodexGatewayDeepSeekRequest_FunctionCallsToolChoiceAndReasoningDisablePolicy(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-flash",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-flash",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"shell"}`),
		Reasoning:  json.RawMessage(`{"effort":"minimal"}`),
		RawFields: map[string]json.RawMessage{
			"temperature": json.RawMessage(`0.4`),
		},
	}

	prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{
		AllowReasoningDisable: true,
	})
	require.NoError(t, err)

	require.Equal(t, map[string]any{"type": "disabled"}, prepared.Body["thinking"])
	require.Equal(t, 0.4, prepared.Body["temperature"])

	toolChoice := prepared.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoice["type"])
	require.Equal(t, "shell", toolChoice["function"].(map[string]any)["name"])

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 2)
	require.Equal(t, "", messages[0].(map[string]any)["content"])
	require.Equal(t, "", messages[0].(map[string]any)["reasoning_content"])

	preparedWithSeed, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
		UserID:       "contains space",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	seededUserID, _ := preparedWithSeed.Body["user_id"].(string)
	require.NotEmpty(t, seededUserID)
	require.NotEqual(t, "contains space", seededUserID)
	require.True(t, regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`).MatchString(seededUserID))

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"missing_tool"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2b","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`"missing_tool"`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")

	preparedLegacyPatch, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[{"type":"function_call","call_id":"call_legacy_patch","name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\\n*** End Patch\\n\"}"}]`),
		Tools: json.RawMessage(`[
			{"type":"custom","name":"edit","custom":{"input_schema":{"type":"object"}}}
		]`),
		ToolChoice: json.RawMessage(`"apply_patch"`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceLegacyPatch := preparedLegacyPatch.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoiceLegacyPatch["type"])
	require.Equal(t, "custom__edit", toolChoiceLegacyPatch["function"].(map[string]any)["name"])
	legacyMessages := preparedLegacyPatch.Body["messages"].([]any)
	require.Equal(t, "custom__edit", legacyMessages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	legacyCases := []struct {
		name       string
		toolChoice string
		tools      string
		wantAlias  string
	}{
		{
			name:       "update_plan",
			toolChoice: "update_plan",
			tools:      `[{"type":"function","name":"todowrite","parameters":{"type":"object"}}]`,
			wantAlias:  "todowrite",
		},
		{
			name:       "read_file",
			toolChoice: "read_file",
			tools:      `[{"type":"function","name":"read","parameters":{"type":"object"}}]`,
			wantAlias:  "read",
		},
		{
			name:       "write_file",
			toolChoice: "write_file",
			tools:      `[{"type":"function","name":"write","parameters":{"type":"object"}}]`,
			wantAlias:  "write",
		},
		{
			name:       "execute_bash",
			toolChoice: "execute_bash",
			tools:      `[{"type":"function","name":"bash","parameters":{"type":"object"}}]`,
			wantAlias:  "bash",
		},
	}
	for _, tc := range legacyCases {
		t.Run("legacy_"+tc.name, func(t *testing.T) {
			preparedLegacy, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
				Model:      "deepseek-v4-flash",
				Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_legacy","name":"` + tc.toolChoice + `","arguments":"{}"}]`),
				Tools:      json.RawMessage(tc.tools),
				ToolChoice: json.RawMessage(`"` + tc.toolChoice + `"`),
			}, nil, CodexGatewayDeepSeekRequestContext{
				SessionKey:   "session_1",
				IsolationKey: "user_1",
			}, CodexGatewayDeepSeekRequestConfig{})
			require.NoError(t, err)
			toolChoiceLegacy := preparedLegacy.Body["tool_choice"].(map[string]any)
			require.Equal(t, tc.wantAlias, toolChoiceLegacy["function"].(map[string]any)["name"])
			legacyMessages := preparedLegacy.Body["messages"].([]any)
			require.Equal(t, tc.wantAlias, legacyMessages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])
		})
	}

	preparedLegacyObjectChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-flash",
		Input: json.RawMessage(`[{"type":"custom_tool_call","call_id":"call_legacy_custom","name":"apply_patch","input":"*** Begin Patch\n*** End Patch\n"}]`),
		Tools: json.RawMessage(`[
			{"type":"custom","name":"edit","custom":{"input_schema":{"type":"object"}}}
		]`),
		ToolChoice: json.RawMessage(`{"type":"custom","function":{"name":"apply_patch"}}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceLegacyObject := preparedLegacyObjectChoice.Body["tool_choice"].(map[string]any)
	require.Equal(t, "custom__edit", toolChoiceLegacyObject["function"].(map[string]any)["name"])
	legacyObjectMessages := preparedLegacyObjectChoice.Body["messages"].([]any)
	require.Equal(t, "custom__edit", legacyObjectMessages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2c","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"bogus"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported type")

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2d","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"type":"shell"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported type")

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_2e","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice: json.RawMessage(`{"name":"shell"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_choice.type is required")

	preparedCustomChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_3","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"custom","name":"scratch pad","custom":{"input_schema":{"type":"object"}}}]`),
		ToolChoice: json.RawMessage(`{"type":"custom"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceCustom := preparedCustomChoice.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoiceCustom["type"])
	require.Equal(t, "custom__scratch_pad", toolChoiceCustom["function"].(map[string]any)["name"])

	preparedCustomPathChoice, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_3b","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"namespace","name":"browser","tools":[{"type":"custom","name":"scratch pad","custom":{"input_schema":{"type":"object"}}}]}]`),
		ToolChoice: json.RawMessage(`{"type":"custom","name":"browser__scratch pad"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	toolChoiceCustomPath := preparedCustomPathChoice.Body["tool_choice"].(map[string]any)
	require.Equal(t, "function", toolChoiceCustomPath["type"])
	require.Equal(t, "browser__custom__scratch_pad", toolChoiceCustomPath["function"].(map[string]any)["name"])

	_, err = BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:      "deepseek-v4-flash",
		Input:      json.RawMessage(`[{"type":"function_call","call_id":"call_5","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]`),
		Tools:      json.RawMessage(`[{"type":"custom","name":"scratch pad","custom":{"input_schema":{"type":"object"}}}]`),
		ToolChoice: json.RawMessage(`{"type":"custom","name":"missing_custom"}`),
	}, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")
}

func TestCodexGatewayDeepSeekRequest_RejectsAmbiguousLeafToolNames(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"open","arguments":"{\"url\":\"https://example.com\"}"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"browser","tools":[{"name":"open","parameters":{"type":"object"}}]},
			{"type":"namespace","name":"tabs","tools":[{"name":"open","parameters":{"type":"object"}}]}
		]`),
	}

	_, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous tool name")
}

func TestCodexGatewayDeepSeekRequest_RejectsAmbiguousTopLevelAndNamespacedPath(t *testing.T) {
	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"a__b","arguments":"{\"url\":\"https://example.com\"}"}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"a__b","parameters":{"type":"object"}},
			{"type":"namespace","name":"a","tools":[{"name":"b","parameters":{"type":"object"}}]}
		]`),
	}

	_, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous tool name")
}

func deepSeekRequestToolFunctionByName(t *testing.T, tools []any, name string) map[string]any {
	t.Helper()
	for _, toolAny := range tools {
		tool, ok := toolAny.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if function["name"] == name {
			return function
		}
	}
	require.Failf(t, "deepseek request tool not found", "name=%s tools=%v", name, tools)
	return nil
}

func deepSeekRequestToolFunctionBySuffix(t *testing.T, tools []any, suffix string) map[string]any {
	t.Helper()
	for _, toolAny := range tools {
		tool, ok := toolAny.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if strings.HasSuffix(fmt.Sprint(function["name"]), suffix) {
			return function
		}
	}
	require.Failf(t, "deepseek request tool not found by suffix", "suffix=%s tools=%v", suffix, tools)
	return nil
}

func loadCodexGatewayDeepSeekNativeParityFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "codex_gateway_deepseek_native_parity", name))
	require.NoError(t, err)
	require.True(t, gjson.ValidBytes(body), "fixture must be valid JSON: %s", name)
	return body
}

func countDeepSeekSerialToolInstructions(messages any) int {
	count := 0
	switch typed := messages.(type) {
	case []any:
		for _, msg := range typed {
			m, ok := msg.(map[string]any)
			if ok && m["role"] == "system" && m["content"] == codexGatewayDeepSeekSerialToolInstruction {
				count++
			}
		}
	case []json.RawMessage:
		for _, raw := range typed {
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil && m["role"] == "system" && m["content"] == codexGatewayDeepSeekSerialToolInstruction {
				count++
			}
		}
	}
	return count
}

func deepSeekRequestToolFunctionByDescription(t *testing.T, tools []any, needle string) map[string]any {
	t.Helper()
	for _, toolAny := range tools {
		tool, ok := toolAny.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if strings.Contains(fmt.Sprint(function["description"]), needle) {
			return function
		}
	}
	require.Failf(t, "deepseek request tool not found by description", "needle=%s tools=%v", needle, tools)
	return nil
}
