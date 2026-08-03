package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexGatewayToolMapping_FunctionNamespaceAndCustomTools(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"function",
			"name":"shell_exec-1",
			"description":"run shell",
			"parameters":{"type":"object","properties":{"cmd":{"type":"string"}}},
			"strict":true,
			"output_schema":{"type":"object"}
		},
		{
			"type":"namespace",
			"name":"browser",
			"tools":[
				{
					"name":"open-page",
					"description":"open a page",
					"parameters":{"type":"object","properties":{"url":{"type":"string"}}}
				}
			]
		},
		{
			"type":"custom",
			"name":"mcp shell",
			"custom":{
				"description":"run MCP shell",
				"input_schema":{"type":"object","properties":{"cmd":{"type":"string"}}},
				"defer_loading":true
			}
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Len(t, result.Tools, 3)

	shellTool := mappedToolByName(t, result.Tools, "shell_exec-1")
	require.NotContains(t, shellTool["function"].(map[string]any), "strict")
	require.NotContains(t, shellTool["function"].(map[string]any), "output_schema")

	require.NotNil(t, mappedToolByName(t, result.Tools, "browser__open-page"))
	require.Equal(t, CodexGatewayToolKindNamespace, result.NameMap["browser__open-page"].Kind)
	require.Equal(t, "browser", result.NameMap["browser__open-page"].Namespace)
	require.Equal(t, "open-page", result.NameMap["browser__open-page"].Name)

	require.NotNil(t, mappedToolByName(t, result.Tools, "custom__mcp_shell"))
	require.Equal(t, CodexGatewayToolKindCustom, result.NameMap["custom__mcp_shell"].Kind)
	require.Equal(t, "mcp shell", result.NameMap["custom__mcp_shell"].Name)
}

func TestCodexGatewayToolMapping_EquivalentToolSetsProduceStableOrder(t *testing.T) {
	rawA := json.RawMessage(`[
		{"type":"namespace","name":"browser","tools":[
			{"type":"function","name":"open","parameters":{"type":"object"}},
			{"type":"function","name":"click","parameters":{"type":"object"}}
		]},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar"}},
		{"type":"function","name":"exec_command","parameters":{"type":"object"}},
		{"type":"web_search"}
	]`)
	rawB := json.RawMessage(`[
		{"type":"web_search"},
		{"type":"function","name":"exec_command","parameters":{"type":"object"}},
		{"type":"namespace","name":"browser","tools":[
			{"type":"function","name":"click","parameters":{"type":"object"}},
			{"type":"function","name":"open","parameters":{"type":"object"}}
		]},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
	]`)

	first, err := BuildCodexGatewayToolMapping(rawA, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayToolMapping(rawB, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	require.Equal(t, first.NameMap, second.NameMap)
	require.Equal(t, mappedToolNames(first.Tools), mappedToolNames(second.Tools))
	require.Equal(t, mustMarshalJSON(t, first.Tools), mustMarshalJSON(t, second.Tools))
}

func TestCodexGatewayToolMapping_SortsRequiredSchemaKeys(t *testing.T) {
	rawA := json.RawMessage(`[
		{"type":"function","name":"create_issue","parameters":{
			"type":"object",
			"properties":{
				"title":{"type":"string"},
				"body":{"type":"string"},
				"meta":{"type":"object","properties":{"priority":{"type":"string"},"team":{"type":"string"}},"required":["team","priority"]}
			},
			"required":["meta","body","title"]
		}}
	]`)
	rawB := json.RawMessage(`[
		{"type":"function","name":"create_issue","parameters":{
			"type":"object",
			"properties":{
				"title":{"type":"string"},
				"body":{"type":"string"},
				"meta":{"type":"object","properties":{"priority":{"type":"string"},"team":{"type":"string"}},"required":["priority","team"]}
			},
			"required":["title","body","meta"]
		}}
	]`)

	first, err := BuildCodexGatewayToolMapping(rawA, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayToolMapping(rawB, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	require.Equal(t, mustMarshalJSON(t, first.Tools[0]), mustMarshalJSON(t, second.Tools[0]))
}

func TestCodexGatewayToolMapping_SortsDependentRequiredSchemaKeys(t *testing.T) {
	rawA := json.RawMessage(`[
		{"type":"function","name":"search","parameters":{
			"type":"object",
			"properties":{"mode":{"type":"string"},"query":{"type":"string"},"limit":{"type":"integer"}},
			"dependentRequired":{"mode":["limit","query"],"query":["mode"]}
		}}
	]`)
	rawB := json.RawMessage(`[
		{"type":"function","name":"search","parameters":{
			"type":"object",
			"properties":{"limit":{"type":"integer"},"query":{"type":"string"},"mode":{"type":"string"}},
			"dependentRequired":{"query":["mode"],"mode":["query","limit"]}
		}}
	]`)

	first, err := BuildCodexGatewayToolMapping(rawA, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayToolMapping(rawB, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	require.Equal(t, mustMarshalJSON(t, first.Tools[0]), mustMarshalJSON(t, second.Tools[0]))
	params := first.Tools[0]["function"].(map[string]any)["parameters"].(map[string]any)
	dependent := params["dependentRequired"].(map[string]any)
	require.Equal(t, []any{"limit", "query"}, dependent["mode"])
}

func TestCodexGatewayToolMapping_DoesNotSortSemanticSchemaArrays(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","name":"pick_value","parameters":{
			"type":"object",
			"properties":{
				"mode":{"type":"string","enum":["zeta","alpha"]},
				"payload":{"oneOf":[{"type":"string"},{"type":"number"}],"anyOf":[{"minimum":1},{"maximum":5}]}
			},
			"required":["payload","mode"]
		}}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	params := result.Tools[0]["function"].(map[string]any)["parameters"].(map[string]any)
	properties := params["properties"].(map[string]any)
	mode := properties["mode"].(map[string]any)
	payload := properties["payload"].(map[string]any)
	require.Equal(t, []any{"zeta", "alpha"}, mode["enum"])
	require.Equal(t, "string", payload["oneOf"].([]any)[0].(map[string]any)["type"])
	require.Equal(t, "number", payload["oneOf"].([]any)[1].(map[string]any)["type"])
	require.Equal(t, float64(1), payload["anyOf"].([]any)[0].(map[string]any)["minimum"])
	require.Equal(t, float64(5), payload["anyOf"].([]any)[1].(map[string]any)["maximum"])
	require.Equal(t, []any{"mode", "payload"}, params["required"])
}

func TestCodexGatewayToolMapping_CanonicalizesPropertyNamedRequired(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","name":"schema_edge","parameters":{
			"type":"object",
			"properties":{
				"required":{
					"type":"object",
					"properties":{"b":{"type":"string"},"a":{"type":"string"}},
					"required":["b","a"]
				}
			},
			"required":["required"]
		}}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	params := result.Tools[0]["function"].(map[string]any)["parameters"].(map[string]any)
	properties := params["properties"].(map[string]any)
	requiredProperty := properties["required"].(map[string]any)
	require.Equal(t, []any{"a", "b"}, requiredProperty["required"])
}

func TestCodexGatewayToolMapping_DropsNullRequiredSchemaKeys(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","name":"get_goal","parameters":{
			"type":"object",
			"properties":{
				"filter":{
					"type":"object",
					"properties":{"status":{"type":"string"}},
					"required":null
				}
			},
			"required":null
		}}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	function := result.Tools[0]["function"].(map[string]any)
	params := function["parameters"].(map[string]any)
	require.NotContains(t, params, "required")
	filter := params["properties"].(map[string]any)["filter"].(map[string]any)
	require.NotContains(t, filter, "required")
	require.NotContains(t, mustMarshalJSON(t, result.Tools[0]), `"required":null`)
}

func TestCodexGatewayToolMapping_DropsInvalidRequiredSchemaKeywords(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","name":"get_goal","parameters":{
			"type":"object",
			"$defs":{
				"GoalFilter":{
					"type":"object",
					"properties":{"status":{"type":"string"}},
					"required":null
				}
			},
			"properties":{
				"filter":{"$ref":"#/$defs/GoalFilter"},
				"items":{"type":"array","items":{"type":"object","required":"bad"}},
				"required":null
			},
			"anyOf":[{"type":"object","required":null}],
			"required":null
		}}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	body := mustMarshalJSON(t, result.Tools[0])
	require.NotContains(t, body, `"required":null`)
	require.NotContains(t, body, `"required":"bad"`)

	params := result.Tools[0]["function"].(map[string]any)["parameters"].(map[string]any)
	properties := params["properties"].(map[string]any)
	require.Equal(t, map[string]any{}, properties["required"])
}

func TestCodexGatewayToolMapping_CanonicalizesRequiredStringSlices(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"b": map[string]any{"type": "string"},
			"a": map[string]any{"type": "string"},
		},
		"required": []string{"b", "a"},
		"items": map[string]any{
			"type":     "object",
			"required": []any{"z", nil, "a", float64(1)},
		},
	}

	canonical := canonicalizeCodexGatewayToolSchema(raw).(map[string]any)
	require.Equal(t, []any{"a", "b"}, canonical["required"])
	require.Equal(t, []any{"a", "z"}, canonical["items"].(map[string]any)["required"])
}

func TestCodexGatewayToolMapping_CustomFormatBecomesInputSchema(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"custom",
			"name":"apply_patch",
			"description":"Use the apply_patch tool to edit files.",
			"format":{
				"type":"grammar",
				"syntax":"lark",
				"definition":"start: begin_patch hunk+ end_patch\nbegin_patch: \"*** Begin Patch\" LF\nend_patch: \"*** End Patch\" LF?\n"
			}
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)

	function := result.Tools[0]["function"].(map[string]any)
	require.Equal(t, "custom__apply_patch", function["name"])
	require.Contains(t, function["description"], "exact raw input")
	require.Contains(t, function["description"], "start: begin_patch")

	params := function["parameters"].(map[string]any)
	require.Equal(t, "object", params["type"])
	require.Equal(t, []any{"input"}, params["required"])
	input := params["properties"].(map[string]any)["input"].(map[string]any)
	require.Equal(t, "string", input["type"])
	require.Contains(t, input["description"], "freeform payload")
}

func TestCodexGatewayToolMapping_ExposesHostedResponsesTools(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"web_search"},
		{"type":"image_generation","size":"1024x1024"},
		{"type":"computer_use_preview"},
		{"type":"file_search"},
		{
			"type":"namespace",
			"name":"mcp__computer_use__",
			"tools":[
				{
					"type":"function",
					"name":"click",
					"description":"Click an element",
					"parameters":{"type":"object","properties":{"x":{"type":"number"}}}
				}
			]
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Empty(t, result.IgnoredHostedToolTypes)
	require.Len(t, result.Tools, 5)
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["web_search"].Kind)
	require.NotNil(t, mappedToolByName(t, result.Tools, "web_search"))
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["image_generation"].Kind)
	require.NotNil(t, mappedToolByName(t, result.Tools, "image_generation"))
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["computer_use_preview"].Kind)
	require.NotNil(t, mappedToolByName(t, result.Tools, "computer_use_preview"))
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["file_search"].Kind)
	require.NotNil(t, mappedToolByName(t, result.Tools, "file_search"))
	var alias string
	for _, name := range mappedToolNames(result.Tools) {
		if strings.Contains(name, "mcp_computer_use") {
			alias = name
			break
		}
	}
	require.Contains(t, alias, "mcp_computer_use")
	require.Contains(t, alias, "__click")
	require.Equal(t, CodexGatewayToolKindNamespace, result.NameMap[alias].Kind)
}

func TestCodexGatewayToolMapping_MapsCodexToolSearchWithoutName(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"tool_search",
			"parameters":{
				"type":"object",
				"properties":{
					"query":{"type":"string"},
					"limit":{"type":"integer"}
				},
				"required":["query"]
			}
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)

	function := result.Tools[0]["function"].(map[string]any)
	require.Equal(t, "tool_search", function["name"])
	params := function["parameters"].(map[string]any)
	properties := params["properties"].(map[string]any)
	require.Contains(t, properties, "query")
	require.Contains(t, properties, "limit")

	entry := result.NameMap["tool_search"]
	require.Equal(t, "tool_search", entry.Alias)
	require.Equal(t, CodexGatewayToolKindFunction, entry.Kind)
	require.Equal(t, "tool_search", entry.Name)
}

func TestCodexGatewayToolMapping_PreservesNestedNamespacePath(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"namespace",
			"name":"browser",
			"tools":[
				{
					"type":"namespace",
					"name":"tabs",
					"tools":[
						{"name":"open","parameters":{"type":"object"}}
					]
				}
			]
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	require.Equal(t, "browser__tabs__open", result.Tools[0]["function"].(map[string]any)["name"])
	require.Equal(t, "browser__tabs", result.NameMap["browser__tabs__open"].Namespace)
}

func TestCodexGatewayToolMapping_PreservesNestedCustomNamespacePath(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"namespace",
			"name":"browser",
			"tools":[
				{"type":"custom","name":"scratch pad","custom":{"input_schema":{"type":"object"}}}
			]
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	require.Equal(t, "browser__custom__scratch_pad", result.Tools[0]["function"].(map[string]any)["name"])
	require.Equal(t, CodexGatewayToolKindCustom, result.NameMap["browser__custom__scratch_pad"].Kind)
	require.Equal(t, "browser", result.NameMap["browser__custom__scratch_pad"].Namespace)
}

func TestCodexGatewayToolMapping_LiteralDoubleUnderscoreDoesNotCollapseIntoStructure(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"namespace","name":"a__b","tools":[{"name":"open","parameters":{"type":"object"}}]},
		{"type":"namespace","name":"a","tools":[{"type":"namespace","name":"b","tools":[{"name":"open","parameters":{"type":"object"}}]}]}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Len(t, result.Tools, 2)
	first := result.Tools[0]["function"].(map[string]any)["name"].(string)
	second := result.Tools[1]["function"].(map[string]any)["name"].(string)
	require.NotEqual(t, first, second)
	require.Regexp(t, codexGatewayToolSafeNameRe, first)
	require.Regexp(t, codexGatewayToolSafeNameRe, second)
}

func TestCodexGatewayToolMapping_SanitizesNonASCIIAliasesToASCII(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"namespace","name":"浏览器","tools":[{"name":"open","parameters":{"type":"object"}}]},
		{"type":"namespace","name":"browser","tools":[{"type":"custom","name":"草稿板","custom":{"input_schema":{"type":"object"}}}]}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	require.Len(t, result.Tools, 2)
	first := result.Tools[0]["function"].(map[string]any)["name"].(string)
	second := result.Tools[1]["function"].(map[string]any)["name"].(string)
	require.Regexp(t, codexGatewayToolSafeNameRe, first)
	require.Regexp(t, codexGatewayToolSafeNameRe, second)
	require.Contains(t, strings.Join([]string{first, second}, " "), "__open")
	require.Contains(t, strings.Join([]string{first, second}, " "), "custom__")
}

func TestCodexGatewayToolMapping_TruncatesLongNamesDeterministicallyAndDetectsCollisions(t *testing.T) {
	longName := "namespace_with_a_very_long_name_for_hashing_purposes__tool_with_a_very_long_name_for_hashing_purposes"
	raw := json.RawMessage(`[
		{
			"type":"namespace",
			"name":"namespace with a very long name for hashing purposes",
			"tools":[{"name":"tool with a very long name for hashing purposes","parameters":{"type":"object"}}]
		}
	]`)

	first, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	second, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)

	alias := first.Tools[0]["function"].(map[string]any)["name"].(string)
	require.Equal(t, alias, second.Tools[0]["function"].(map[string]any)["name"])
	require.LessOrEqual(t, len(alias), 64)
	require.NotEqual(t, longName, alias)

	_, err = BuildCodexGatewayToolMapping(json.RawMessage(`[
		{"type":"namespace","name":"browser","tools":[{"name":"open page","parameters":{"type":"object"}}]},
		{"type":"namespace","name":"browser","tools":[{"name":"open page","parameters":{"type":"object"}}]}
	]`), CodexGatewayToolMappingConfig{})
	require.Error(t, err)
}

func TestCodexGatewayToolMapping_StrictBetaStripsOrRejectsUnsupportedConstraints(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"function",
			"name":"search",
			"parameters":{
				"type":"object",
				"properties":{
					"query":{
						"anyOf":[{"type":"string"},{"type":"number"}],
						"minLength":1
					}
				}
			},
			"strict":true
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{
		EnableStrictBeta:               true,
		RejectUnsupportedStrictSchemas: false,
	})
	require.NoError(t, err)
	function := result.Tools[0]["function"].(map[string]any)
	require.Equal(t, true, function["strict"])
	params := function["parameters"].(map[string]any)
	properties := params["properties"].(map[string]any)
	query := properties["query"].(map[string]any)
	require.Contains(t, query, "anyOf")
	require.NotContains(t, query, "minLength")

	_, err = BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{
		EnableStrictBeta:               true,
		RejectUnsupportedStrictSchemas: true,
	})
	require.Error(t, err)

	_, err = BuildCodexGatewayToolMapping(json.RawMessage(`[
		{
			"type":"custom",
			"name":"strict custom",
			"strict":true,
			"custom":{
				"input_schema":{
					"type":"object",
					"properties":{"query":{"minLength":1}}
				}
			}
		}
	]`), CodexGatewayToolMappingConfig{
		EnableStrictBeta:               true,
		RejectUnsupportedStrictSchemas: true,
	})
	require.Error(t, err)
}

func TestCodexGatewayToolMapping_NonStrictSchemasPassThrough(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"function",
			"name":"search",
			"parameters":{
				"type":"object",
				"properties":{
					"query":{
						"anyOf":[{"type":"string"},{"type":"number"}],
						"minLength":1
					}
				}
			}
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{})
	require.NoError(t, err)
	function := result.Tools[0]["function"].(map[string]any)
	params := function["parameters"].(map[string]any)
	properties := params["properties"].(map[string]any)
	query := properties["query"].(map[string]any)
	require.Contains(t, query, "anyOf")
	require.Contains(t, query, "minLength")
	require.NotContains(t, function, "strict")
}

func TestCodexGatewayToolMapping_StrictSchemaPreservesPropertyNamesThatMatchKeywords(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type":"function",
			"name":"search",
			"parameters":{
				"type":"object",
				"properties":{
					"minLength":{"type":"integer"}
				}
			},
			"strict":true
		}
	]`)

	result, err := BuildCodexGatewayToolMapping(raw, CodexGatewayToolMappingConfig{
		EnableStrictBeta:               true,
		RejectUnsupportedStrictSchemas: true,
	})
	require.NoError(t, err)
	function := result.Tools[0]["function"].(map[string]any)
	params := function["parameters"].(map[string]any)
	properties := params["properties"].(map[string]any)
	require.Contains(t, properties, "minLength")
}

func mappedToolNames(tools []map[string]any) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		out = append(out, function["name"].(string))
	}
	return out
}

func mappedToolByName(t *testing.T, tools []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		if function["name"] == name {
			return tool
		}
	}
	require.Failf(t, "mapped tool not found", "name=%s tools=%v", name, mappedToolNames(tools))
	return nil
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}
