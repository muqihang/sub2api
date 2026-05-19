package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCodexGatewayLegacyToolRefs_ClearsStaleToolChoiceWhenToolIsUnavailable(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Tools: json.RawMessage(`[
			{"type":"function","name":"read_file","parameters":{"type":"object","properties":{}}}
		]`),
		ToolChoice: json.RawMessage(`"edit"`),
		RawFields: map[string]json.RawMessage{
			"tools": json.RawMessage(`[
				{"type":"function","name":"read_file","parameters":{"type":"object","properties":{}}}
			]`),
			"tool_choice": json.RawMessage(`"edit"`),
		},
	}

	require.NoError(t, normalizeCodexGatewayLegacyToolRefs(&req))
	require.JSONEq(t, "null", string(req.ToolChoice))
	require.JSONEq(t, "null", string(req.RawFields["tool_choice"]))
}

func TestNormalizeCodexGatewayLegacyToolRefs_KeepsToolChoiceWhenToolIsAvailable(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Tools: json.RawMessage(`[
			{"type":"custom","name":"apply_patch","description":"edit files","format":{"type":"grammar"}}
		]`),
		ToolChoice: json.RawMessage(`"apply_patch"`),
		RawFields: map[string]json.RawMessage{
			"tools": json.RawMessage(`[
				{"type":"custom","name":"apply_patch","description":"edit files","format":{"type":"grammar"}}
			]`),
			"tool_choice": json.RawMessage(`"apply_patch"`),
		},
	}

	require.NoError(t, normalizeCodexGatewayLegacyToolRefs(&req))
	require.JSONEq(t, `"edit"`, string(req.ToolChoice))
	require.JSONEq(t, `"edit"`, string(req.RawFields["tool_choice"]))
}

func TestNormalizeCodexGatewayLegacyToolRefs_KeepsOrdinaryUnknownToolChoiceForValidation(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		ToolChoice: json.RawMessage(`"missing_tool"`),
		RawFields: map[string]json.RawMessage{
			"tool_choice": json.RawMessage(`"missing_tool"`),
		},
	}

	require.NoError(t, normalizeCodexGatewayLegacyToolRefs(&req))
	require.JSONEq(t, `"missing_tool"`, string(req.ToolChoice))
	require.JSONEq(t, `"missing_tool"`, string(req.RawFields["tool_choice"]))
}
