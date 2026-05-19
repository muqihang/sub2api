package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexGatewayResponsesCodec_PreservesRawFields(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"instructions":"You are helpful",
		"input":[{"role":"user","content":"hello"}],
		"tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
		"tool_choice":{"type":"auto"},
		"reasoning":{"effort":"high","summary":"auto"},
		"text":{"verbosity":"low"},
		"client_metadata":{"trace_id":"abc"},
		"prompt_cache_key":"turn-abc",
		"max_output_tokens":512,
		"parallel_tool_calls":false,
		"store":true,
		"stream":true,
		"include":["reasoning.encrypted_content"]
	}`)

	req, err := DecodeCodexGatewayResponsesCreateRequest(body)
	require.NoError(t, err)

	require.Equal(t, "gpt-5.5", req.Model)
	require.JSONEq(t, `"You are helpful"`, string(req.Instructions))
	require.JSONEq(t, `[{"role":"user","content":"hello"}]`, string(req.Input))
	require.JSONEq(t, `[{"type":"function","name":"shell","parameters":{"type":"object"}}]`, string(req.Tools))
	require.JSONEq(t, `{"type":"auto"}`, string(req.ToolChoice))
	require.JSONEq(t, `{"effort":"high","summary":"auto"}`, string(req.Reasoning))
	require.JSONEq(t, `{"verbosity":"low"}`, string(req.Text))
	require.JSONEq(t, `{"trace_id":"abc"}`, string(req.ClientMetadata))
	require.JSONEq(t, `["reasoning.encrypted_content"]`, string(req.Include))
	require.Equal(t, "turn-abc", req.PromptCacheKey)
	require.NotNil(t, req.MaxOutputTokens)
	require.Equal(t, 512, *req.MaxOutputTokens)
	require.NotNil(t, req.ParallelToolCalls)
	require.False(t, *req.ParallelToolCalls)
	require.NotNil(t, req.Store)
	require.True(t, *req.Store)
	require.NotNil(t, req.Stream)
	require.True(t, *req.Stream)
	require.Contains(t, req.RawFields, "client_metadata")
}

func TestCodexGatewayResponsesCodec_RejectsPreviousResponseIDForHTTPGateway(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"input":[{"role":"user","content":"hello"}],
		"previous_response_id":"resp_123"
	}`)

	req, err := DecodeCodexGatewayResponsesCreateRequest(body)
	require.NoError(t, err)

	err = ValidateCodexGatewayResponsesCreateRequest(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "previous_response_id")
}

func TestCodexGatewayResponseEvents_StreamLifecycle(t *testing.T) {
	var buf bytes.Buffer
	writer := NewCodexGatewayResponseEventWriter(&buf)

	response := CodexGatewayResponse{
		ID:     "resp_123",
		Object: "response",
		Model:  "gpt-5.5",
		Status: "completed",
		Output: []json.RawMessage{
			json.RawMessage(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}`),
		},
		RawFields: map[string]json.RawMessage{
			"created_at":          json.RawMessage(`1234567890`),
			"parallel_tool_calls": json.RawMessage(`false`),
		},
	}
	item := json.RawMessage(`{"id":"msg_1","type":"message","role":"assistant","content":[]}`)

	require.NoError(t, writer.WriteResponseCreated(response))
	require.NoError(t, writer.WriteResponseInProgress(response))
	require.NoError(t, writer.WriteOutputItemAdded("resp_123", 0, item))
	require.NoError(t, writer.WriteOutputTextDelta("resp_123", "msg_1", 0, 0, "hel"))
	require.NoError(t, writer.WriteFunctionCallArgumentsDelta("resp_123", "fc_call_1", 1, "{\"cmd\":\"ls"))
	require.NoError(t, writer.WriteFunctionCallArgumentsDone("resp_123", "fc_call_1", 1, json.RawMessage(`{"id":"fc_call_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"}`)))
	require.NoError(t, writer.WriteCustomToolCallInputDelta("resp_123", "fc_call_2", "call_2", 2, "*** Begin"))
	require.NoError(t, writer.WriteCustomToolCallInputDone("resp_123", "fc_call_2", 2, "*** Begin Patch\n*** End Patch"))
	require.NoError(t, writer.WriteOutputItemDone("resp_123", 0, item))
	require.NoError(t, writer.WriteResponseCompleted(response))
	require.NoError(t, writer.WriteResponseFailed(CodexGatewayResponse{
		ID:     "resp_failed",
		Object: "response",
		Model:  "gpt-5.5",
		Status: "failed",
		Error: &CodexGatewayResponseError{
			Code:    "upstream_error",
			Message: "boom",
			RawFields: map[string]json.RawMessage{
				"type":  json.RawMessage(`"server_error"`),
				"param": json.RawMessage(`"model"`),
			},
		},
	}))
	require.NoError(t, writer.WriteResponseIncomplete(CodexGatewayResponse{
		ID:                "resp_incomplete",
		Object:            "response",
		Model:             "gpt-5.5",
		Status:            "incomplete",
		IncompleteDetails: json.RawMessage(`{"reason":"max_output_tokens"}`),
	}))

	stream := buf.String()
	for _, eventName := range []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.output_text.delta",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.output_item.done",
		"response.completed",
		"response.failed",
		"response.incomplete",
	} {
		require.Contains(t, stream, "event: "+eventName)
	}

	events := parseCodexGatewayEventPayloads(t, stream)
	created := events["response.created"]
	inProgress := events["response.in_progress"]
	completed := events["response.completed"]
	failed := events["response.failed"]
	incomplete := events["response.incomplete"]

	for _, payload := range []map[string]any{created, inProgress, completed, failed, incomplete} {
		require.Contains(t, payload, "response")
	}

	requireSequentialCodexGatewaySequenceNumbers(t, stream)

	createdResponse, ok := created["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "resp_123", createdResponse["id"])
	require.Equal(t, "response", createdResponse["object"])
	require.Equal(t, "gpt-5.5", createdResponse["model"])
	require.Equal(t, "completed", createdResponse["status"])
	require.Equal(t, float64(1234567890), createdResponse["created_at"])
	require.Equal(t, false, createdResponse["parallel_tool_calls"])

	failedResponse, ok := failed["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "resp_failed", failedResponse["id"])
	require.Equal(t, "failed", failedResponse["status"])
	failedError, ok := failedResponse["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "server_error", failedError["type"])
	require.Equal(t, "model", failedError["param"])

	incompleteResponse, ok := incomplete["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "resp_incomplete", incompleteResponse["id"])
	require.Equal(t, "incomplete", incompleteResponse["status"])

	funcDelta := events["response.function_call_arguments.delta"]
	require.Equal(t, "fc_call_1", funcDelta["item_id"])
	require.Equal(t, float64(1), funcDelta["output_index"])
	funcDone := events["response.function_call_arguments.done"]
	require.Equal(t, "fc_call_1", funcDone["item_id"])
	require.Equal(t, float64(1), funcDone["output_index"])
	doneItem, ok := funcDone["item"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "fc_call_1", doneItem["id"])

	customDelta := events["response.custom_tool_call_input.delta"]
	require.Equal(t, "fc_call_2", customDelta["item_id"])
	require.Equal(t, "call_2", customDelta["call_id"])
	require.Equal(t, "*** Begin", customDelta["delta"])
	customDone := events["response.custom_tool_call_input.done"]
	require.Equal(t, "fc_call_2", customDone["item_id"])
	require.Equal(t, float64(2), customDone["output_index"])
	require.Equal(t, "*** Begin Patch\n*** End Patch", customDone["input"])
}

func TestCodexGatewayErrors_InvalidRequestEnvelope(t *testing.T) {
	body, err := MarshalCodexGatewayErrorJSON("invalid_request_error", "invalid_request", "bad payload")
	require.NoError(t, err)

	require.JSONEq(t, `{
		"error": {
			"type": "invalid_request_error",
			"code": "invalid_request",
			"message": "bad payload"
		}
	}`, string(body))

	require.True(t, strings.Contains(string(body), `"error"`))
}

func parseCodexGatewayEventPayloads(t *testing.T, stream string) map[string]map[string]any {
	t.Helper()

	result := make(map[string]map[string]any)
	blocks := strings.Split(strings.TrimSpace(stream), "\n\n")
	for _, block := range blocks {
		lines := strings.Split(block, "\n")
		require.Len(t, lines, 2)
		eventName := strings.TrimPrefix(lines[0], "event: ")
		raw := strings.TrimPrefix(lines[1], "data: ")
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &payload))
		result[eventName] = payload
	}
	return result
}

func requireSequentialCodexGatewaySequenceNumbers(t *testing.T, stream string) {
	t.Helper()

	blocks := strings.Split(strings.TrimSpace(stream), "\n\n")
	expected := 0
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		require.Len(t, lines, 2)
		raw := strings.TrimPrefix(lines[1], "data: ")
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &payload))
		require.Equal(t, float64(expected), payload["sequence_number"])
		expected++
	}
	require.Greater(t, expected, 0)
}
