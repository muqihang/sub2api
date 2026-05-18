package service

import (
	"encoding/json"
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

	require.Equal(t, "shell_exec-1", result.Tools[0]["function"].(map[string]any)["name"])
	require.NotContains(t, result.Tools[0]["function"].(map[string]any), "strict")
	require.NotContains(t, result.Tools[0]["function"].(map[string]any), "output_schema")

	require.Equal(t, "browser__open-page", result.Tools[1]["function"].(map[string]any)["name"])
	require.Equal(t, CodexGatewayToolKindNamespace, result.NameMap["browser__open-page"].Kind)
	require.Equal(t, "browser", result.NameMap["browser__open-page"].Namespace)
	require.Equal(t, "open-page", result.NameMap["browser__open-page"].Name)

	require.Equal(t, "custom__mcp_shell", result.Tools[2]["function"].(map[string]any)["name"])
	require.Equal(t, CodexGatewayToolKindCustom, result.NameMap["custom__mcp_shell"].Kind)
	require.Equal(t, "mcp shell", result.NameMap["custom__mcp_shell"].Name)
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
	require.Equal(t, "web_search", result.Tools[0]["function"].(map[string]any)["name"])
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["web_search"].Kind)
	require.Equal(t, "image_generation", result.Tools[1]["function"].(map[string]any)["name"])
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["image_generation"].Kind)
	require.Equal(t, "computer_use_preview", result.Tools[2]["function"].(map[string]any)["name"])
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["computer_use_preview"].Kind)
	require.Equal(t, "file_search", result.Tools[3]["function"].(map[string]any)["name"])
	require.Equal(t, CodexGatewayToolKindHosted, result.NameMap["file_search"].Kind)
	alias := result.Tools[4]["function"].(map[string]any)["name"].(string)
	require.Contains(t, alias, "mcp_computer_use")
	require.Contains(t, alias, "__click")
	require.Equal(t, CodexGatewayToolKindNamespace, result.NameMap[alias].Kind)
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
	require.Contains(t, first, "__open")
	require.Contains(t, second, "custom__")
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
