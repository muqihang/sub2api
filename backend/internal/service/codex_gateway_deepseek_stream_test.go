package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexGatewayDeepSeekStream_PersistsOrdinaryAssistantState(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}
	reqCtx := CodexGatewayDeepSeekRequestContext{SessionKey: "session_stream_ordinary", IsolationKey: "iso_stream_ordinary"}
	stateStore := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_stream_ordinary","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"brief plan"},"finish_reason":null}]}`,
			"",
			`data: {"id":"chatcmpl_stream_ordinary","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"hello there"},"finish_reason":"stop"}]}`,
			"",
			`data: [DONE]`,
			"",
		}, "\n"))
	}))
	defer server.Close()

	var buf bytes.Buffer
	_, err := ExecuteCodexGatewayDeepSeekStream(context.Background(), server.Client(), server.URL, "test-key", model, req, stateStore, reqCtx, CodexGatewayDeepSeekRequestConfig{}, &buf)
	require.NoError(t, err)

	state, err := stateStore.Get(CodexGatewayStateLookupKey{
		ResponseID:    "chatcmpl_stream_ordinary",
		SessionKey:    "session_stream_ordinary",
		IsolationKey:  "iso_stream_ordinary",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	})
	require.NoError(t, err)
	require.Equal(t, "hello there", state.AssistantContent)
	require.True(t, state.AssistantContentPresent)
	require.Equal(t, "brief plan", state.ReasoningContent)
	require.True(t, state.ReasoningContentPresent)
	require.Len(t, state.ReplayMessages, 2)
	var replayAssistant map[string]any
	require.NoError(t, json.Unmarshal(state.ReplayMessages[1], &replayAssistant))
	require.Equal(t, "assistant", replayAssistant["role"])
	require.Equal(t, "hello there", replayAssistant["content"])
	require.Equal(t, "brief plan", replayAssistant["reasoning_content"])
}

func TestCodexGatewayDeepSeekStream_EmitsOutputTextDeltasAsChunksArrive(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}

	firstChunkFlushed := make(chan string, 1)
	secondChunkFlushed := make(chan string, 1)
	secondChunk := make(chan struct{})
	finishStream := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_stream_live_text\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello \"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		select {
		case got := <-firstChunkFlushed:
			require.Contains(t, got, "event: response.output_item.added")
			require.Contains(t, got, "event: response.content_part.added")
			require.Contains(t, got, "event: response.output_text.delta")
			require.Contains(t, got, `"delta":"hello "`)
		case <-time.After(2 * time.Second):
			t.Fatal("first text chunk was not flushed before the next upstream chunk")
		}
		close(secondChunk)

		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_stream_live_text\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		select {
		case got := <-secondChunkFlushed:
			require.Contains(t, got, `"delta":"world"`)
		case <-time.After(2 * time.Second):
			t.Fatal("second text chunk was not flushed before terminal chunk")
		}
		<-finishStream

		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_stream_live_text\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	dst := &observableBuffer{
		onWrite: func(current string) {
			if strings.Contains(current, `"delta":"hello "`) {
				select {
				case firstChunkFlushed <- current:
				default:
				}
			}
			if strings.Contains(current, `"delta":"world"`) {
				select {
				case secondChunkFlushed <- current:
				default:
				}
			}
		},
	}
	resultCh := make(chan struct {
		result CodexGatewayDeepSeekAdapterResult
		err    error
	}, 1)
	go func() {
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_live_text", IsolationKey: "iso_live_text"},
			CodexGatewayDeepSeekRequestConfig{},
			dst,
		)
		resultCh <- struct {
			result CodexGatewayDeepSeekAdapterResult
			err    error
		}{result: result, err: err}
	}()
	<-secondChunk
	close(finishStream)
	outcome := <-resultCh
	require.NoError(t, outcome.err)
	require.Equal(t, "completed", outcome.result.ProviderResult.Response.Status)

	events := parseCodexGatewayOrderedEvents(t, dst.String())
	require.Equal(t, 1, countCodexGatewayEvent(events, "response.output_item.added"))
	require.Equal(t, 1, countCodexGatewayEvent(events, "response.content_part.added"))
	require.Equal(t, 2, countCodexGatewayEvent(events, "response.output_text.delta"))
	require.Equal(t, "hello ", gjson.GetBytes(nthCodexGatewayEventPayload(t, events, "response.output_text.delta", 0), "delta").String())
	require.Equal(t, "world", gjson.GetBytes(nthCodexGatewayEventPayload(t, events, "response.output_text.delta", 1), "delta").String())
	require.Equal(t, "hello world", gjson.GetBytes(firstCodexGatewayEventPayload(t, events, "response.output_text.done"), "text").String())
	require.Equal(t, "hello world", gjson.GetBytes(firstCodexGatewayEventPayload(t, events, "response.content_part.done"), "part.text").String())
	require.Equal(t, "response.completed", events[len(events)-1].Event)
}

func TestCodexGatewayDeepSeekStream_PreservesPartialTextWhenTerminalMissing(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_stream_partial_text","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"partial answer"},"finish_reason":null}]}`,
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
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_partial_text", IsolationKey: "iso_partial_text"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
	require.Equal(t, "incomplete", result.ProviderResult.Response.Status)
	require.Contains(t, buf.String(), "event: response.output_text.delta")
	require.Contains(t, buf.String(), `"delta":"partial answer"`)
	events := parseCodexGatewayOrderedEvents(t, buf.String())
	require.Equal(t, "response.incomplete", events[len(events)-1].Event)
	require.Equal(t, "partial answer", gjson.GetBytes(events[len(events)-1].Payload, "response.output.0.content.0.text").String())
}

func TestCodexGatewayDeepSeekStream_HostedWebSearchErrorWritesTerminalEvent(t *testing.T) {
	origSearch := codexGatewayExecuteHostedWebSearchFunc
	t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
	codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
		return "", fmt.Errorf("search failed with marker FAKE_SECRET_MARKER for %s", query)
	}

	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"search current news"}]}
		]`),
		Tools: json.RawMessage(`[{"type":"web_search"}]`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_stream_hosted_search_error","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"current news\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_hosted_search_error", IsolationKey: "iso_hosted_search_error"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
	require.Equal(t, "failed", result.ProviderResult.Response.Status)

	stream := buf.String()
	require.Contains(t, stream, "event: response.web_search_call.in_progress")
	require.Contains(t, stream, "event: response.web_search_call.searching")
	require.NotContains(t, stream, "FAKE_SECRET_MARKER")
	events := parseCodexGatewayOrderedEvents(t, stream)
	require.Equal(t, "response.failed", events[len(events)-1].Event)
	require.NotEqual(t, "response.web_search_call.searching", events[len(events)-1].Event)
	require.Equal(t, "hosted_tool_error", gjson.GetBytes(events[len(events)-1].Payload, "response.error.code").String())
}

func TestCodexGatewayDeepSeekStream_ToolSearchFunctionCallEmitsToolSearchCall(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"find subagent tool"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
		]`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, deepSeekAdapterToolNames(body), "tool_search")

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_tool_search","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_tool_search","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"spawn_agent\",\"limit\":10}"}}]},"finish_reason":"tool_calls"}]}`,
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
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_tool_search_stream", IsolationKey: "iso_tool_search_stream"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
	require.Equal(t, "completed", result.ProviderResult.Response.Status)
	require.Len(t, result.ProviderResult.ToolCalls, 1)
	require.Equal(t, "tool_search", result.ProviderResult.ToolCalls[0].Alias)

	stream := buf.String()
	require.NotContains(t, stream, "event: response.function_call_arguments.delta")
	require.NotContains(t, stream, "event: response.function_call_arguments.done")
	require.NotContains(t, stream, `"type":"function_call"`)

	events := parseCodexGatewayOrderedEvents(t, stream)
	added := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "tool_search_call")
	require.Equal(t, "call_tool_search", gjson.GetBytes(added, "item.call_id").String())
	require.Equal(t, "client", gjson.GetBytes(added, "item.execution").String())
	require.Equal(t, "spawn_agent", gjson.GetBytes(added, "item.arguments.query").String())
	require.Equal(t, int64(10), gjson.GetBytes(added, "item.arguments.limit").Int())
	done := firstCodexGatewayOutputItemDonePayloadByType(t, events, "tool_search_call")
	require.Equal(t, "completed", gjson.GetBytes(done, "item.status").String())
	require.Equal(t, "client", gjson.GetBytes(done, "item.execution").String())
	terminal := events[len(events)-1].Payload
	toolOutput := codexGatewayTerminalOutputByType(t, terminal, "tool_search_call")
	require.Equal(t, "spawn_agent", gjson.GetBytes(toolOutput, "arguments.query").String())
}

func TestCodexGatewayDeepSeekStream_MapsDeferredNamespaceToolCallFromToolSearchOutput(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"spawn one explorer"}]},
			{"type":"tool_search_call","call_id":"call_tool_search","status":"completed","execution":"client","arguments":{"query":"spawn_agent","limit":10}},
			{"type":"tool_search_output","call_id":"call_tool_search","status":"completed","execution":"client","tools":[
				{"type":"namespace","name":"multi_agent_v1","tools":[
					{"type":"function","name":"spawn_agent","parameters":{"type":"object","properties":{"message":{"type":"string"},"model":{"type":"string"}}}}
				]}
			]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
		]`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_deferred_spawn_stream","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_spawn","type":"function","function":{"name":"multi_agent_v1__spawn_agent","arguments":"{\"message\":\"say ready\",\"model\":\"deepseek-v4-flash\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_deferred_spawn_stream", IsolationKey: "iso_deferred_spawn_stream"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
	require.Equal(t, "completed", result.ProviderResult.Response.Status)

	stream := buf.String()
	require.NotContains(t, stream, `"name":"multi_agent_v1__spawn_agent"`)
	events := parseCodexGatewayOrderedEvents(t, stream)
	added := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "function_call")
	require.Equal(t, "spawn_agent", gjson.GetBytes(added, "item.name").String())
	require.Equal(t, "multi_agent_v1", gjson.GetBytes(added, "item.namespace").String())
	done := firstCodexGatewayOutputItemDonePayloadByType(t, events, "function_call")
	require.Equal(t, "spawn_agent", gjson.GetBytes(done, "item.name").String())
	require.Equal(t, "multi_agent_v1", gjson.GetBytes(done, "item.namespace").String())
}

func TestCodexGatewayDeepSeekStream_ToolSearchFunctionCallEmitsAfterSplitArguments(t *testing.T) {
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"find subagent tool"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"tool_search","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}}
		]`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_tool_search_split","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_tool_search_split","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"spa"}}]},"finish_reason":null}]}`,
			"",
			`data: {"id":"chatcmpl_tool_search_split","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"wn_agent\",\"limit\":10}"}}]},"finish_reason":"tool_calls"}]}`,
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
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_tool_search_stream_split", IsolationKey: "iso_tool_search_stream_split"},
		CodexGatewayDeepSeekRequestConfig{},
		&buf,
	)
	require.NoError(t, err)
	require.Equal(t, "completed", result.ProviderResult.Response.Status)

	stream := buf.String()
	require.NotContains(t, stream, "event: response.function_call_arguments.delta")
	require.NotContains(t, stream, `"type":"function_call"`)

	events := parseCodexGatewayOrderedEvents(t, stream)
	added := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "tool_search_call")
	require.Equal(t, "call_tool_search_split", gjson.GetBytes(added, "item.call_id").String())
	require.Equal(t, "spawn_agent", gjson.GetBytes(added, "item.arguments.query").String())
	require.Equal(t, int64(10), gjson.GetBytes(added, "item.arguments.limit").Int())
	done := firstCodexGatewayOutputItemDonePayloadByType(t, events, "tool_search_call")
	require.Equal(t, "completed", gjson.GetBytes(done, "item.status").String())
}

func TestCodexGatewayDeepSeekStream_ToolSearchDoesNotEmitFromRepairablePartialArguments(t *testing.T) {
	state := newCodexGatewayDeepSeekStreamState(
		CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"},
		map[string]CodexGatewayToolNameMapEntry{
			"tool_search": {
				Alias: "tool_search",
				Name:  "tool_search",
				Kind:  CodexGatewayToolKindFunction,
			},
		},
	)
	var buf bytes.Buffer
	writer := NewCodexGatewayResponseEventWriter(&buf)

	err := state.consumePayload([]byte(`{"id":"chatcmpl_tool_search_partial","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_tool_search_partial","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"spa"}}]},"finish_reason":null}]}`), writer)
	require.NoError(t, err)
	require.NotContains(t, buf.String(), `"type":"tool_search_call"`)
	require.NotContains(t, buf.String(), `"type":"function_call"`)

	err = state.consumePayload([]byte(`{"id":"chatcmpl_tool_search_partial","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"wn_agent\",\"limit\":10}"}}]},"finish_reason":"tool_calls"}]}`), writer)
	require.NoError(t, err)
	_, err = state.finish(writer)
	require.NoError(t, err)

	stream := buf.String()
	require.NotContains(t, stream, "event: response.function_call_arguments.delta")
	require.NotContains(t, stream, `"type":"function_call"`)
	events := parseCodexGatewayOrderedEvents(t, stream)
	added := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "tool_search_call")
	require.Equal(t, "spawn_agent", gjson.GetBytes(added, "item.arguments.query").String())
	require.Equal(t, int64(10), gjson.GetBytes(added, "item.arguments.limit").Int())
}

func TestCodexGatewayAgnesStreamCaptureUsesProviderDoneLabel(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash", InputModalities: []string{"text", "image"}}
	req := CodexGatewayResponsesCreateRequest{Model: "agnes-2.0-flash", Input: json.RawMessage(`"hello"`)}
	captureBaseDir := t.TempDir()
	capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  captureBaseDir,
		HashKeyFile:              filepath.Join(captureBaseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer capture.Close()
	trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "agnes_stream", Provider: "agnes", Model: "agnes-2.0-flash"})
	require.NotNil(t, trace)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_agnes_stream","object":"chat.completion.chunk","model":"agnes-2.0-flash","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
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
		"sk-agnes",
		model,
		req,
		nil,
		CodexGatewayDeepSeekRequestContext{SessionKey: "session_agnes_stream", IsolationKey: "iso_agnes_stream", Provider: "agnes", CaptureTrace: trace},
		CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai", SupportsNativeImages: true},
		&buf,
	)
	require.NoError(t, err)
	capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, capture.Close())
	upstreamEvents, err := os.ReadFile(filepath.Join(captureBaseDir, time.Now().Format("2006-01-02"), "agnes_stream", "upstream_stream.events.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(upstreamEvents), "agnes.done")
	require.NotContains(t, string(upstreamEvents), "deepseek.done")
}

func TestCodexGatewayAgnesStreamUsesConfiguredUpstreamModelWhenProviderOmitModel(t *testing.T) {
	model := CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes", UpstreamModel: "agnes-2.0-flash", InputModalities: []string{"text", "image"}}
	req := CodexGatewayResponsesCreateRequest{Model: "agnes-2.0-flash", Input: json.RawMessage(`"hello"`)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl_agnes_stream_no_model","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
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
		"sk-agnes",
		model,
		req,
		nil,
		CodexGatewayDeepSeekRequestContext{Provider: "agnes"},
		CodexGatewayDeepSeekRequestConfig{Provider: "agnes", ReasoningMode: "openai", SupportsNativeImages: true},
		&buf,
	)
	require.NoError(t, err)
	require.Equal(t, "agnes-2.0-flash", result.ProviderResult.UpstreamModel)
	require.Contains(t, buf.String(), `"model":"agnes-2.0-flash"`)
}

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
		captureBaseDir := t.TempDir()
		capture := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
			Enabled:                  true,
			BaseDir:                  captureBaseDir,
			HashKeyFile:              captureBaseDir + "/.key",
			CaptureSuccessSampleRate: 1,
		})
		defer capture.Close()
		trace := capture.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "deepseek_stream"})
		require.NotNil(t, trace)
		reqCtx.CaptureTrace = trace
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
		capture.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
		require.NoError(t, capture.Close())
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Equal(t, 3, result.ProviderResult.Usage.CacheReadInputTokens)
		require.Equal(t, "plan ", result.ProviderResult.ReasoningContent)

		stream := buf.String()
		require.NotContains(t, stream, "[DONE]")
		events := parseCodexGatewayOrderedEvents(t, stream)
		require.NotEmpty(t, events)
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.created"))
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.in_progress"))
		requireSequentialCodexGatewayOrderedSequenceNumbers(t, events)
		require.Equal(t, "response.in_progress", events[1].Event)
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
		require.Equal(t, "reasoning", gjson.GetBytes(addedPayload, "item.type").String())
		require.True(t, gjson.GetBytes(addedPayload, "item.content").Exists())
		require.False(t, gjson.GetBytes(addedPayload, "item.encrypted_content").Exists())
		messageAddedPayload := nthCodexGatewayEventPayload(t, events, "response.output_item.added", 1)
		require.Equal(t, "message", gjson.GetBytes(messageAddedPayload, "item.type").String())
		require.Equal(t, "final_answer", gjson.GetBytes(messageAddedPayload, "item.phase").String())

		terminal := events[len(events)-1].Payload
		require.Equal(t, "completed", gjson.GetBytes(terminal, "response.status").String())
		require.Equal(t, "reasoning", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "message", gjson.GetBytes(terminal, "response.output.1.type").String())
		require.Equal(t, "final_answer", gjson.GetBytes(terminal, "response.output.1.phase").String())
		require.Equal(t, "hello world", gjson.GetBytes(terminal, "response.output.1.content.0.text").String())
		require.Equal(t, int64(3), gjson.GetBytes(terminal, "response.usage.input_tokens_details.cached_tokens").Int())
		upstreamEvents, err := os.ReadFile(filepath.Join(captureBaseDir, time.Now().Format("2006-01-02"), "deepseek_stream", "upstream_stream.events.jsonl"))
		require.NoError(t, err)
		require.Contains(t, string(upstreamEvents), "chat.completion.chunk")
		require.NotContains(t, string(upstreamEvents), "plan ")
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
		require.Contains(t, stream, `"type":"reasoning"`)

		events := parseCodexGatewayOrderedEvents(t, stream)
		terminal := events[len(events)-1].Payload
		require.Equal(t, "reasoning", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "message", gjson.GetBytes(terminal, "response.output.1.type").String())
		require.Equal(t, "final_answer", gjson.GetBytes(terminal, "response.output.1.phase").String())
		require.Equal(t, "done", gjson.GetBytes(terminal, "response.output.1.content.0.text").String())
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
		require.Contains(t, stream, "event: response.function_call_arguments.delta")
		require.Contains(t, stream, "event: response.function_call_arguments.done")
		require.Contains(t, stream, "event: response.custom_tool_call_input.delta")
		require.Contains(t, stream, "event: response.custom_tool_call_input.done")
		require.NotContains(t, stream, "event: response.output_text.delta")
		require.NotContains(t, stream, "event: response.content_part.added")
		require.NotContains(t, stream, "event: response.content_part.done")
		require.NotContains(t, stream, "event: response.reasoning_text.delta")
		require.NotContains(t, stream, "event: response.reasoning_text.done")
		require.NotContains(t, stream, "need tools")
		require.NotContains(t, stream, "正在使用工具继续推进")
		events := parseCodexGatewayOrderedEvents(t, stream)
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.created"))
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.in_progress"))
		requireSequentialCodexGatewayOrderedSequenceNumbers(t, events)
		require.Equal(t, "response.in_progress", events[1].Event)
		require.Equal(t, "response.completed", events[len(events)-1].Event)
		addedPayload := firstCodexGatewayEventPayload(t, events, "response.output_item.added")
		require.Equal(t, "in_progress", gjson.GetBytes(addedPayload, "item.status").String())
		require.Equal(t, "reasoning", gjson.GetBytes(addedPayload, "item.type").String())
		terminal := events[len(events)-1].Payload
		require.Equal(t, "reasoning", gjson.GetBytes(terminal, "response.output.0.type").String())
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

	t.Run("emits assistant content before tool calls as commentary phase", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect files"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
			]`),
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_commentary_tool","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"我先检查项目结构。"},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_commentary_tool","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_exec","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"pwd && ls\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_commentary_tool", IsolationKey: "iso_commentary_tool"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Len(t, result.ProviderResult.ToolCalls, 1)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		firstAdded := nthCodexGatewayEventPayload(t, events, "response.output_item.added", 0)
		require.Equal(t, "message", gjson.GetBytes(firstAdded, "item.type").String())
		messageAdded := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "message")
		require.Equal(t, "final_answer", gjson.GetBytes(messageAdded, "item.phase").String())
		toolAdded := firstCodexGatewayOutputItemAddedPayloadByType(t, events, CodexGatewayOutputItemTypeFunctionCall)
		require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, gjson.GetBytes(toolAdded, "item.type").String())
		require.Less(t, indexCodexGatewayOutputItemDoneByType(events, "message"), indexCodexGatewayOutputItemAddedByType(events, CodexGatewayOutputItemTypeFunctionCall))

		terminal := events[len(events)-1].Payload
		messageOutput := codexGatewayTerminalOutputByType(t, terminal, "message")
		require.Equal(t, "final_answer", gjson.GetBytes(messageOutput, "phase").String())
		require.Equal(t, "我先检查项目结构。", gjson.GetBytes(messageOutput, "content.0.text").String())
		require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, gjson.GetBytes(codexGatewayTerminalOutputByType(t, terminal, CodexGatewayOutputItemTypeFunctionCall), "type").String())
		requireCodexGatewayTextEventsMatchAddedMessage(t, events)
	})

	t.Run("allows repeated mutating tool calls without incomplete terminal", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect files"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
			]`),
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_duplicate_exec","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_exec_a","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"cat README.md | head\"}"}}]},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_duplicate_exec","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_exec_b","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"cat README.md | head\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_duplicate_exec", IsolationKey: "iso_duplicate_exec"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Len(t, result.ProviderResult.ToolCalls, 2)
		require.Equal(t, "call_exec_a", result.ProviderResult.ToolCalls[0].ID)
		require.Equal(t, "call_exec_b", result.ProviderResult.ToolCalls[1].ID)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, "response.completed", events[len(events)-1].Event)
		require.Equal(t, 3, countCodexGatewayEvent(events, "response.output_item.done"))
		require.Equal(t, -1, indexCodexGatewayEvent(events, "response.incomplete"))
	})

	t.Run("synthesizes reasoning shell before tool call when upstream omits reasoning", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect files"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
			]`),
		}
		stateStore := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
			TTL:      time.Minute,
			MaxItems: 4,
			Now:      time.Now,
		})
		reqCtx := CodexGatewayDeepSeekRequestContext{
			SessionKey:   "session_synthetic_reasoning",
			IsolationKey: "iso_synthetic_reasoning",
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_synthetic_reasoning","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_exec","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
		require.Len(t, result.ProviderResult.Response.Output, 2)
		require.Equal(t, "reasoning", gjson.GetBytes(result.ProviderResult.Response.Output[0], "type").String())
		require.Equal(t, "function_call", gjson.GetBytes(result.ProviderResult.Response.Output[1], "type").String())
		require.False(t, result.ProviderResult.ReasoningContentPresent)
		require.NotContains(t, buf.String(), "event: response.reasoning_text.delta")

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		reasoningAddedIdx := indexCodexGatewayOutputItemAddedByType(events, "reasoning")
		functionAddedIdx := indexCodexGatewayOutputItemAddedByType(events, "function_call")
		require.GreaterOrEqual(t, reasoningAddedIdx, 0)
		require.GreaterOrEqual(t, functionAddedIdx, 0)
		require.Less(t, reasoningAddedIdx, functionAddedIdx)

		stored, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_stream_synthetic_reasoning",
			SessionKey:    "session_synthetic_reasoning",
			IsolationKey:  "iso_synthetic_reasoning",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.NoError(t, err)
		require.True(t, stored.ReasoningContentSynthesized)
		require.False(t, stored.ReasoningContentPresent)
	})

	t.Run("emits shell namespace tools as executable function calls", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"run pwd"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_shell","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_shell","type":"function","function":{"name":"shell__exec","arguments":"{\"cmd\":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_shell", IsolationKey: "iso_shell"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		added := firstCodexGatewayOutputItemAddedPayloadByType(t, events, CodexGatewayOutputItemTypeFunctionCall)
		require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, gjson.GetBytes(added, "item.type").String())
		require.Equal(t, "shell", gjson.GetBytes(added, "item.namespace").String())
		require.Equal(t, "exec", gjson.GetBytes(added, "item.name").String())
		doneArgs := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.done")
		require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, gjson.GetBytes(doneArgs, "item.type").String())
		require.Equal(t, "shell", gjson.GetBytes(doneArgs, "item.namespace").String())
		require.Equal(t, "exec", gjson.GetBytes(doneArgs, "item.name").String())
		require.JSONEq(t, `{"cmd":"pwd"}`, gjson.GetBytes(doneArgs, "item.arguments").String())
		done := firstCodexGatewayOutputItemDonePayloadByType(t, events, CodexGatewayOutputItemTypeFunctionCall)
		require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, gjson.GetBytes(done, "item.type").String())
		require.False(t, gjson.GetBytes(done, "item.action").Exists())
		terminal := events[len(events)-1].Payload
		toolOutput := codexGatewayTerminalOutputByType(t, terminal, CodexGatewayOutputItemTypeFunctionCall)
		require.Equal(t, "shell", gjson.GetBytes(toolOutput, "namespace").String())
		require.Equal(t, "exec", gjson.GetBytes(toolOutput, "name").String())
	})

	t.Run("streams split shell function arguments without local_shell_call", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"run pwd"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_shell_split","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_shell","type":"function","function":{"name":"shell__exec","arguments":"{\"cmd\""}}]},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_shell_split","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_shell_split", IsolationKey: "iso_shell_split"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		added := firstCodexGatewayOutputItemAddedPayloadByType(t, events, CodexGatewayOutputItemTypeFunctionCall)
		require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, gjson.GetBytes(added, "item.type").String())
		require.Equal(t, "shell", gjson.GetBytes(added, "item.namespace").String())
		require.Equal(t, "exec", gjson.GetBytes(added, "item.name").String())
		require.False(t, gjson.GetBytes(added, "item.action").Exists())
		doneArgs := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.done")
		require.JSONEq(t, `{"cmd":"pwd"}`, gjson.GetBytes(doneArgs, "item.arguments").String())
	})

	t.Run("keeps assistant preamble as commentary when the same turn calls a tool", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"run pwd"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_shell_preamble","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"我来执行命令。"},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_shell_preamble","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_shell","type":"function","function":{"name":"shell__exec","arguments":"{\"cmd\":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_shell_preamble", IsolationKey: "iso_shell_preamble"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		stream := buf.String()
		require.Contains(t, stream, "event: response.output_text.delta")
		require.Contains(t, stream, "我来执行命令")
		events := parseCodexGatewayOrderedEvents(t, stream)
		messageAdded := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "message")
		require.Equal(t, "message", gjson.GetBytes(messageAdded, "item.type").String())
		require.Equal(t, "final_answer", gjson.GetBytes(messageAdded, "item.phase").String())
		terminal := events[len(events)-1].Payload
		messageOutput := codexGatewayTerminalOutputByType(t, terminal, "message")
		require.Equal(t, "final_answer", gjson.GetBytes(messageOutput, "phase").String())
		require.Equal(t, CodexGatewayOutputItemTypeFunctionCall, gjson.GetBytes(codexGatewayTerminalOutputByType(t, terminal, CodexGatewayOutputItemTypeFunctionCall), "type").String())
		requireCodexGatewayTextEventsMatchAddedMessage(t, events)
	})

	t.Run("does not expose hosted web search as client function call", func(t *testing.T) {
		origSearch := codexGatewayExecuteHostedWebSearchFunc
		t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
		codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
			return `{"results":[]}`, nil
		}

		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search current news"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"web_search"},
				{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if strings.Contains(string(mustReadCodexGatewayRequestBody(t, r)), `"tool_call_id":"call_search"`) {
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_hosted_search_final","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
				return
			}
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_hosted_search","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"current news\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_hosted_search", IsolationKey: "iso_hosted_search"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)

		stream := buf.String()
		require.NotContains(t, stream, `"name":"web_search"`)
		require.NotContains(t, stream, "event: response.function_call_arguments.done")
		require.Len(t, result.ProviderResult.ToolCalls, 0)
	})

	t.Run("executes hosted web search and resumes model without client tool call", func(t *testing.T) {
		origSearch := codexGatewayExecuteHostedWebSearchFunc
		t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
		var searchedQueries []string
		var buf bytes.Buffer
		codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
			searchedQueries = append(searchedQueries, query)
			streamSoFar := buf.String()
			require.Contains(t, streamSoFar, `"type":"web_search_call"`)
			require.Contains(t, streamSoFar, `"status":"in_progress"`)
			return `{"results":[{"title":"Result","url":"https://example.test","snippet":"Found from gateway search"}]}`, nil
		}

		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search current news"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"web_search"},
				{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}
			]`),
		}

		var requestBodies [][]byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			requestBodies = append(requestBodies, body)
			w.Header().Set("Content-Type", "text/event-stream")
			switch len(requestBodies) {
			case 1:
				require.Equal(t, "web_search", gjson.GetBytes(body, `tools.#(function.name=="web_search").function.name`).String())
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_hosted_search","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"current news\"}"}}]},"finish_reason":"tool_calls"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
			case 2:
				require.Equal(t, "assistant", gjson.GetBytes(body, "messages.1.role").String())
				require.Equal(t, "tool", gjson.GetBytes(body, "messages.2.role").String())
				require.Equal(t, "call_search", gjson.GetBytes(body, "messages.2.tool_call_id").String())
				require.Contains(t, string(body), "Found from gateway search")
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_hosted_search_final","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"Search result says Found from gateway search."},"finish_reason":"stop"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
			default:
				t.Fatalf("unexpected extra upstream request %d", len(requestBodies))
			}
		}))
		defer server.Close()

		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_hosted_search_resume", IsolationKey: "iso_hosted_search_resume"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, []string{"current news"}, searchedQueries)
		require.Len(t, requestBodies, 2)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		stream := buf.String()
		require.NotContains(t, stream, `"name":"web_search"`)
		require.NotContains(t, stream, "event: response.function_call_arguments.done")
		require.Contains(t, stream, `"type":"web_search_call"`)
		require.Contains(t, stream, `"status":"completed"`)
		require.Contains(t, stream, "event: response.web_search_call.in_progress")
		require.Contains(t, stream, "event: response.web_search_call.searching")
		require.Contains(t, stream, "event: response.web_search_call.completed")
		events := parseCodexGatewayOrderedEvents(t, stream)
		requireSequentialCodexGatewayOrderedSequenceNumbers(t, events)
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.created"))
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.in_progress"))
		require.Equal(t, 3, countCodexGatewayEvent(events, "response.output_item.added"))
		rootResponseID := gjson.GetBytes(events[0].Payload, "response.id").String()
		require.NotEmpty(t, rootResponseID)
		for _, event := range events {
			if responseID := gjson.GetBytes(event.Payload, "response_id").String(); responseID != "" {
				require.Equal(t, rootResponseID, responseID, "event %s should stay on the synthetic response", event.Event)
			}
		}
		terminal := events[len(events)-1].Payload
		require.Equal(t, rootResponseID, gjson.GetBytes(terminal, "response.id").String())
		require.Equal(t, "web_search_call", gjson.GetBytes(codexGatewayTerminalOutputByType(t, terminal, "web_search_call"), "type").String())
		require.Equal(t, "message", gjson.GetBytes(codexGatewayTerminalOutputByType(t, terminal, "message"), "type").String())
		require.Contains(t, stream, "Search result says Found from gateway search.")
	})

	t.Run("reuses hosted web search result for repeated query instead of exceeding loop limit", func(t *testing.T) {
		origSearch := codexGatewayExecuteHostedWebSearchFunc
		t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
		var searchedQueries []string
		codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
			searchedQueries = append(searchedQueries, query)
			return `{"results":[{"title":"Result","url":"https://example.test","snippet":"cached search result"}]}`, nil
		}

		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search current news"}]}
			]`),
			Tools: json.RawMessage(`[{"type":"web_search"}]`),
		}

		var requestBodies [][]byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			requestBodies = append(requestBodies, body)
			w.Header().Set("Content-Type", "text/event-stream")
			switch len(requestBodies) {
			case 1, 2:
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_repeat_search","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_search_` + string(rune('0'+len(requestBodies))) + `","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"current news\"}"}}]},"finish_reason":"tool_calls"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
			case 3:
				require.Empty(t, gjson.GetBytes(body, `tools.#(function.name=="web_search").function.name`).String())
				require.Contains(t, string(body), "already been executed")
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_repeat_search_final","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"Final answer from cached search."},"finish_reason":"stop"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
			default:
				t.Fatalf("unexpected extra upstream request %d", len(requestBodies))
			}
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_repeat_search", IsolationKey: "iso_repeat_search"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Equal(t, []string{"current news"}, searchedQueries)
		require.Len(t, requestBodies, 3)
		require.Contains(t, buf.String(), `"type":"web_search_call"`)
		require.Contains(t, buf.String(), "Final answer from cached search.")
	})

	t.Run("continues hosted web search through multiple turns without hard stop", func(t *testing.T) {
		origSearch := codexGatewayExecuteHostedWebSearchFunc
		t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
		calls := 0
		codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
			calls++
			return `{"results":[{"title":"Result","url":"https://example.test","snippet":"` + query + `"}]}`, nil
		}

		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search repeatedly"}]}
			]`),
			Tools: json.RawMessage(`[{"type":"web_search"}]`),
		}

		const searchTurns = 13
		requestBodies := make([][]byte, 0, searchTurns+1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			requestBodies = append(requestBodies, body)
			w.Header().Set("Content-Type", "text/event-stream")
			if len(requestBodies) <= searchTurns {
				query := fmt.Sprintf("turn-%d", len(requestBodies))
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_multi_search_` + fmt.Sprint(len(requestBodies)) + `","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_` + fmt.Sprint(len(requestBodies)) + `","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"` + query + `\"}"}}]},"finish_reason":"tool_calls"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
				return
			}
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_multi_search_final","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_multi_search", IsolationKey: "iso_multi_search"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, searchTurns, calls)
		require.Len(t, requestBodies, searchTurns+1)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Contains(t, buf.String(), `"type":"web_search_call"`)
		require.Contains(t, buf.String(), "done")
	})

	t.Run("compacts long hosted web search replay into checkpoint summary after threshold", func(t *testing.T) {
		origSearch := codexGatewayExecuteHostedWebSearchFunc
		t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
		searchCalls := 0
		codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
			searchCalls++
			return fmt.Sprintf(`{"query":%q,"summary":%q,"raw_blob":%q}`, query, "summary for "+query, "RAW-BLOB<"+query+">"), nil
		}

		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search repeatedly with checkpointing"}]}
			]`),
			Tools: json.RawMessage(`[{"type":"web_search"}]`),
		}

		const searchTurns = 21
		requestBodies := make([][]byte, 0, searchTurns+1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			requestBodies = append(requestBodies, body)
			w.Header().Set("Content-Type", "text/event-stream")
			if len(requestBodies) <= searchTurns {
				query := fmt.Sprintf("turn-%d", len(requestBodies))
				if len(requestBodies) == searchTurns {
					require.Contains(t, string(body), "Hosted web search checkpoint summary")
					require.NotContains(t, string(body), "RAW-BLOB\\u003cturn-1\\u003e")
					require.NotContains(t, string(body), "RAW-BLOB\\u003cturn-2\\u003e")
					require.Contains(t, string(body), "RAW-BLOB\\u003cturn-20\\u003e")
				}
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_checkpoint_` + fmt.Sprint(len(requestBodies)) + `","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_` + fmt.Sprint(len(requestBodies)) + `","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"` + query + `\"}"}}]},"finish_reason":"tool_calls"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
				return
			}
			require.Contains(t, string(body), "Hosted web search checkpoint summary")
			require.NotContains(t, string(body), "RAW-BLOB\\u003cturn-1\\u003e")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_checkpoint_final","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"done with checkpoints"},"finish_reason":"stop"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_checkpoint_compact", IsolationKey: "iso_checkpoint_compact"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, searchTurns, searchCalls)
		require.Len(t, requestBodies, searchTurns+1)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Contains(t, buf.String(), `"type":"web_search_call"`)
		require.Contains(t, buf.String(), "done with checkpoints")
	})

	t.Run("reuses repeated hosted web search queries even when replay compaction is active", func(t *testing.T) {
		origSearch := codexGatewayExecuteHostedWebSearchFunc
		t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
		searchedQueries := make([]string, 0, 20)
		codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
			searchedQueries = append(searchedQueries, query)
			return fmt.Sprintf(`{"query":%q,"summary":%q,"raw_blob":%q}`, query, "summary for "+query, "RAW-BLOB<"+query+">"), nil
		}

		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search repeatedly with one duplicate"}]}
			]`),
			Tools: json.RawMessage(`[{"type":"web_search"}]`),
		}

		queries := make([]string, 0, 22)
		for i := 1; i <= 20; i++ {
			queries = append(queries, fmt.Sprintf("turn-%d", i))
		}
		queries = append(queries, "turn-5")

		requestBodies := make([][]byte, 0, len(queries)+1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			requestBodies = append(requestBodies, body)
			w.Header().Set("Content-Type", "text/event-stream")
			if len(requestBodies) <= len(queries) {
				query := queries[len(requestBodies)-1]
				if len(requestBodies) == len(queries) {
					require.Contains(t, string(body), "Hosted web search checkpoint summary")
				}
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_compact_repeat_` + fmt.Sprint(len(requestBodies)) + `","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_` + fmt.Sprint(len(requestBodies)) + `","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"` + query + `\"}"}}]},"finish_reason":"tool_calls"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
				return
			}
			require.Contains(t, string(body), "Hosted web search checkpoint summary")
			require.Contains(t, string(body), "already been executed")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_compact_repeat_final","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"final repeated answer"},"finish_reason":"stop"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_checkpoint_repeat", IsolationKey: "iso_checkpoint_repeat"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Len(t, requestBodies, len(queries)+1)
		require.Equal(t, queries[:20], searchedQueries)
		require.Contains(t, buf.String(), `"type":"web_search_call"`)
		require.Contains(t, buf.String(), "final repeated answer")
	})

	t.Run("does not fail when hosted web search appears after visible output already flushed", func(t *testing.T) {
		origSearch := codexGatewayExecuteHostedWebSearchFunc
		t.Cleanup(func() { codexGatewayExecuteHostedWebSearchFunc = origSearch })
		searchCalls := 0
		codexGatewayExecuteHostedWebSearchFunc = func(_ context.Context, query string) (string, error) {
			searchCalls++
			return `{"results":[{"title":"Late Result","url":"https://example.test","snippet":"late hosted search"}]}`, nil
		}

		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search after text"}]}
			]`),
			Tools: json.RawMessage(`[{"type":"web_search"}]`),
		}

		requestBodies := make([][]byte, 0, 2)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			requestBodies = append(requestBodies, body)
			w.Header().Set("Content-Type", "text/event-stream")
			switch len(requestBodies) {
			case 1:
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_late_hosted_search","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"I will search this."},"finish_reason":null}]}`,
					"",
					`data: {"id":"chatcmpl_stream_late_hosted_search","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"late search\"}"}}]},"finish_reason":"tool_calls"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
			case 2:
				require.Contains(t, string(body), "late hosted search")
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_late_hosted_search_final","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"Search finished."},"finish_reason":"stop"}]}`,
					"",
					`data: [DONE]`,
					"",
				}, "\n"))
			default:
				t.Fatalf("unexpected extra upstream request %d", len(requestBodies))
			}
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_late_hosted_search", IsolationKey: "iso_late_hosted_search"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, 1, searchCalls)
		require.Len(t, requestBodies, 2)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		stream := buf.String()
		require.Contains(t, stream, "I will search this.")
		require.Contains(t, stream, `"type":"web_search_call"`)
		require.Contains(t, stream, `"status":"completed"`)
		require.Contains(t, stream, "Search finished.")
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

		stateStore := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{})
		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
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

		addedPayload := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "custom_tool_call")
		require.Equal(t, "apply_patch", gjson.GetBytes(addedPayload, "item.name").String())
		customDeltaPayload := firstCodexGatewayEventPayload(t, events, "response.custom_tool_call_input.delta")
		require.Equal(t, "fc_call_patch", gjson.GetBytes(customDeltaPayload, "item_id").String())
		require.Equal(t, "*** Begin Patch\n*** End Patch", gjson.GetBytes(customDeltaPayload, "delta").String())
		customDonePayload := firstCodexGatewayEventPayload(t, events, "response.custom_tool_call_input.done")
		require.Equal(t, "fc_call_patch", gjson.GetBytes(customDonePayload, "item_id").String())
		require.Equal(t, "*** Begin Patch\n*** End Patch", gjson.GetBytes(customDonePayload, "input").String())

		terminal := events[len(events)-1].Payload
		require.NotContains(t, buf.String(), "正在使用工具继续推进")
		customOutput := codexGatewayTerminalOutputByType(t, terminal, "custom_tool_call")
		require.Equal(t, "apply_patch", gjson.GetBytes(customOutput, "name").String())
		require.Equal(t, "*** Begin Patch\n*** End Patch", gjson.GetBytes(customOutput, "input").String())
	})

	t.Run("does not synthesize assistant text before tool-only turns", func(t *testing.T) {
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
		firstAdded := firstCodexGatewayOutputItemAddedPayloadByType(t, events, "function_call")
		require.Equal(t, "function_call", gjson.GetBytes(firstAdded, "item.type").String())
		require.Equal(t, "in_progress", gjson.GetBytes(firstAdded, "item.status").String())
		require.NotContains(t, buf.String(), "event: response.output_text.delta")
		require.NotContains(t, buf.String(), "event: response.content_part.added")
		require.NotContains(t, buf.String(), "正在使用工具继续推进")

		functionDoneIdx := indexCodexGatewayEvent(events, "response.function_call_arguments.done")
		require.Equal(t, -1, indexCodexGatewayEvent(events, "response.output_text.done"))
		require.GreaterOrEqual(t, functionDoneIdx, 0)

		terminal := events[len(events)-1].Payload
		toolOutput := codexGatewayTerminalOutputByType(t, terminal, "function_call")
		require.Equal(t, `{"city":"SF"}`, gjson.GetBytes(toolOutput, "arguments").String())
	})

	t.Run("normalizes wait_agent arguments before exposing function call events", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"wait for subagent"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"function","name":"wait_agent","parameters":{"type":"object","properties":{"targets":{"type":"array","items":{"type":"string"}},"timeout_ms":{"type":"number"}},"required":["targets"]}}
			]`),
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_wait_agent","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_wait","type":"function","function":{"name":"wait_agent","arguments":"{\"targets\":\"agent-1\",\"timeout_ms\":\"30000\"}"}}]},"finish_reason":"tool_calls"}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"))
		}))
		defer server.Close()

		stateStore := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{})
		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			server.Client(),
			server.URL,
			"test-key",
			model,
			req,
			stateStore,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_wait_agent", IsolationKey: "iso_wait_agent"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, 1, countCodexGatewayEvent(events, "response.function_call_arguments.delta"))
		funcDeltaPayload := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.delta")
		require.Equal(t, `{"targets":["agent-1"],"timeout_ms":30000}`, gjson.GetBytes(funcDeltaPayload, "delta").String())
		funcDonePayload := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.done")
		require.Equal(t, `{"targets":["agent-1"],"timeout_ms":30000}`, gjson.GetBytes(funcDonePayload, "arguments").String())

		terminal := events[len(events)-1].Payload
		require.Equal(t, `{"targets":["agent-1"],"timeout_ms":30000}`, gjson.GetBytes(codexGatewayTerminalOutputByType(t, terminal, "function_call"), "arguments").String())
		require.Equal(t, `{"targets":["agent-1"],"timeout_ms":30000}`, result.ProviderResult.ToolCalls[0].Arguments)
		stored, err := stateStore.Get(CodexGatewayStateLookupKey{
			ResponseID:    "chatcmpl_stream_wait_agent",
			SessionKey:    "session_wait_agent",
			IsolationKey:  "iso_wait_agent",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		})
		require.NoError(t, err)
		require.Equal(t, `{"targets":["agent-1"],"timeout_ms":30000}`, stored.ToolCalls[0].Arguments)
	})

	t.Run("repairs safe wait_agent JSON but blocks malformed mutating tools", func(t *testing.T) {
		t.Run("wait_agent", func(t *testing.T) {
			model := CodexGatewayModel{
				Slug:          "deepseek-v4-pro",
				Provider:      "deepseek",
				UpstreamModel: "deepseek-v4-pro",
			}
			req := CodexGatewayResponsesCreateRequest{
				Model: "deepseek-v4-pro",
				Input: json.RawMessage(`[
					{"type":"message","role":"user","content":[{"type":"input_text","text":"wait for subagent"}]}
				]`),
				Tools: json.RawMessage(`[
					{"type":"function","name":"wait_agent","parameters":{"type":"object","properties":{"targets":{"type":"array","items":{"type":"string"}},"timeout_ms":{"type":"number"}},"required":["targets"]}}
				]`),
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_wait_agent_repair","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_wait_repair","type":"function","function":{"name":"wait_agent","arguments":"{\"targets\":\"agent-1\",\"timeout_ms\":\"30000\""}}]},"finish_reason":"tool_calls"}]}`,
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
				CodexGatewayDeepSeekRequestContext{SessionKey: "session_wait_agent_repair", IsolationKey: "iso_wait_agent_repair"},
				CodexGatewayDeepSeekRequestConfig{},
				&buf,
			)
			require.NoError(t, err)
			require.Equal(t, "completed", result.ProviderResult.Response.Status)
			events := parseCodexGatewayOrderedEvents(t, buf.String())
			donePayload := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.done")
			require.Equal(t, `{"targets":["agent-1"],"timeout_ms":30000}`, gjson.GetBytes(donePayload, "arguments").String())
		})

		t.Run("shell_exec", func(t *testing.T) {
			model := CodexGatewayModel{
				Slug:          "deepseek-v4-pro",
				Provider:      "deepseek",
				UpstreamModel: "deepseek-v4-pro",
			}
			req := CodexGatewayResponsesCreateRequest{
				Model: "deepseek-v4-pro",
				Input: json.RawMessage(`[
					{"type":"message","role":"user","content":[{"type":"input_text","text":"run pwd"}]}
				]`),
				Tools: json.RawMessage(`[
					{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}
				]`),
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_shell_block","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_shell_block","type":"function","function":{"name":"shell__exec","arguments":"{\"cmd\":\"pwd\""}}]},"finish_reason":"tool_calls"}]}`,
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
				CodexGatewayDeepSeekRequestContext{SessionKey: "session_shell_block", IsolationKey: "iso_shell_block"},
				CodexGatewayDeepSeekRequestConfig{},
				&buf,
			)
			require.NoError(t, err)
			require.Equal(t, "incomplete", result.ProviderResult.Response.Status)
			require.Empty(t, result.ProviderResult.ToolCalls)
			require.NotContains(t, buf.String(), "event: response.output_item.added")
			require.NotContains(t, buf.String(), "event: response.function_call_arguments.done")
			events := parseCodexGatewayOrderedEvents(t, buf.String())
			require.Equal(t, int64(0), gjson.GetBytes(events[len(events)-1].Payload, "response.output.#").Int())
		})

		t.Run("apply_patch", func(t *testing.T) {
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
					{"type":"custom","name":"apply_patch","description":"edit files","format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}}
				]`),
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, strings.Join([]string{
					`data: {"id":"chatcmpl_stream_patch_block","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_patch_block","type":"function","function":{"name":"custom__apply_patch","arguments":"{\"input\":\"*** Begin Patch\""}}]},"finish_reason":"tool_calls"}]}`,
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
				CodexGatewayDeepSeekRequestContext{SessionKey: "session_patch_block", IsolationKey: "iso_patch_block"},
				CodexGatewayDeepSeekRequestConfig{},
				&buf,
			)
			require.NoError(t, err)
			require.Equal(t, "completed", result.ProviderResult.Response.Status)
			require.Len(t, result.ProviderResult.ToolCalls, 1)
			require.Contains(t, buf.String(), "event: response.custom_tool_call_input.done")
			events := parseCodexGatewayOrderedEvents(t, buf.String())
			require.Equal(t, int64(2), gjson.GetBytes(events[len(events)-1].Payload, "response.output.#").Int())
		})
	})

	t.Run("unflattens deepseek flattened tool arguments before exposing them", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"search the repo"}]}
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
						"required":["query","workspace"]
					}
				}
			]`),
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_unflatten","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_search_repo","type":"function","function":{"name":"search_repo","arguments":"{\"query\":\"gateway\",\"workspace.root\":\"/tmp/project\",\"workspace.filters.include\":\"*.go\",\"workspace.filters.exclude\":\"vendor\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_unflatten", IsolationKey: "iso_unflatten"},
			CodexGatewayDeepSeekRequestConfig{
				ToolMappingConfig: CodexGatewayToolMappingConfig{
					EnableDeepSeekSchemaFlattening: true,
					DeepSeekFlattenMinDepth:        3,
					DeepSeekFlattenMinLeaves:       4,
				},
			},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)

		want := `{"query":"gateway","workspace":{"root":"/tmp/project","filters":{"include":"*.go","exclude":"vendor"}}}`
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		donePayload := firstCodexGatewayEventPayload(t, events, "response.function_call_arguments.done")
		require.JSONEq(t, want, gjson.GetBytes(donePayload, "arguments").String())
		terminal := events[len(events)-1].Payload
		require.JSONEq(t, want, gjson.GetBytes(codexGatewayTerminalOutputByType(t, terminal, "function_call"), "arguments").String())
		require.JSONEq(t, want, result.ProviderResult.ToolCalls[0].Arguments)
	})

	t.Run("allows repeated mutating tool calls with identical arguments in one turn", func(t *testing.T) {
		model := CodexGatewayModel{
			Slug:          "deepseek-v4-pro",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		}
		req := CodexGatewayResponsesCreateRequest{
			Model: "deepseek-v4-pro",
			Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"run pwd once"}]}
			]`),
			Tools: json.RawMessage(`[
				{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}
			]`),
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_repeat_shell","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_shell_a","type":"function","function":{"name":"shell__exec","arguments":"{\"cmd\":\"pwd\"}"}},{"index":1,"id":"call_shell_b","type":"function","function":{"name":"shell__exec","arguments":"{\"cmd\":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_repeat_shell", IsolationKey: "iso_repeat_shell"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Len(t, result.ProviderResult.ToolCalls, 2)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, 3, countCodexGatewayEvent(events, "response.output_item.added"))
		require.Equal(t, 3, countCodexGatewayEvent(events, "response.output_item.done"))
		terminal := events[len(events)-1].Payload
		require.Equal(t, "response.completed", events[len(events)-1].Event)
		require.Equal(t, int64(3), gjson.GetBytes(terminal, "response.output.#").Int())
	})

	t.Run("does not turn reasoning_content into executable tool calls", func(t *testing.T) {
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
			Tools: json.RawMessage(`[
				{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}
			]`),
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`data: {"id":"chatcmpl_stream_reasoning_only","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"reasoning_content":"{\"tool_calls\":[{\"name\":\"shell__exec\",\"arguments\":{\"cmd\":\"pwd\"}}]}"},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_stream_reasoning_only","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_reasoning_only", IsolationKey: "iso_reasoning_only"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "completed", result.ProviderResult.Response.Status)
		require.Empty(t, result.ProviderResult.ToolCalls)
		require.NotContains(t, buf.String(), "response.function_call_arguments")
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		terminal := events[len(events)-1].Payload
		require.Equal(t, "reasoning", gjson.GetBytes(terminal, "response.output.0.type").String())
		require.Equal(t, "done", gjson.GetBytes(terminal, "response.output.1.content.0.text").String())
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
		require.Equal(t, `{"city":"SF"}`, gjson.GetBytes(codexGatewayTerminalOutputByType(t, events[len(events)-1].Payload, "function_call"), "arguments").String())
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
		require.Len(t, result.ProviderResult.Response.Output, 2)
		require.Equal(t, "reasoning", gjson.GetBytes(result.ProviderResult.Response.Output[0], "type").String())
		require.Equal(t, "function_call", gjson.GetBytes(result.ProviderResult.Response.Output[1], "type").String())
		require.Len(t, result.ProviderResult.ToolCalls, 1)
		require.Equal(t, "get_weather", result.ProviderResult.ToolCalls[0].Name)

		events := parseCodexGatewayOrderedEvents(t, buf.String())
		functionDoneIdx := indexCodexGatewayEvent(events, "response.function_call_arguments.done")
		require.GreaterOrEqual(t, functionDoneIdx, 0)
		require.Equal(t, -1, indexCodexGatewayEvent(events, "response.reasoning_text.done"))
		require.GreaterOrEqual(t, indexCodexGatewayOutputItemDoneByType(events, "reasoning"), 0)

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
		require.Contains(t, buf.String(), "event: response.output_text.delta")
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, "response.incomplete", events[len(events)-1].Event)
		require.Equal(t, "partial", gjson.GetBytes(events[len(events)-1].Payload, "response.output.0.content.0.text").String())
	})

	t.Run("returns failover error on connect refusal before output", func(t *testing.T) {
		req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
			"model":"deepseek-v4-pro",
			"input":[{"type":"message","role":"user","content":"ping"}],
			"stream":true
		}`))
		require.NoError(t, err)

		var dst bytes.Buffer
		_, err = ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, &net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: syscall.ECONNREFUSED,
				}
			})},
			"http://127.0.0.1:8991",
			"test-key",
			CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"},
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{},
			CodexGatewayDeepSeekRequestConfig{},
			&dst,
		)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
		require.Contains(t, string(failoverErr.ResponseBody), "connection refused")
		require.Empty(t, dst.String())
	})

	t.Run("returns failover error on retryable upstream rejection before output", func(t *testing.T) {
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
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"try again later"}}`)
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
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_429", IsolationKey: "iso_429"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
		require.Contains(t, string(failoverErr.ResponseBody), "rate_limit_exceeded")
		require.Empty(t, buf.String())
	})

	t.Run("empty reasoning delta still returns failover on upstream close", func(t *testing.T) {
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
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
		require.Contains(t, string(failoverErr.ResponseBody), "missing terminal event")
		require.Empty(t, result.ProviderResult.Response.Status)
		require.Empty(t, buf.String())
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
		require.Equal(t, "reasoning", gjson.GetBytes(events[len(events)-1].Payload, "response.output.0.type").String())
		require.NotContains(t, buf.String(), "正在使用工具继续推进")

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

	t.Run("emits incomplete terminal event on post-output transport error", func(t *testing.T) {
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

		body := strings.Join([]string{
			`data: {"id":"chatcmpl_stream_transport_cut","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
			"",
			"",
		}, "\n")
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: &errAfterDataReadCloser{
					data: []byte(body),
					err:  io.ErrUnexpectedEOF,
				},
			}, nil
		})}

		var buf bytes.Buffer
		result, err := ExecuteCodexGatewayDeepSeekStream(
			context.Background(),
			client,
			"http://deepseek.example",
			"test-key",
			model,
			req,
			nil,
			CodexGatewayDeepSeekRequestContext{SessionKey: "session_transport_cut", IsolationKey: "iso_transport_cut"},
			CodexGatewayDeepSeekRequestConfig{},
			&buf,
		)
		require.NoError(t, err)
		require.Equal(t, "incomplete", result.ProviderResult.Response.Status)
		events := parseCodexGatewayOrderedEvents(t, buf.String())
		require.Equal(t, "response.incomplete", events[len(events)-1].Event)
		require.NotContains(t, buf.String(), "response.failed")
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

type observableBuffer struct {
	bytes.Buffer
	onWrite func(string)
}

func (b *observableBuffer) Write(p []byte) (int, error) {
	n, err := b.Buffer.Write(p)
	if err == nil && b.onWrite != nil {
		b.onWrite(b.Buffer.String())
	}
	return n, err
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

func nthCodexGatewayEventPayload(t *testing.T, events []codexGatewayOrderedEvent, name string, n int) []byte {
	t.Helper()
	seen := 0
	for _, event := range events {
		if event.Event != name {
			continue
		}
		if seen == n {
			return event.Payload
		}
		seen++
	}
	t.Fatalf("event %s[%d] not found", name, n)
	return nil
}

func firstCodexGatewayOutputItemAddedPayloadByType(t *testing.T, events []codexGatewayOrderedEvent, itemType string) []byte {
	t.Helper()
	for _, event := range events {
		if event.Event != "response.output_item.added" {
			continue
		}
		if gjson.GetBytes(event.Payload, "item.type").String() == itemType {
			return event.Payload
		}
	}
	t.Fatalf("response.output_item.added with item.type %s not found", itemType)
	return nil
}

func firstCodexGatewayOutputItemDonePayloadByType(t *testing.T, events []codexGatewayOrderedEvent, itemType string) []byte {
	t.Helper()
	for _, event := range events {
		if event.Event != "response.output_item.done" {
			continue
		}
		if gjson.GetBytes(event.Payload, "item.type").String() == itemType {
			return event.Payload
		}
	}
	t.Fatalf("response.output_item.done with item.type %s not found", itemType)
	return nil
}

func codexGatewayTerminalOutputByType(t *testing.T, terminal []byte, itemType string) []byte {
	t.Helper()
	outputs := gjson.GetBytes(terminal, "response.output").Array()
	for _, output := range outputs {
		if output.Get("type").String() == itemType {
			return []byte(output.Raw)
		}
	}
	t.Fatalf("terminal response output with type %s not found", itemType)
	return nil
}

func requireSequentialCodexGatewayOrderedSequenceNumbers(t *testing.T, events []codexGatewayOrderedEvent) {
	t.Helper()
	require.NotEmpty(t, events)
	for index, event := range events {
		require.Equal(t, int64(index), gjson.GetBytes(event.Payload, "sequence_number").Int())
	}
}

func requireCodexGatewayTextEventsMatchAddedMessage(t *testing.T, events []codexGatewayOrderedEvent) {
	t.Helper()
	addedByIndex := make(map[int64]struct {
		itemID string
		phase  string
	})
	for _, event := range events {
		if event.Event != "response.output_item.added" {
			continue
		}
		outputIndex := gjson.GetBytes(event.Payload, "output_index").Int()
		itemType := gjson.GetBytes(event.Payload, "item.type").String()
		if existing, ok := addedByIndex[outputIndex]; ok {
			require.Equal(t, existing.itemID, gjson.GetBytes(event.Payload, "item.id").String(), "output_index %d must not be reused for a different item", outputIndex)
			require.Equal(t, "message", itemType)
			continue
		}
		if itemType == "message" {
			addedByIndex[outputIndex] = struct {
				itemID string
				phase  string
			}{
				itemID: gjson.GetBytes(event.Payload, "item.id").String(),
				phase:  gjson.GetBytes(event.Payload, "item.phase").String(),
			}
		} else {
			addedByIndex[outputIndex] = struct {
				itemID string
				phase  string
			}{itemID: gjson.GetBytes(event.Payload, "item.id").String()}
		}
	}
	for _, event := range events {
		switch event.Event {
		case "response.output_text.delta", "response.output_text.done", "response.content_part.done":
			outputIndex := gjson.GetBytes(event.Payload, "output_index").Int()
			added, ok := addedByIndex[outputIndex]
			require.True(t, ok, "text event %s references output_index %d without added message", event.Event, outputIndex)
			require.Equal(t, added.itemID, gjson.GetBytes(event.Payload, "item_id").String())
		case "response.output_item.done":
			if gjson.GetBytes(event.Payload, "item.type").String() != "message" {
				continue
			}
			outputIndex := gjson.GetBytes(event.Payload, "output_index").Int()
			added, ok := addedByIndex[outputIndex]
			require.True(t, ok, "message done references output_index %d without added message", outputIndex)
			require.Equal(t, added.itemID, gjson.GetBytes(event.Payload, "item.id").String())
			require.Equal(t, added.phase, gjson.GetBytes(event.Payload, "item.phase").String())
		}
	}
}

func indexCodexGatewayEvent(events []codexGatewayOrderedEvent, name string) int {
	for i, event := range events {
		if event.Event == name {
			return i
		}
	}
	return -1
}

func indexCodexGatewayOutputItemDoneByType(events []codexGatewayOrderedEvent, itemType string) int {
	for i, event := range events {
		if event.Event != "response.output_item.done" {
			continue
		}
		if gjson.GetBytes(event.Payload, "item.type").String() == itemType {
			return i
		}
	}
	return -1
}

func indexCodexGatewayOutputItemAddedByType(events []codexGatewayOrderedEvent, itemType string) int {
	for i, event := range events {
		if event.Event != "response.output_item.added" {
			continue
		}
		if gjson.GetBytes(event.Payload, "item.type").String() == itemType {
			return i
		}
	}
	return -1
}

func mustReadCodexGatewayRequestBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body
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

type errAfterDataReadCloser struct {
	data []byte
	err  error
	read bool
}

func (r *errAfterDataReadCloser) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, r.err
}

func (r *errAfterDataReadCloser) Close() error {
	return nil
}
