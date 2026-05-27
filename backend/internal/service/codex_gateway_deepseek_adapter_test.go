package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexGatewayDeepSeekAdapterNonStream(t *testing.T) {
	t.Run("maps text completion and usage", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model:        "deepseek-v4-pro",
			Instructions: json.RawMessage(`"Be concise."`),
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"Say hello"}]}
			]`),
		}
		stateStore := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
			TTL:      time.Minute,
			MaxItems: 4,
			Now:      time.Now,
		})
		reqCtx := CodexGatewayDeepSeekRequestContext{
			SessionKey:   "session_text",
			IsolationKey: "iso_text",
			UserID:       "user_text",
		}

		prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, stateStore, reqCtx, CodexGatewayDeepSeekRequestConfig{})
		require.NoError(t, err)
		expectedBody, err := json.Marshal(prepared.Body)
		require.NoError(t, err)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, "/v1/chat/completions", r.URL.Path)
			require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, string(expectedBody), string(body))

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("x-request-id", "rid_text")
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl_text",
				"object":"chat.completion",
				"model":"deepseek-v4-pro",
				"choices":[
					{
						"index":0,
						"message":{"role":"assistant","content":"hello there"},
						"finish_reason":"stop"
					}
				],
				"usage":{
					"prompt_tokens":11,
					"completion_tokens":5,
					"total_tokens":16,
					"prompt_tokens_details":{"cached_tokens":4},
					"prompt_cache_hit_tokens":4
				}
			}`))
		}))
		defer server.Close()

		result, err := ExecuteCodexGatewayDeepSeekAdapter(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
		)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, result.ServiceResponse.StatusCode)
		require.Equal(t, "rid_text", result.ProviderResult.UpstreamRequestID)
		require.Equal(t, "chatcmpl_text", result.ProviderResult.ResponseID)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Equal(t, 11, result.ProviderResult.Usage.InputTokens)
		require.Equal(t, 5, result.ProviderResult.Usage.OutputTokens)
		require.Equal(t, 16, result.ProviderResult.Usage.TotalTokens)
		require.Equal(t, 4, result.ProviderResult.Usage.CacheReadInputTokens)
		require.Equal(t, float64(4), result.ProviderResult.Usage.ProviderUsageExtra["prompt_cache_hit_tokens"])

		body := result.ServiceResponse.Body
		require.JSONEq(t, `{
			"id":"chatcmpl_text",
			"object":"response",
			"model":"deepseek-v4-pro",
			"status":"completed",
			"output":[
				{
					"type":"message",
					"id":"msg_chatcmpl_text_0",
					"role":"assistant",
					"status":"completed",
					"content":[{"type":"output_text","text":"hello there"}]
				}
			],
			"usage":{
				"input_tokens":11,
				"output_tokens":5,
				"total_tokens":16,
				"input_tokens_details":{"cached_tokens":4}
			}
		}`, string(body))
	})

	t.Run("preserves empty output when upstream returns no choices", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl_empty",
				"object":"chat.completion",
				"model":"deepseek-v4-pro",
				"choices":[],
				"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}
			}`))
		}))
		defer server.Close()

		result, err := ExecuteCodexGatewayDeepSeekAdapter(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_empty", IsolationKey: "iso_empty"},
			CodexGatewayDeepSeekRequestConfig{},
		)
		require.NoError(t, err)
		require.Equal(t, int64(0), gjson.GetBytes(result.ServiceResponse.Body, "output.#").Int())
	})

	t.Run("maps function and custom tool calls and stores reasoning", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"use tools"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}},
				{"type":"custom","name":"shell","description":"run shell","input_schema":{"type":"string"}}
			]`),
		}
		stateStore := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
			TTL:      time.Minute,
			MaxItems: 4,
			Now:      time.Now,
		})
		reqCtx := CodexGatewayDeepSeekRequestContext{
			SessionKey:   "session_tools",
			IsolationKey: "iso_tools",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Equal(t, "get_weather", gjson.GetBytes(body, "tools.0.function.name").String())
			require.Equal(t, "custom__shell", gjson.GetBytes(body, "tools.1.function.name").String())

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl_tools",
				"object":"chat.completion",
				"model":"deepseek-v4-pro",
				"choices":[
					{
						"index":0,
						"message":{
							"role":"assistant",
							"content":"",
							"reasoning_content":"need both tools",
							"tool_calls":[
								{
									"id":"call_weather",
									"type":"function",
									"function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}
								},
								{
									"id":"call_shell",
									"type":"function",
									"function":{"name":"custom__shell","arguments":"pwd"}
								}
							]
						},
						"finish_reason":"tool_calls"
					}
				],
				"usage":{
					"prompt_tokens":9,
					"completion_tokens":3,
					"total_tokens":12,
					"prompt_cache_hit_tokens":7
				}
			}`))
		}))
		defer server.Close()

		result, err := ExecuteCodexGatewayDeepSeekAdapter(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
		)
		require.NoError(t, err)

		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Equal(t, 7, result.ProviderResult.Usage.CacheReadInputTokens)
		require.Len(t, result.ProviderResult.ToolCalls, 2)
		require.True(t, result.ProviderResult.ReasoningContentPresent)
		require.Equal(t, "need both tools", result.ProviderResult.ReasoningContent)

		body := result.ServiceResponse.Body
		require.NotContains(t, string(body), "need both tools")
		require.Equal(t, "function_call", gjson.GetBytes(body, "output.0.type").String())
		require.Equal(t, "fc_call_weather", gjson.GetBytes(body, "output.0.id").String())
		require.Equal(t, "call_weather", gjson.GetBytes(body, "output.0.call_id").String())
		require.Equal(t, "get_weather", gjson.GetBytes(body, "output.0.name").String())
		require.Equal(t, `{"city":"SF"}`, gjson.GetBytes(body, "output.0.arguments").String())
		require.Equal(t, "custom_tool_call", gjson.GetBytes(body, "output.1.type").String())
		require.Equal(t, "fc_call_shell", gjson.GetBytes(body, "output.1.id").String())
		require.Equal(t, "call_shell", gjson.GetBytes(body, "output.1.call_id").String())
		require.Equal(t, "shell", gjson.GetBytes(body, "output.1.name").String())
		require.Equal(t, "pwd", gjson.GetBytes(body, "output.1.input").String())
		require.Equal(t, int64(7), gjson.GetBytes(body, "usage.input_tokens_details.cached_tokens").Int())

		stored, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_tools",
			SessionKey:    "session_tools",
			IsolationKey:  "iso_tools",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.NoError(t, err)
		require.Equal(t, "need both tools", stored.ReasoningContent)
		require.True(t, stored.ReasoningContentPresent)
		require.Len(t, stored.ToolCalls, 2)
		require.Equal(t, "get_weather", stored.ToolCalls[0].Name)
		require.Equal(t, "get_weather", stored.ToolCalls[0].Alias)
		require.Equal(t, "shell", stored.ToolCalls[1].Name)
		require.Equal(t, "custom__shell", stored.ToolCalls[1].Alias)
	})

	t.Run("maps finish reasons conservatively", func(t *testing.T) {
		tests := []struct {
			name         string
			finishReason string
			wantStatus   string
			wantEventual string
		}{
			{name: "length", finishReason: "length", wantStatus: "incomplete", wantEventual: "max_output_tokens"},
			{name: "insufficient_system_resource", finishReason: "insufficient_system_resource", wantStatus: "incomplete", wantEventual: "insufficient_system_resource"},
			{name: "unknown", finishReason: "mystery_reason", wantStatus: "incomplete", wantEventual: "mystery_reason"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				model := CodexGatewayModel{
					Slug:          "deepseek-v4-pro",
					Provider:      "deepseek",
					UpstreamModel: "deepseek-v4-pro",
				}
				req := CodexGatewayResponsesCreateRequest{
					Model: "deepseek-v4-pro",
					Input: json.RawMessage(`[
						{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
					]`),
				}

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{
						"id":"chatcmpl_incomplete",
						"object":"chat.completion",
						"model":"deepseek-v4-pro",
						"choices":[
							{
								"index":0,
								"message":{"role":"assistant","content":"partial"},
								"finish_reason":"` + tt.finishReason + `"
							}
						]
					}`))
				}))
				defer server.Close()

				result, err := ExecuteCodexGatewayDeepSeekAdapter(
					context.Background(),
					server.Client(),
					server.URL,
					"test-key",
					model,
					req,
					nil,
					CodexGatewayDeepSeekRequestContext{SessionKey: "session_incomplete", IsolationKey: "iso_incomplete"},
					CodexGatewayDeepSeekRequestConfig{},
				)
				require.NoError(t, err)
				require.Equal(t, tt.wantStatus, result.ProviderResult.Response.Status)
				require.Equal(t, tt.wantStatus, gjson.GetBytes(result.ServiceResponse.Body, "status").String())
				require.Equal(t, tt.wantEventual, gjson.GetBytes(result.ServiceResponse.Body, "incomplete_details.reason").String())
				require.NotEqual(t, "function_call", gjson.GetBytes(result.ServiceResponse.Body, "output.0.type").String())
				require.NotEqual(t, "custom_tool_call", gjson.GetBytes(result.ServiceResponse.Body, "output.0.type").String())
			})
		}
	})

	t.Run("does not persist incomplete tool call state", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"use tools"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}
			]`),
		}
		stateStore := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
			TTL:      time.Minute,
			MaxItems: 4,
			Now:      time.Now,
		})
		reqCtx := CodexGatewayDeepSeekRequestContext{
			SessionKey:   "session_incomplete_tools",
			IsolationKey: "iso_incomplete_tools",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl_incomplete_tools",
				"object":"chat.completion",
				"model":"deepseek-v4-pro",
				"choices":[
					{
						"index":0,
						"message":{
							"role":"assistant",
							"content":"",
							"reasoning_content":"partial tool call",
							"tool_calls":[
								{
									"id":"call_weather",
									"type":"function",
									"function":{"name":"get_weather","arguments":"{\"city\":\""}
								}
							]
						},
						"finish_reason":"length"
					}
				],
				"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}
			}`))
		}))
		defer server.Close()

		result, err := ExecuteCodexGatewayDeepSeekAdapter(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
		)
		require.NoError(t, err)
		require.Equal(t, "incomplete", result.ProviderResult.Response.Status)
		require.NotEqual(t, "function_call", gjson.GetBytes(result.ServiceResponse.Body, "output.0.type").String())
		require.NotEqual(t, "custom_tool_call", gjson.GetBytes(result.ServiceResponse.Body, "output.0.type").String())

		_, err = stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_incomplete_tools",
			SessionKey:    "session_incomplete_tools",
			IsolationKey:  "iso_incomplete_tools",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCodexGatewayStateNotFound)
	})

	t.Run("skips anonymous tool calls in non stream output", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"use tools"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl_anon_tools",
				"object":"chat.completion",
				"model":"deepseek-v4-pro",
				"choices":[
					{
						"index":0,
						"message":{
							"role":"assistant",
							"content":"",
							"tool_calls":[
								{
									"type":"function",
									"function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}
								}
							]
						},
						"finish_reason":"tool_calls"
					}
				],
				"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}
			}`))
		}))
		defer server.Close()

		result, err := ExecuteCodexGatewayDeepSeekAdapter(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_anon_tools", IsolationKey: "iso_anon_tools"},
			CodexGatewayDeepSeekRequestConfig{},
		)
		require.NoError(t, err)
		require.Len(t, result.ProviderResult.ToolCalls, 0)
		require.Equal(t, int64(0), gjson.GetBytes(result.ServiceResponse.Body, "output.#").Int())
	})

	t.Run("maps upstream 400 to responses error json", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("data: {\"id\":\"chunk_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"leak\"}}]}"))
		}))
		defer server.Close()

		result, err := ExecuteCodexGatewayDeepSeekAdapter(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_err", IsolationKey: "iso_err"},
			CodexGatewayDeepSeekRequestConfig{},
		)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, result.ServiceResponse.StatusCode)
		require.JSONEq(t, `{
			"error":{
				"type":"invalid_request_error",
				"code":"invalid_request",
				"message":"DeepSeek request was rejected."
			}
		}`, string(result.ServiceResponse.Body))
		require.NotContains(t, string(result.ServiceResponse.Body), "chat.completion.chunk")
		require.NotContains(t, string(result.ServiceResponse.Body), "delta")
	})
}

func TestCodexGatewayDeepSeekToolCallOutputItem_PreservesNamespaceForCodex(t *testing.T) {
	item, stored, ok := codexGatewayDeepSeekToolCallOutputItem(apicompat.ChatToolCall{
		ID: "call_click",
		Function: apicompat.ChatFunctionCall{
			Name:      "mcp_computer_use_click_123456",
			Arguments: `{"x":10,"y":20}`,
		},
	}, map[string]CodexGatewayToolNameMapEntry{
		"mcp_computer_use_click_123456": {
			Alias:     "mcp_computer_use_click_123456",
			Kind:      CodexGatewayToolKindNamespace,
			Namespace: "mcp__computer_use__",
			Name:      "click",
		},
	}, nil)
	require.True(t, ok)
	require.Equal(t, "function_call", item["type"])
	require.Equal(t, "click", item["name"])
	require.Equal(t, "mcp__computer_use__", item["namespace"])
	require.Equal(t, "mcp_computer_use_click_123456", stored.Alias)
	require.Equal(t, "click", stored.Name)
}

func TestCodexGatewayDeepSeekToolCallOutputItem_UsesFunctionCallForShellExec(t *testing.T) {
	item, stored, ok := codexGatewayDeepSeekToolCallOutputItem(apicompat.ChatToolCall{
		ID: "call_shell",
		Function: apicompat.ChatFunctionCall{
			Name:      "shell__exec",
			Arguments: `{"cmd":"pwd"}`,
		},
	}, map[string]CodexGatewayToolNameMapEntry{
		"shell__exec": {
			Alias:     "shell__exec",
			Kind:      CodexGatewayToolKindNamespace,
			Namespace: "shell",
			Name:      "exec",
		},
	}, nil)
	require.True(t, ok)
	require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, item["type"])
	require.Equal(t, "shell", item["namespace"])
	require.Equal(t, "exec", item["name"])
	require.Equal(t, `{"cmd":"pwd"}`, item["arguments"])
	require.NotContains(t, item, "action")
	require.Equal(t, "exec", stored.Name)
}

func TestCodexGatewayDeepSeekToolCallOutputItem_UnwrapsCustomToolInput(t *testing.T) {
	rawArguments := `{"custom__apply_patch":"*** Begin Patch\n*** Add File: probe.txt\n+codex-gateway-custom\n*** End Patch\n"}`
	item, stored, ok := codexGatewayDeepSeekToolCallOutputItem(apicompat.ChatToolCall{
		ID: "call_patch",
		Function: apicompat.ChatFunctionCall{
			Name:      "custom__apply_patch",
			Arguments: rawArguments,
		},
	}, map[string]CodexGatewayToolNameMapEntry{
		"custom__apply_patch": {
			Alias: "custom__apply_patch",
			Kind:  CodexGatewayToolKindCustom,
			Name:  "apply_patch",
		},
	}, nil)
	require.True(t, ok)
	require.Equal(t, "custom_tool_call", item["type"])
	require.Equal(t, "apply_patch", item["name"])
	require.Equal(t, "*** Begin Patch\n*** Add File: probe.txt\n+codex-gateway-custom\n*** End Patch\n", item["input"])
	require.Equal(t, rawArguments, stored.Arguments)
}
