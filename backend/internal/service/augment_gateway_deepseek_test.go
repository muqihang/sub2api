package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayDeepSeekGoldenJSON_ThinkingToolCallRequest(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()

	for _, modelID := range []string{"deepseek-v4-pro", "deepseek-v4-flash"} {
		t.Run(modelID, func(t *testing.T) {
			model, ok := reg.Resolve(modelID)
			require.True(t, ok)

			out, err := BuildAugmentGatewayDeepSeekChatCompletionsJSON(model, map[string]any{
				"model": "unsanitized-model",
				"messages": []any{
					map[string]any{"role": "user", "content": "find references"},
					map[string]any{
						"role":              "assistant",
						"reasoning_content": "",
						"tool_calls": []any{
							map[string]any{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "codebase-retrieval",
									"arguments": "{}",
								},
							},
						},
					},
					map[string]any{
						"role":         "tool",
						"tool_call_id": "call_1",
						"content":      "retrieved context",
					},
				},
				"tools": []any{
					map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":        "codebase-retrieval",
							"description": "retrieve workspace context",
							"parameters":  map[string]any{"type": "object"},
						},
					},
				},
				"tool_choice": "auto",
				"stream":      true,
			})
			require.NoError(t, err)

			require.JSONEq(t, `{
				"model": "`+modelID+`",
				"messages": [
					{ "role": "user", "content": "find references" },
					{
						"role": "assistant",
						"content": "",
						"reasoning_content": "",
						"tool_calls": [
							{
								"id": "call_1",
								"type": "function",
								"function": { "name": "codebase-retrieval", "arguments": "{}" }
							}
						]
					},
					{ "role": "tool", "tool_call_id": "call_1", "content": "retrieved context" }
				],
				"tools": [
					{
						"type": "function",
						"function": {
							"name": "codebase-retrieval",
							"description": "retrieve workspace context",
							"parameters": { "type": "object" }
						}
					}
				],
				"thinking": { "type": "enabled" },
				"reasoning_effort": "max",
				"stream": true
			}`, string(out))

			var body map[string]any
			require.NoError(t, json.Unmarshal(out, &body))
			require.NotContains(t, body, "tool_choice")

			messages, ok := body["messages"].([]any)
			require.True(t, ok)
			assistant, ok := messages[1].(map[string]any)
			require.True(t, ok)
			require.Contains(t, assistant, "content")
			require.Equal(t, "", assistant["content"])
			require.Contains(t, assistant, "reasoning_content")
			require.Equal(t, "", assistant["reasoning_content"])
		})
	}
}

func TestAugmentGatewayDeepSeekInjectsEmptyReasoningContentWhenMissing(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()
	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)

	out, err := BuildAugmentGatewayDeepSeekChatCompletionsJSON(model, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "use a tool"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_missing_reasoning",
						"type": "function",
						"function": map[string]any{
							"name":      "codebase-retrieval",
							"arguments": "{}",
						},
					},
				},
			},
		},
		"tools":       []any{map[string]any{"type": "function", "function": map[string]any{"name": "codebase-retrieval"}}},
		"tool_choice": "auto",
	})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))

	messages := body["messages"].([]any)
	assistant := messages[1].(map[string]any)
	require.Equal(t, "", assistant["content"])
	require.Contains(t, assistant, "reasoning_content")
	require.Equal(t, "", assistant["reasoning_content"])
	require.NotContains(t, body, "tool_choice")
}

func TestAugmentGatewayDeepSeekAddsReasoningContentToAssistantHistoryDuringToolLoop(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()
	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)

	out, err := BuildAugmentGatewayDeepSeekChatCompletionsJSON(model, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "first question"},
			map[string]any{"role": "assistant", "content": "plain prior answer"},
			map[string]any{"role": "user", "content": "now use a tool"},
			map[string]any{
				"role":              "assistant",
				"content":           "",
				"reasoning_content": "must be replayed",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "codebase-retrieval",
							"arguments": `{"query":"real query"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "retrieved context"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "codebase-retrieval"}}},
	})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))

	messages := body["messages"].([]any)
	priorAssistant := messages[1].(map[string]any)
	require.Contains(t, priorAssistant, "reasoning_content")
	require.Equal(t, "", priorAssistant["reasoning_content"])

	toolAssistant := messages[3].(map[string]any)
	require.Equal(t, "must be replayed", toolAssistant["reasoning_content"])
}

func TestAugmentGatewayDeepSeekPairsSyntheticToolCallsWithTheirResults(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()
	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)

	out, err := BuildAugmentGatewayDeepSeekChatCompletionsJSON(model, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "continue after tools"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "codebase-retrieval",
							"arguments": `{"query":"first"}`,
						},
					},
				},
			},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_2",
						"type": "function",
						"function": map[string]any{
							"name":      "codebase-retrieval",
							"arguments": `{"query":"second"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "first result"},
			map[string]any{"role": "tool", "tool_call_id": "call_2", "content": "second result"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "codebase-retrieval"}}},
	})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))

	messages := body["messages"].([]any)
	require.Len(t, messages, 5)
	require.Equal(t, "assistant", messages[1].(map[string]any)["role"])
	require.Equal(t, "call_1", messages[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"])
	require.Equal(t, "tool", messages[2].(map[string]any)["role"])
	require.Equal(t, "call_1", messages[2].(map[string]any)["tool_call_id"])
	require.Equal(t, "assistant", messages[3].(map[string]any)["role"])
	require.Equal(t, "call_2", messages[3].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["id"])
	require.Equal(t, "tool", messages[4].(map[string]any)["role"])
	require.Equal(t, "call_2", messages[4].(map[string]any)["tool_call_id"])
}

func TestAugmentGatewayDeepSeekDoesNotMoveToolResultsAcrossNonToolMessages(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()
	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)

	out, err := BuildAugmentGatewayDeepSeekChatCompletionsJSON(model, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "first"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_late",
						"type": "function",
						"function": map[string]any{
							"name":      "codebase-retrieval",
							"arguments": `{"query":"late"}`,
						},
					},
				},
			},
			map[string]any{"role": "user", "content": "new user message before tool result"},
			map[string]any{"role": "tool", "tool_call_id": "call_late", "content": "late result"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "codebase-retrieval"}}},
	})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))

	messages := body["messages"].([]any)
	require.Len(t, messages, 4)
	require.Equal(t, "assistant", messages[1].(map[string]any)["role"])
	require.Equal(t, "user", messages[2].(map[string]any)["role"])
	require.Equal(t, "tool", messages[3].(map[string]any)["role"])
}
