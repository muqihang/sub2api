package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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

func TestCodexGatewayDeepSeekRequestWithVisionProxy_RewritesImageToHostedVisionText(t *testing.T) {
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
	cfg := CodexGatewayDeepSeekRequestConfig{
		HostedImageVision: func(ctx context.Context, imageURL string) (string, error) {
			require.Equal(t, "data:image/png;base64,AAAA", imageURL)
			return "这是一张终端截图，主要内容是目录树。", nil
		},
	}

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, cfg)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"请看这张图"},
				{"type":"input_text","text":"[hosted_image_vision]\n这是一张终端截图，主要内容是目录树。"}
			]
		}
	]`, string(rewritten.Input))
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

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, cfg)
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

	rewritten, err := codexGatewayDeepSeekRequestWithHostedVision(context.Background(), req, cfg)
	require.NoError(t, err)
	prepared, err := BuildCodexGatewayDeepSeekRequest(model, rewritten, nil, CodexGatewayDeepSeekRequestContext{}, cfg)
	require.NoError(t, err)

	messages := prepared.Body["messages"].([]any)
	require.Len(t, messages, 1)
	require.Contains(t, messages[0].(map[string]any)["content"].(string), codexGatewayDeepSeekImagePlaceholder())
}

func TestCodexGatewayDeepSeekRequest_ExposesHostedToolsAndParallelToolFlag(t *testing.T) {
	parallel := true
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"reply ok"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"web_search"},
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
	require.NotContains(t, prepared.Body["messages"].([]any)[0].(map[string]any)["content"], "OpenAI hosted tools are not available")

	tools := prepared.Body["tools"].([]any)
	require.Len(t, tools, 5)
	require.Equal(t, "web_search", tools[0].(map[string]any)["function"].(map[string]any)["name"])
	require.Equal(t, "image_generation", tools[1].(map[string]any)["function"].(map[string]any)["name"])
	require.Equal(t, "computer_use_preview", tools[2].(map[string]any)["function"].(map[string]any)["name"])
	require.Equal(t, "file_search", tools[3].(map[string]any)["function"].(map[string]any)["name"])
	require.Equal(t, "exec_command", tools[4].(map[string]any)["function"].(map[string]any)["name"])
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
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_beta",
		IsolationKey: "api_key_1",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	third, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{
		SessionKey:   "session_alpha",
		IsolationKey: "api_key_2",
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	require.Equal(t, first.Body["user_id"], second.Body["user_id"])
	require.NotEqual(t, first.Body["user_id"], third.Body["user_id"])
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
	require.Equal(t, "web_search", firstTools[0].(map[string]any)["function"].(map[string]any)["name"])
	require.Equal(t, "web_search", secondTools[1].(map[string]any)["function"].(map[string]any)["name"])
	require.NotContains(t, first.Body["messages"].([]any)[0].(map[string]any)["content"], "OpenAI hosted tools are not available")
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
