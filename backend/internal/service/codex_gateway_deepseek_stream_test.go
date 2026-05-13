package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexGatewayDeepSeekStream(t *testing.T) {
	t.Run("emits completed terminal event and swallows done", func(t *testing.T) {
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
		reqCtx := CodexGatewayDeepSeekRequestContext{
			SessionKey:   "session_stream_text",
			IsolationKey: "iso_stream_text",
		}
		prepared, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, reqCtx, CodexGatewayDeepSeekRequestConfig{})
		require.NoError(t, err)
		expectedBody := cloneCodexGatewayStreamBody(prepared.Body)
		expectedBody["stream"] = true
		expectedBody["stream_options"] = map[string]any{"include_usage": true}
		expectedJSON, err := json.Marshal(expectedBody)
		require.NoError(t, err)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, string(expectedJSON), string(body))

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("x-request-id", "rid_stream_text")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_text","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"plan "},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_text","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_text","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
				"",
				`data: {"id":"chatcmpl_stream_text","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9,"prompt_cache_hit_tokens":3}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Equal(t, 3, result.ProviderResult.Usage.CacheReadInputTokens)
		require.Equal(t, "plan ", result.ProviderResult.ReasoningContent)

		stream := buf.String()
		require.NotContains(t, stream, "[DONE]")
		events := parseCodexGatewayOrderedEvents(t, stream)
		require.NotEmpty(t, events)
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.created"))
		require.Equal(t, "response.completed", events[len(events)-1].Event)
		require.Contains(t, stream, "event: response.output_item.added")
		require.Contains(t, stream, "event: response.content_part.added")
		require.Contains(t, stream, "event: response.content_part.done")
		require.Contains(t, stream, "event: response.output_text.done")
		require.Contains(t, stream, "event: response.output_text.delta")
		require.Contains(t, stream, "event: response.output_item.done")
		require.NotContains(t, stream, "event: response.reasoning_text.delta")
		require.NotContains(t, stream, "event: response.reasoning_text.done")
		require.NotContains(t, stream, "plan ")
		addedPayload := firstCodexGatewayEventPayload(t, events, "response.output_item.added")
		require.Equal(t, "in_progress", gjson.GetBytes(addedPayload, "item.status").String())
		require.Equal(t, "message", gjson.GetBytes(addedPayload, "item.type").String())

		terminal := events[len(events)-1].Payload
		require.Equal(t, "completed", gjson.GetBytes(terminal, "response.status").String())
		require.Equal(t, "message", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "hello world", gjson.GetBytes(terminal, "response.output.0.content.0.text").String())
		require.Equal(t, int64(3), gjson.GetBytes(terminal, "response.usage.input_tokens_details.cached_tokens").Int())
	})

	t.Run("does not expose upstream reasoning text in client-visible stream", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"think privately"}]}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_hidden_reasoning","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"private chain"},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_hidden_reasoning","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_hidden_reasoning", IsolationKey: "iso_hidden_reasoning"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "private chain", result.ProviderResult.ReasoningContent)

		stream := buf.String()
		require.NotContains(t, stream, "private chain")
		require.NotContains(t, stream, "response.reasoning_text.delta")
		require.NotContains(t, stream, "response.reasoning_text.done")
		require.NotContains(t, stream, `"type":"reasoning"`)

		events := parseCodexGatewayOrderedEvents(t, stream)
		terminal := events[len(events)-1].Payload
		require.Equal(t, "message", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "done", gjson.GetBytes(terminal, "response.output.0.content.0.text").String())
	})

	t.Run("streams function and custom tool call deltas and stores reasoning", func(t *testing.T) {
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
			SessionKey:   "session_stream_tools",
			IsolationKey: "iso_stream_tools",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_tools","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"need tools"},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_tools","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_tools","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"SF\"}"}}]},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_tools","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_shell","type":"function","function":{"name":"custom__shell","arguments":"pwd"}}]},"finish_reason":"tool_calls"}]}`,
				"",
				`data: {"id":"chatcmpl_stream_tools","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":2}}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Len(t, result.ProviderResult.ToolCalls, 2)
		require.Equal(t, "need tools", result.ProviderResult.ReasoningContent)

		stream := buf.String()
		require.Contains(t, stream, "event: response.content_part.added")
		require.Contains(t, stream, "event: response.content_part.done")
		require.Contains(t, stream, "event: response.function_call_arguments.delta")
		require.Contains(t, stream, "event: response.function_call_arguments.done")
		require.Contains(t, stream, "event: response.custom_tool_call_input.delta")
		require.Contains(t, stream, "event: response.custom_tool_call_input.done")
		require.NotContains(t, stream, "event: response.reasoning_text.delta")
		require.NotContains(t, stream, "event: response.reasoning_text.done")
		require.NotContains(t, stream, "need tools")
		events := parseCodexGatewayOrderedEvents(t, stream)
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.created"))
		require.Equal(t, "response.completed", events[len(events)-1].Event)
		addedPayload := firstCodexGatewayEventPayload(t, events, "response.output_item.added")
		require.Equal(t, "in_progress", gjson.GetBytes(addedPayload, "item.status").String())
		terminal := events[len(events)-1].Payload
		require.Equal(t, "message", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "正在使用工具继续推进。\n", gjson.GetBytes(terminal, "response.output.0.content.0.text").String())
		require.Equal(t, "function_call", gjson.GetBytes(terminal, "response.output.1.type").String())
		require.Equal(t, `{"city":"SF"}`, gjson.GetBytes(terminal, "response.output.1.arguments").String())
		require.Equal(t, "custom_tool_call", gjson.GetBytes(terminal, "response.output.2.type").String())
		require.Equal(t, "pwd", gjson.GetBytes(terminal, "response.output.2.input").String())

		funcDeltaPayload := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.delta")
		require.Equal(t, "fc_call_weather", gjson.GetBytes(funcDeltaPayload, "item_id").String())
		require.Equal(t, int64(1), gjson.GetBytes(funcDeltaPayload, "output_index").Int())
		funcDonePayload := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.done")
		require.Equal(t, "fc_call_weather", gjson.GetBytes(funcDonePayload, "item_id").String())
		require.Equal(t, int64(1), gjson.GetBytes(funcDonePayload, "output_index").Int())
		require.Equal(t, "fc_call_weather", gjson.GetBytes(funcDonePayload, "item.id").String())
		customDeltaPayload := firstCodexGatewayEventPayload(t, events, "response.custom_tool_call_input.delta")
		require.Equal(t, "fc_call_shell", gjson.GetBytes(customDeltaPayload, "item_id").String())
		require.Equal(t, "call_shell", gjson.GetBytes(customDeltaPayload, "call_id").String())
		require.Equal(t, "pwd", gjson.GetBytes(customDeltaPayload, "delta").String())
		customDonePayload := firstCodexGatewayEventPayload(t, events, "response.custom_tool_call_input.done")
		require.Equal(t, "fc_call_shell", gjson.GetBytes(customDonePayload, "item_id").String())
		require.Equal(t, "pwd", gjson.GetBytes(customDonePayload, "input").String())

		stored, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_stream_tools",
			SessionKey:    "session_stream_tools",
			IsolationKey:  "iso_stream_tools",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.NoError(t, err)
		require.Equal(t, "need tools", stored.ReasoningContent)
		require.Len(t, stored.ToolCalls, 2)
		require.Equal(t, "custom__shell", stored.ToolCalls[1].Alias)
	})

	t.Run("streams unwrapped custom tool input and custom done event", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"patch a file"}]}
			]`),
			Tools: json.RawMessage(`[
				{
					"type":"custom",
					"name":"apply_patch",
					"description":"edit files",
					"format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}
				}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_custom_patch","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_patch","type":"function","function":{"name":"custom__apply_patch","arguments":"{\"input\":\"*** Begin"}}]},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_custom_patch","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":" Patch\\n*** End Patch\"}"}}]},"finish_reason":"tool_calls"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_custom_patch", IsolationKey: "iso_custom_patch"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.custom_tool_call_input.delta"))
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.custom_tool_call_input.done"))
		require.Equal(t, 0, countCodexGatewayEvent(events, "response.function_call_arguments.done"))

		customDeltaPayload := firstCodexGatewayEventPayload(t, events, "response.custom_tool_call_input.delta")
		require.Equal(t, "fc_call_patch", gjson.GetBytes(customDeltaPayload, "item_id").String())
		require.Equal(t, "*** Begin Patch\n*** End Patch", gjson.GetBytes(customDeltaPayload, "delta").String())
		customDonePayload := firstCodexGatewayEventPayload(t, events, "response.custom_tool_call_input.done")
		require.Equal(t, "fc_call_patch", gjson.GetBytes(customDonePayload, "item_id").String())
		require.Equal(t, "*** Begin Patch\n*** End Patch", gjson.GetBytes(customDonePayload, "input").String())

		terminal := events[len(events)-1].Payload
		require.Equal(t, "message", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "正在使用工具继续推进。\n", gjson.GetBytes(terminal, "response.output.0.content.0.text").String())
		require.Equal(t, "custom_tool_call", gjson.GetBytes(terminal, "response.output.1.type").String())
		require.Equal(t, "*** Begin Patch\n*** End Patch", gjson.GetBytes(terminal, "response.output.1.input").String())
	})

	t.Run("emits safe activity text before tool-only turns", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"use a tool"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_activity","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}]},"finish_reason":"tool_calls"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_activity", IsolationKey: "iso_activity"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		firstAdded := firstCodexGatewayEventPayload(t, events, "response.output_item.added")
		require.Equal(t, "message", gjson.GetBytes(firstAdded, "item.type").String())
		require.Equal(t, "assistant", gjson.GetBytes(firstAdded, "item.role").String())
		require.Equal(t, "in_progress", gjson.GetBytes(firstAdded, "item.status").String())
		require.Contains(t, buf.String(), "event: response.output_text.delta")
		require.Contains(t, buf.String(), "正在使用工具继续推进。")

		activityDoneIdx := indexCodexGatewayEvent(events, "response.output_text.done")
		functionDoneIdx := indexCodexGatewayEvent(events, "response.function_call_arguments.done")
		require.GreaterOrEqual(t, activityDoneIdx, 0)
		require.GreaterOrEqual(t, functionDoneIdx, 0)
		require.Less(t, activityDoneIdx, functionDoneIdx)

		terminal := events[len(events)-1].Payload
		require.Equal(t, "message", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "正在使用工具继续推进。\n", gjson.GetBytes(terminal, "response.output.0.content.0.text").String())
		require.Equal(t, "function_call", gjson.GetBytes(terminal, "response.output.1.type").String())
		require.Equal(t, `{"city":"SF"}`, gjson.GetBytes(terminal, "response.output.1.arguments").String())
	})

	t.Run("buffers tool argument deltas until id and name are known", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"use delayed tool"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_delayed","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\""}}]},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_delayed","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"SF\"}"}}]},"finish_reason":"tool_calls"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_delayed", IsolationKey: "iso_delayed"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		addedIdx := indexCodexGatewayEvent(events, "response.output_item.added")
		deltaIdx := indexCodexGatewayEvent(events, "response.function_call_arguments.delta")
		require.GreaterOrEqual(t, addedIdx, 0)
		require.GreaterOrEqual(t, deltaIdx, 0)
		require.Less(t, addedIdx, deltaIdx)

		funcDeltaPayload := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.delta")
		require.Equal(t, "fc_call_weather", gjson.GetBytes(funcDeltaPayload, "item_id").String())
		require.Equal(t, `{"city":"SF"}`, gjson.GetBytes(events[len(events)-1].Payload, "response.output.1.arguments").String())
	})

	t.Run("done event and stored tool order follow output index", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"mixed order"}]}
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
			SessionKey:   "session_ordered_done",
			IsolationKey: "iso_ordered_done",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_ordered_done","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}]},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_ordered_done","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"reasoning_content":"later reasoning"},"finish_reason":"tool_calls"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Equal(t, "message", gjson.GetBytes(result.ProviderResult.Response.Output[0], "type").String())
		require.Equal(t, "function_call", gjson.GetBytes(result.ProviderResult.Response.Output[1], "type").String())
		require.Len(t, result.ProviderResult.Response.Output, 2)
		require.Len(t, result.ProviderResult.ToolCalls, 1)
		require.Equal(t, "get_weather", result.ProviderResult.ToolCalls[0].Name)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		functionDoneIdx := indexCodexGatewayEvent(events, "response.function_call_arguments.done")
		require.GreaterOrEqual(t, functionDoneIdx, 0)
		require.Equal(t, -1, indexCodexGatewayEvent(events, "response.reasoning_text.done"))

		stored, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_stream_ordered_done",
			SessionKey:    "session_ordered_done",
			IsolationKey:  "iso_ordered_done",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.NoError(t, err)
		require.Len(t, stored.ToolCalls, 1)
		require.Equal(t, "get_weather", stored.ToolCalls[0].Name)
	})

	t.Run("closes incomplete when upstream ends early", func(t *testing.T) {
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
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_cut","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":null}]}`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_cut", IsolationKey: "iso_cut"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "incomplete", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, "response.incomplete", events[len(events)-1].Event)
		require.Equal(t, "partial", gjson.GetBytes(events[len(events)-1].Payload, "response.output.0.content.0.text").String())
	})

	t.Run("empty reasoning delta still yields incomplete on upstream close", func(t *testing.T) {
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
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_empty_reasoning","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":""},"finish_reason":null}]}`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_empty_reasoning", IsolationKey: "iso_empty_reasoning"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "incomplete", result.ProviderResult.Response.Status)
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, "response.incomplete", events[len(events)-1].Event)
	})

	t.Run("does not persist partial tool call state on early stream close", func(t *testing.T) {
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
			SessionKey:   "session_partial_tool",
			IsolationKey: "iso_partial_tool",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_partial_tool","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "incomplete", result.ProviderResult.Response.Status)
		require.Len(t, result.ProviderResult.ToolCalls, 0)
		require.NotContains(t, buf.String(), "event: response.function_call_arguments.done")
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, int64(1), gjson.GetBytes(events[len(events)-1].Payload, "response.output.#").Int())
		require.Equal(t, "message", gjson.GetBytes(events[len(events)-1].Payload, "response.output.0.type").String())

		_, err = stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_stream_partial_tool",
			SessionKey:    "session_partial_tool",
			IsolationKey:  "iso_partial_tool",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCodexGatewayStateNotFound)
	})

	t.Run("does not serialize anonymous tool slots on terminal tool_calls", func(t *testing.T) {
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
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_anonymous","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"SF\"}"}}]},"finish_reason":"tool_calls"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_anon", IsolationKey: "iso_anon"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Len(t, result.ProviderResult.ToolCalls, 0)
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, int64(0), gjson.GetBytes(events[len(events)-1].Payload, "response.output.#").Int())
	})

	t.Run("maps insufficient system resource to non completed terminal event", func(t *testing.T) {
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
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_resource","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":"insufficient_system_resource"}]}`,
				"",
				`data: {"id":"chatcmpl_stream_resource","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[],"usage":{"prompt_tokens":6,"completion_tokens":1,"total_tokens":7}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_resource", IsolationKey: "iso_resource"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "incomplete", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, "response.incomplete", events[len(events)-1].Event)
		require.NotContains(t, buf.String(), "event: response.completed")
	})

	t.Run("maps 4xx failure with explicit empty output", func(t *testing.T) {
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
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","code":"invalid_request","message":"bad request"}}`)
		}))
		defer server.Close()

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_4xx", IsolationKey: "iso_4xx"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "failed", result.ProviderResult.Response.Status)
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, "response.failed", events[len(events)-1].Event)
		require.Equal(t, int64(0), gjson.GetBytes(events[len(events)-1].Payload, "response.output.#").Int())
		require.Equal(t, "invalid_request_error", gjson.GetBytes(events[len(events)-1].Payload, "response.error.type").String())
		require.Equal(t, "invalid_request", gjson.GetBytes(events[len(events)-1].Payload, "response.error.code").String())
		require.Equal(t, "bad request", gjson.GetBytes(events[len(events)-1].Payload, "response.error.message").String())
	})

	t.Run("writer failure during terminal events prevents persistence", func(t *testing.T) {
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
			SessionKey:   "session_writer_fail_terminal",
			IsolationKey: "iso_writer_fail_terminal",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_writer_fail_terminal","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}]},"finish_reason":"tool_calls"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		writer := &failingCodexGatewayStreamWriter{failOn: "event: response.function_call_arguments.done"}
		_, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			reqCtx,
			CodexGatewayDeepSeekRequestConfig{},
			writer,
		)
		require.Error(t, err)
		_, err = stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_stream_writer_fail_terminal",
			SessionKey:    "session_writer_fail_terminal",
			IsolationKey:  "iso_writer_fail_terminal",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCodexGatewayStateNotFound)
	})
}

type codexGatewayOrderedEvent struct {
	Event   string
	Payload []byte
}

func parseCodexGatewayOrderedEvents(t *testing.T, stream string) []codexGatewayOrderedEvent {
	t.Helper()

	blocks := strings.Split(strings.TrimSpace(stream), "\n\n")
	out := make([]codexGatewayOrderedEvent, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		require.Len(t, lines, 2)
		out = append(out, codexGatewayOrderedEvent{
			Event:   strings.TrimPrefix(lines[0], "event: "),
			Payload: []byte(strings.TrimPrefix(lines[1], "data: ")),
		})
	}
	return out
}

func countCodexGatewayEvent(events []codexGatewayOrderedEvent, name string) int {
	count := 0
	for _, event := range events {
		if event.Event == name {
			count++
		}
	}
	return count
}

func firstCodexGatewayEventPayload(t *testing.T, events []codexGatewayOrderedEvent, name string) []byte {
	t.Helper()
	for _, event := range events {
		if event.Event == name {
			return event.Payload
		}
	}
	t.Fatalf("event %s not found", name)
	return nil
}

func indexCodexGatewayEvent(events []codexGatewayOrderedEvent, name string) int {
	for i, event := range events {
		if event.Event == name {
			return i
		}
	}
	return -1
}

type failingCodexGatewayStreamWriter struct {
	failOn string
}

func (w *failingCodexGatewayStreamWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.failOn) {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
