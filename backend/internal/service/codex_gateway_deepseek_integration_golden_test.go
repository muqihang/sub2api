package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexGatewayGoldenFixture struct {
	Name      string                        `json:"name"`
	Mode      string                        `json:"mode"`
	Request   codexGatewayGoldenRequest     `json:"request"`
	Upstream  *codexGatewayGoldenUpstream   `json:"upstream,omitempty"`
	SeedState *CodexGatewayResponseState    `json:"seed_state,omitempty"`
	Expect    codexGatewayGoldenExpectation `json:"expect"`
}

type codexGatewayGoldenRequest struct {
	Model              string          `json:"model"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Input              json.RawMessage `json:"input,omitempty"`
	Tools              json.RawMessage `json:"tools,omitempty"`
}

func (r codexGatewayGoldenRequest) toCreateRequest() CodexGatewayResponsesCreateRequest {
	req := CodexGatewayResponsesCreateRequest{
		Model: r.Model,
		Input: cloneGoldenRawMessage(r.Input),
		Tools: cloneGoldenRawMessage(r.Tools),
	}
	if strings.TrimSpace(r.PreviousResponseID) != "" {
		prev := strings.TrimSpace(r.PreviousResponseID)
		req.PreviousResponseID = &prev
	}
	return req
}

type codexGatewayGoldenUpstream struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

type codexGatewayGoldenExpectation struct {
	TerminalEvent   string                   `json:"terminal_event,omitempty"`
	ResponseStatus  string                   `json:"response_status,omitempty"`
	Usage           *codexGatewayGoldenUsage `json:"usage,omitempty"`
	ReasoningText   string                   `json:"reasoning_text,omitempty"`
	OutputText      string                   `json:"output_text,omitempty"`
	OutputTypes     []string                 `json:"output_types,omitempty"`
	ToolName        string                   `json:"tool_name,omitempty"`
	ToolArguments   string                   `json:"tool_arguments,omitempty"`
	StoredToolCalls int                      `json:"stored_tool_calls,omitempty"`
	ErrorType       string                   `json:"error_type,omitempty"`
	ErrorCode       string                   `json:"error_code,omitempty"`
	ErrorMessage    string                   `json:"error_message,omitempty"`
	Messages        json.RawMessage          `json:"messages,omitempty"`
	ErrorContains   string                   `json:"error_contains,omitempty"`
}

type codexGatewayGoldenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	CachedTokens int `json:"cached_tokens"`
}

func TestCodexGatewayDeepSeekIntegrationGolden_StreamFixtures(t *testing.T) {
	for _, fixtureName := range []string{
		"simple_text_stream",
		"function_tool_call_stream",
		"namespace_tool_call_stream",
		"custom_tool_call_stream",
		"usage_only_stream",
		"error_invalid_request_stream",
		"error_upstream_failure_stream",
	} {
		fixture := loadCodexGatewayGoldenFixture(t, fixtureName)
		t.Run(fixture.Name, func(t *testing.T) {
			model := CodexGatewayModel{
				Slug:          "deepseek-v4-pro",
				Provider:      "deepseek",
				UpstreamModel: "deepseek-v4-pro",
			}
			stateStore := newCodexGatewayGoldenStateStore()
			if fixture.SeedState != nil {
				insertCodexGatewayGoldenState(t, stateStore, *fixture.SeedState)
			}
			var buf bytes.Buffer

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				for key, value := range fixture.Upstream.Headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(fixture.Upstream.StatusCode)
				_, err := io.WriteString(w, fixture.Upstream.Body)
				require.NoError(t, err)
			}))
			defer server.Close()

			result, err := ExecuteCodexGatewayDeepSeekStream(
				context.Background(),
				server.Client(),
				server.URL,
				"test-key",
				model,
				fixture.Request.toCreateRequest(),
				stateStore,
				CodexGatewayDeepSeekRequestContext{
					SessionKey:   "fixture-session",
					IsolationKey: "fixture-isolation",
				},
				CodexGatewayDeepSeekRequestConfig{},
				&buf,
			)
			require.NoError(t, err)

			stream := buf.String()
			require.NotContains(t, stream, "[DONE]")
			require.NotContains(t, stream, `"object":"chat.completion.chunk"`)

			events := parseCodexGatewayOrderedEvents(t, stream)
			assertCodexGatewayGoldenEventContract(t, events)
			require.NotEmpty(t, events)
			require.Equal(t, fixture.Expect.TerminalEvent, events[len(events)-1].Event)
			require.Equal(t, fixture.Expect.ResponseStatus, gjson.GetBytes(events[len(events)-1].Payload, "response.status").String())
			require.Equal(t, fixture.Expect.ResponseStatus, result.ProviderResult.Response.Status)

			if fixture.Expect.Usage != nil {
				assertCodexGatewayGoldenUsage(t, events[len(events)-1].Payload, fixture.Expect.Usage)
			}
			if fixture.Expect.ReasoningText != "" {
				require.Equal(t, fixture.Expect.ReasoningText, gjson.GetBytes(events[len(events)-1].Payload, "response.output.0.content.0.text").String())
			}
			if fixture.Expect.OutputText != "" {
				require.Equal(t, fixture.Expect.OutputText, gjson.GetBytes(events[len(events)-1].Payload, "response.output.1.content.0.text").String())
			}
			if len(fixture.Expect.OutputTypes) > 0 {
				require.Equal(t, fixture.Expect.OutputTypes, codexGatewayGoldenOutputTypes(t, events[len(events)-1].Payload))
			}
			if fixture.Expect.ToolName != "" {
				terminal := events[len(events)-1].Payload
				require.Equal(t, fixture.Expect.ToolName, codexGatewayGoldenFirstOutputString(terminal, "name"))
				if fixture.Expect.ToolArguments != "" {
					toolArg := codexGatewayGoldenFirstOutputString(terminal, "arguments")
					if toolArg == "" {
						toolArg = codexGatewayGoldenFirstOutputString(terminal, "input")
					}
					require.Equal(t, fixture.Expect.ToolArguments, toolArg)
				}
			}
			if fixture.Expect.StoredToolCalls > 0 {
				require.Len(t, result.ProviderResult.ToolCalls, fixture.Expect.StoredToolCalls)
			}
			if fixture.Expect.ErrorType != "" {
				terminal := events[len(events)-1].Payload
				require.Equal(t, fixture.Expect.ErrorType, gjson.GetBytes(terminal, "response.error.type").String())
				require.Equal(t, fixture.Expect.ErrorCode, gjson.GetBytes(terminal, "response.error.code").String())
				require.Equal(t, fixture.Expect.ErrorMessage, gjson.GetBytes(terminal, "response.error.message").String())
			}
		})
	}
}

func TestCodexGatewayDeepSeekIntegrationGolden_RequestFixtures(t *testing.T) {
	for _, fixtureName := range []string{
		"tool_result_followup_request",
		"reasoning_tool_loop_replay_request",
		"final_assistant_reasoning_replay_request",
		"missing_reasoning_invalid_state_request",
	} {
		fixture := loadCodexGatewayGoldenFixture(t, fixtureName)
		t.Run(fixture.Name, func(t *testing.T) {
			model := CodexGatewayModel{
				Slug:          "deepseek-v4-pro",
				Provider:      "deepseek",
				UpstreamModel: "deepseek-v4-pro",
			}
			stateStore := newCodexGatewayGoldenStateStore()
			require.NotNil(t, fixture.SeedState)
			if fixture.Expect.ErrorContains != "" {
				insertCodexGatewayGoldenState(t, stateStore, *fixture.SeedState)
			} else {
				require.NoError(t, stateStore.Put(*fixture.SeedState))
			}

			prepared, err := BuildCodexGatewayDeepSeekRequest(
				model,
				fixture.Request.toCreateRequest(),
				stateStore,
				CodexGatewayDeepSeekRequestContext{
					SessionKey:   "fixture-session",
					IsolationKey: "fixture-isolation",
				},
				CodexGatewayDeepSeekRequestConfig{},
			)
			if fixture.Expect.ErrorContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), fixture.Expect.ErrorContains)
				return
			}
			require.NoError(t, err)

			rawMessages, err := json.Marshal(prepared.Body["messages"])
			require.NoError(t, err)
			require.JSONEq(t, string(fixture.Expect.Messages), string(rawMessages))
		})
	}
}

func loadCodexGatewayGoldenFixture(t *testing.T, name string) codexGatewayGoldenFixture {
	t.Helper()

	path := filepath.Join("testdata", "codex_gateway", name+".json")
	body, err := os.ReadFile(path)
	require.NoError(t, err)

	var fixture codexGatewayGoldenFixture
	require.NoError(t, json.Unmarshal(body, &fixture))
	return fixture
}

func newCodexGatewayGoldenStateStore() *CodexGatewayStateStore {
	return NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{
		TTL:      time.Minute,
		MaxItems: 16,
		Now:      time.Now,
	})
}

func insertCodexGatewayGoldenState(t *testing.T, store *CodexGatewayStateStore, state CodexGatewayResponseState) {
	t.Helper()

	key := codexGatewayStateStorageKey(state.Key)
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries[key] = codexGatewayStateEntry{
		state:     cloneCodexGatewayResponseState(state),
		createdAt: store.nowTime(),
		expiresAt: store.nowTime().Add(time.Minute),
	}
}

func assertCodexGatewayGoldenEventContract(t *testing.T, events []codexGatewayOrderedEvent) {
	t.Helper()
	require.NotEmpty(t, events)
	terminalEvent := events[len(events)-1].Event
	require.Contains(t, []string{"response.completed", "response.incomplete", "response.failed"}, terminalEvent)
	if events[0].Event != "response.created" {
		require.Len(t, events, 1)
		require.Equal(t, "response.failed", events[0].Event)
		return
	}

	seenAdded := make(map[string]int)
	seenDelta := make(map[string]int)
	seenDone := make(map[string]int)

	for i, event := range events {
		var payload map[string]any
		require.NoError(t, json.Unmarshal(event.Payload, &payload))
		switch event.Event {
		case "response.output_item.added":
			key := gjson.GetBytes(event.Payload, "item.id").String()
			if key == "" {
				key = "output_index:" + gjson.GetBytes(event.Payload, "output_index").String()
			}
			seenAdded[key] = i
		case "response.output_text.delta", "response.reasoning_text.delta", "response.function_call_arguments.delta":
			key := gjson.GetBytes(event.Payload, "item_id").String()
			if key == "" {
				key = "output_index:" + gjson.GetBytes(event.Payload, "output_index").String()
			}
			seenDelta[key] = i
		case "response.output_item.done", "response.output_text.done", "response.reasoning_text.done", "response.function_call_arguments.done":
			key := gjson.GetBytes(event.Payload, "item.id").String()
			if key == "" {
				key = gjson.GetBytes(event.Payload, "item_id").String()
			}
			if key == "" {
				key = "output_index:" + gjson.GetBytes(event.Payload, "output_index").String()
			}
			seenDone[key] = i
		}
	}

	for key, doneIndex := range seenDone {
		if addedIndex, ok := seenAdded[key]; ok {
			require.Lessf(t, addedIndex, doneIndex, "expected added before done for %s", key)
		}
		if deltaIndex, ok := seenDelta[key]; ok {
			require.Lessf(t, deltaIndex, doneIndex, "expected delta before done for %s", key)
		}
	}
}

func assertCodexGatewayGoldenUsage(t *testing.T, payload []byte, usage *codexGatewayGoldenUsage) {
	t.Helper()
	require.Equal(t, int64(usage.InputTokens), gjson.GetBytes(payload, "response.usage.input_tokens").Int())
	require.Equal(t, int64(usage.OutputTokens), gjson.GetBytes(payload, "response.usage.output_tokens").Int())
	require.Equal(t, int64(usage.TotalTokens), gjson.GetBytes(payload, "response.usage.total_tokens").Int())
	require.Equal(t, int64(usage.CachedTokens), gjson.GetBytes(payload, "response.usage.input_tokens_details.cached_tokens").Int())
}

func codexGatewayGoldenOutputTypes(t *testing.T, payload []byte) []string {
	t.Helper()
	out := make([]string, 0)
	result := gjson.GetBytes(payload, "response.output")
	require.True(t, result.Exists())
	result.ForEach(func(_, value gjson.Result) bool {
		out = append(out, value.Get("type").String())
		return true
	})
	return out
}

func codexGatewayGoldenFirstOutputString(payload []byte, field string) string {
	result := gjson.GetBytes(payload, "response.output")
	value := ""
	result.ForEach(func(_, item gjson.Result) bool {
		if candidate := item.Get(field).String(); candidate != "" {
			value = candidate
			return false
		}
		return true
	})
	return value
}

func cloneGoldenRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return json.RawMessage(out)
}
