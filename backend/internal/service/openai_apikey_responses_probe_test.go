package service

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestDecideResponsesProbeSupportRequiresFunctionCallFor2xx(t *testing.T) {
	functionCall := []byte(`{"output":[{"type":"reasoning"},{"type":"function_call","name":"probe_ping"}]}`)
	reasoningOnly := []byte(`{"output":[{"type":"reasoning"}]}`)

	cases := []struct {
		name   string
		status int
		body   []byte
		want   bool
	}{
		{"not_found_endpoint_absent", http.StatusNotFound, functionCall, false},
		{"method_not_allowed_endpoint_absent", http.StatusMethodNotAllowed, functionCall, false},
		{"ok_with_function_call_supported", http.StatusOK, functionCall, true},
		{"ok_reasoning_only_unsupported", http.StatusOK, reasoningOnly, false},
		{"ok_invalid_json_unsupported", http.StatusOK, []byte(`not-json`), false},
		{"ok_missing_output_unsupported", http.StatusOK, []byte(`{"status":"completed"}`), false},
		{"bad_request_conservative_supported", http.StatusBadRequest, reasoningOnly, true},
		{"unauthorized_conservative_supported", http.StatusUnauthorized, nil, true},
		{"server_error_conservative_supported", http.StatusInternalServerError, nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, decideResponsesProbeSupport(tc.status, tc.body))
		})
	}
}

func TestResponsesProbeBodyHasFunctionCall(t *testing.T) {
	require.True(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"function_call"}]}`)))
	require.True(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"reasoning"},{"type":"function_call"}]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"reasoning"}]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`garbage`)))
}

func TestSelectResponsesProbeModel(t *testing.T) {
	require.Equal(t, openai.DefaultTestModel, selectResponsesProbeModel(&Account{}))

	account := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{
			"client-b": "zeta-model",
			"client-a": "alpha-model",
		},
	}}
	require.Equal(t, "alpha-model", selectResponsesProbeModel(account))

	accountWithWildcards := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{
			"wildcard": "gpt-*",
			"blank":    "  ",
			"real":     "real-upstream-model",
		},
	}}
	require.Equal(t, "real-upstream-model", selectResponsesProbeModel(accountWithWildcards))

	accountAllWildcards := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{"wildcard": "*"},
	}}
	require.Equal(t, openai.DefaultTestModel, selectResponsesProbeModel(accountAllWildcards))
}

func TestOpenAIResponsesProbePayloadForcesToolCallWithoutDefaultInstructions(t *testing.T) {
	body := openaiResponsesProbePayload("upstream-model")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "upstream-model", payload["model"])
	require.Equal(t, false, payload["stream"])
	require.Equal(t, "required", payload["tool_choice"])
	require.EqualValues(t, 512, payload["max_output_tokens"])
	require.NotContains(t, payload, "instructions")

	tools, ok := payload["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function", tool["type"])
	require.Equal(t, "probe_ping", tool["name"])
}

func TestOpenAIResponsesProbeTimeoutAllowsReasoningModels(t *testing.T) {
	require.Equal(t, 15*time.Second, openaiResponsesProbeTimeout)
}
