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

	"github.com/stretchr/testify/require"
)

func TestExecuteCodexGatewayAnthropicStream_MapsTextToolUseAndUsage(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("x-api-key"))
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-request-id", "req-anthropic")
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"usage":{"input_tokens":11,"cache_read_input_tokens":7,"cache_creation":{"ephemeral_1h_input_tokens":5}}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"准备调用工具。"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"shell__exec","input":{}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"pwd\"}"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":1}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-sonnet-4-6",
		"input":[{"type":"message","role":"user","content":"run pwd"}],
		"tools":[{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}],
		"stream":true
	}`))
	require.NoError(t, err)

	var dst bytes.Buffer
	result, err := ExecuteCodexGatewayAnthropicStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		CodexGatewayModel{Slug: "claude-sonnet-4-6", Provider: "anthropic", UpstreamModel: "claude-sonnet-4-6"},
		req,
		NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{}),
		CodexGatewayAnthropicRequestContext{SessionKey: "s", IsolationKey: "i"},
		CodexGatewayAnthropicRequestConfig{},
		&dst,
	)
	require.NoError(t, err)
	require.Contains(t, string(gotBody), `"stream":true`)
	require.Equal(t, "msg_1", result.ProviderResult.ResponseID)
	require.Equal(t, "req-anthropic", result.ProviderResult.UpstreamRequestID)
	require.Equal(t, 18, result.ProviderResult.Usage.InputTokens)
	require.Equal(t, 9, result.ProviderResult.Usage.OutputTokens)
	require.Equal(t, 5, result.ProviderResult.Usage.CacheCreationInputTokens)
	require.Equal(t, 7, result.ProviderResult.Usage.CacheReadInputTokens)
	require.Equal(t, 0, result.ProviderResult.Usage.CacheCreation5mTokens)
	require.Equal(t, 5, result.ProviderResult.Usage.CacheCreation1hTokens)
	require.Len(t, result.ProviderResult.ToolCalls, 1)
	require.Equal(t, "toolu_1", result.ProviderResult.ToolCalls[0].ID)
	require.Equal(t, "exec", result.ProviderResult.ToolCalls[0].Name)

	events := dst.String()
	require.Contains(t, events, "event: response.created")
	require.Contains(t, events, "event: response.output_text.delta")
	require.Contains(t, events, "event: response.function_call_arguments.delta")
	require.Contains(t, events, "event: response.function_call_arguments.done")
	require.Contains(t, events, "event: response.completed")
	require.NotContains(t, events, "response.reasoning_text.delta")

	var completed map[string]any
	for _, block := range strings.Split(events, "\n\n") {
		if !strings.Contains(block, "event: response.completed") {
			continue
		}
		line := ""
		for _, candidate := range strings.Split(block, "\n") {
			if strings.HasPrefix(candidate, "data: ") {
				line = strings.TrimPrefix(candidate, "data: ")
			}
		}
		require.NotEmpty(t, line)
		require.NoError(t, json.Unmarshal([]byte(line), &completed))
	}
	require.Equal(t, "response.completed", completed["type"])
	response, ok := completed["response"].(map[string]any)
	require.True(t, ok)
	usage, ok := response["usage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(23), usage["input_tokens"])
	require.Equal(t, float64(32), usage["total_tokens"])
}

func TestExecuteCodexGatewayAnthropicStream_NormalizesWaitAgentArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_wait","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"usage":{"input_tokens":11}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_wait","name":"wait_agent","input":{}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"targets\":\"agent-1\",\"timeout_ms\":\"30000\"}"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-sonnet-4-6",
		"input":[{"type":"message","role":"user","content":"wait"}],
		"tools":[{"type":"function","name":"wait_agent","parameters":{"type":"object","properties":{"targets":{"type":"array","items":{"type":"string"}},"timeout_ms":{"type":"number"}},"required":["targets"]}}],
		"stream":true
	}`))
	require.NoError(t, err)

	var dst bytes.Buffer
	result, err := ExecuteCodexGatewayAnthropicStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		CodexGatewayModel{Slug: "claude-sonnet-4-6", Provider: "anthropic", UpstreamModel: "claude-sonnet-4-6"},
		req,
		NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{}),
		CodexGatewayAnthropicRequestContext{SessionKey: "s", IsolationKey: "i"},
		CodexGatewayAnthropicRequestConfig{},
		&dst,
	)
	require.NoError(t, err)
	require.Len(t, result.ProviderResult.ToolCalls, 1)
	require.JSONEq(t, `{"targets":["agent-1"],"timeout_ms":30000}`, result.ProviderResult.ToolCalls[0].Arguments)

	events := dst.String()
	require.Contains(t, events, `"name":"wait_agent"`)
	require.Contains(t, events, `\"targets\":[\"agent-1\"]`)
	require.Contains(t, events, `\"timeout_ms\":30000`)
	require.NotContains(t, events, `\"targets\":\"agent-1\"`)
	require.Contains(t, events, "event: response.function_call_arguments.delta")
	require.Contains(t, events, "event: response.function_call_arguments.done")
}

func TestExecuteCodexGatewayAnthropicStream_PersistsThinkingSignatureForToolReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_think","type":"message","role":"assistant","model":"claude-opus-4-6-thinking","content":[],"usage":{"input_tokens":11}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Need inspect."}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_thinking_1"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"我来查看。"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":1}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"shell__exec","input":{}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"pwd\"}"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6-thinking",
		"input":[{"type":"message","role":"user","content":"run pwd"}],
		"tools":[{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}],
		"reasoning":{"effort":"xhigh"},
		"stream":true
	}`))
	require.NoError(t, err)

	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{})
	var dst bytes.Buffer
	result, err := ExecuteCodexGatewayAnthropicStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
		req,
		store,
		CodexGatewayAnthropicRequestContext{SessionKey: "s", IsolationKey: "i"},
		CodexGatewayAnthropicRequestConfig{},
		&dst,
	)
	require.NoError(t, err)
	require.Equal(t, "Need inspect.", result.ProviderResult.ReasoningContent)

	state, err := store.Get(CodexGatewayStateLookupKey{
		ResponseID:    "msg_think",
		SessionKey:    "s",
		IsolationKey:  "i",
		Provider:      "anthropic",
		UpstreamModel: "claude-opus-4-6-thinking",
	})
	require.NoError(t, err)
	require.NotEmpty(t, state.ReplayMessages)
	rawReplay, err := json.Marshal(state.ReplayMessages)
	require.NoError(t, err)
	require.Contains(t, string(rawReplay), `"type":"thinking"`)
	require.Contains(t, string(rawReplay), `"thinking":"Need inspect."`)
	require.Contains(t, string(rawReplay), `"signature":"sig_thinking_1"`)
	require.Contains(t, string(rawReplay), `"type":"tool_use"`)
}

func TestExecuteCodexGatewayAnthropicStream_RetriesZeroEventToolReplayWithThinkingDisabled(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		if len(bodies) == 1 {
			return
		}
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_retry","type":"message","role":"assistant","model":"claude-opus-4-6-thinking","content":[],"usage":{"input_tokens":11}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n")))
	}))
	defer server.Close()

	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6-thinking",
		"input":[
			{"type":"message","role":"user","content":"inspect"},
			{"type":"function_call","call_id":"call_1","name":"shell.exec","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		],
		"tools":[{"type":"namespace","name":"shell","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}],
		"reasoning":{"effort":"xhigh"},
		"stream":true
	}`))
	require.NoError(t, err)

	var dst bytes.Buffer
	result, err := ExecuteCodexGatewayAnthropicStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
		req,
		NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{}),
		CodexGatewayAnthropicRequestContext{SessionKey: "s", IsolationKey: "i"},
		CodexGatewayAnthropicRequestConfig{},
		&dst,
	)
	require.NoError(t, err)
	require.Equal(t, "msg_retry", result.ProviderResult.ResponseID)
	require.Len(t, bodies, 2)
	require.Contains(t, bodies[0], `"thinking":{"type":"adaptive"`)
	require.Contains(t, bodies[1], `"thinking":{"type":"disabled"`)
	require.Contains(t, dst.String(), "event: response.completed")
}

func TestExecuteCodexGatewayAnthropicStream_Cloudflare524TriggersFailoverBeforeOutput(t *testing.T) {
	const cloudflareHTML = `<!DOCTYPE html><html><head><title>zivv.pro | 524: A timeout occurred</title></head><body><span class="code-label">Error code 524</span></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("CF-Ray", "ray-test")
		w.WriteHeader(524)
		_, _ = w.Write([]byte(cloudflareHTML))
	}))
	defer server.Close()

	req, err := DecodeCodexGatewayResponsesCreateRequest([]byte(`{
		"model":"claude-opus-4-6-thinking",
		"input":[{"type":"message","role":"user","content":"hello"}],
		"reasoning":{"effort":"xhigh"},
		"stream":true
	}`))
	require.NoError(t, err)

	var dst bytes.Buffer
	_, err = ExecuteCodexGatewayAnthropicStream(
		context.Background(),
		server.Client(),
		server.URL,
		"test-key",
		CodexGatewayModel{Slug: "claude-opus-4-6-thinking", Provider: "anthropic", UpstreamModel: "claude-opus-4-6-thinking"},
		req,
		NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{}),
		CodexGatewayAnthropicRequestContext{SessionKey: "s", IsolationKey: "i"},
		CodexGatewayAnthropicRequestConfig{},
		&dst,
	)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, 524, failoverErr.StatusCode)
	require.Equal(t, "ray-test", failoverErr.ResponseHeaders.Get("CF-Ray"))
	require.Contains(t, string(failoverErr.ResponseBody), "Cloudflare 524 timeout")
	require.NotContains(t, string(failoverErr.ResponseBody), "<!DOCTYPE html>")
	require.Empty(t, dst.String())
}
