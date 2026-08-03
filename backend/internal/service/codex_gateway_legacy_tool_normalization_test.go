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

func TestNormalizeCodexGatewayLegacyToolRefs_RewritesToolChoiceFunctionName(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Tools: json.RawMessage(`[
			{"type":"custom","name":"apply_patch","description":"edit files","format":{"type":"grammar"}}
		]`),
		ToolChoice: json.RawMessage(`{"type":"custom","function":{"name":"apply_patch"}}`),
		RawFields: map[string]json.RawMessage{
			"tools": json.RawMessage(`[
				{"type":"custom","name":"apply_patch","description":"edit files","format":{"type":"grammar"}}
			]`),
			"tool_choice": json.RawMessage(`{"type":"custom","function":{"name":"apply_patch"}}`),
		},
	}

	require.NoError(t, normalizeCodexGatewayLegacyToolRefs(&req))
	require.JSONEq(t, `{"type":"custom","function":{"name":"edit"}}`, string(req.ToolChoice))
	require.JSONEq(t, `{"type":"custom","function":{"name":"edit"}}`, string(req.RawFields["tool_choice"]))
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

func TestCodexGatewayIsToolSearchEntry_IsExactAndDoesNotMatchNeighboringToolFamilies(t *testing.T) {
	tests := []struct {
		name  string
		entry CodexGatewayToolNameMapEntry
		want  bool
	}{
		{
			name:  "exact alias",
			entry: CodexGatewayToolNameMapEntry{Alias: "tool_search", Name: "tool_search", Kind: CodexGatewayToolKindFunction},
			want:  true,
		},
		{
			name:  "exact visible name",
			entry: CodexGatewayToolNameMapEntry{Name: "tool_search", Kind: CodexGatewayToolKindFunction},
			want:  true,
		},
		{
			name:  "ordinary project search substring",
			entry: CodexGatewayToolNameMapEntry{Alias: "project_search", Name: "project_search", Kind: CodexGatewayToolKindFunction},
			want:  false,
		},
		{
			name:  "hosted web search",
			entry: CodexGatewayToolNameMapEntry{Alias: "web_search", Name: "web_search", Kind: CodexGatewayToolKindHosted},
			want:  false,
		},
		{
			name:  "custom tool named tool_search",
			entry: CodexGatewayToolNameMapEntry{Alias: "tool_search", Name: "tool_search", Kind: CodexGatewayToolKindCustom},
			want:  false,
		},
		{
			name:  "namespace tool_search",
			entry: CodexGatewayToolNameMapEntry{Name: "tool_search", Namespace: "multi_agent_v1", NamespacePath: "multi_agent_v1.tool_search", Kind: CodexGatewayToolKindFunction},
			want:  false,
		},
		{
			name:  "local shell tool",
			entry: CodexGatewayToolNameMapEntry{Alias: "shell__exec", Name: "exec", Namespace: "shell", NamespacePath: "shell.exec", Kind: CodexGatewayToolKindFunction},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, codexGatewayIsToolSearchEntry(tt.entry))
		})
	}
}
