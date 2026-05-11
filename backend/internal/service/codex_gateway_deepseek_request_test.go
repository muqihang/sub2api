package service

import (
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
			{ID: "call_1", Name: "browser__open-page", Arguments: `{"url":"https://example.com"}`},
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
	require.Len(t, messages, 2)

	first := messages[0].(map[string]any)
	require.Equal(t, "assistant", first["role"])
	require.Equal(t, "let me open that", first["content"])
	require.Equal(t, "", first["reasoning_content"])
	require.Equal(t, "browser__open-page", first["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])

	second := messages[1].(map[string]any)
	require.Equal(t, "assistant", second["role"])
	require.Equal(t, "", second["reasoning_content"])
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
		state: invalidState,
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
		Tools:       json.RawMessage(`[{"type":"function","name":"shell","parameters":{"type":"object"}}]`),
		ToolChoice:  json.RawMessage(`{"type":"function","name":"shell"}`),
		Reasoning:   json.RawMessage(`{"effort":"minimal"}`),
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

	_, err = BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_1",
		IsolationKey: "user_1",
		UserID:       "contains space",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.Error(t, err)

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
