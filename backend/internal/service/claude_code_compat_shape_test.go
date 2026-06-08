package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeAnthropicCompatMessagesBodyFillsAuditableShapeWithoutDroppingCapabilities(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-7","max_tokens":32000,"stream":true,"system":"user system","thinking":{"type":"enabled","budget_tokens":1024},"context_management":{"edits":[]},"output_config":{"effort":"max"},"tools":[{"name":"lookup","description":"lookup","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	require.True(t, shape.ServerFilledShape)
	require.Equal(t, AnthropicCompatClientType, shape.ClientType)
	require.Equal(t, "server_selected", shape.PersonaSource)
	require.Equal(t, "L2", shape.CompatFidelityLevel)
	require.Equal(t, "truthful_pass_through", shape.ToolSearchMode)
	require.False(t, shape.CapabilityBacked)
	require.ElementsMatch(t, []string{"metadata", "metadata.user_id", "system"}, shape.ServerFilledFields)

	require.Equal(t, "claude-opus-4-7", gjson.GetBytes(normalized, "model").String())
	require.Equal(t, int64(32000), gjson.GetBytes(normalized, "max_tokens").Int())
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.True(t, gjson.GetBytes(normalized, "thinking").Exists())
	require.True(t, gjson.GetBytes(normalized, "context_management").Exists())
	require.True(t, gjson.GetBytes(normalized, "output_config").Exists())
	require.True(t, gjson.GetBytes(normalized, "tools").IsArray())
	require.Equal(t, "lookup", gjson.GetBytes(normalized, "tools.0.name").String())

	system := gjson.GetBytes(normalized, "system")
	require.True(t, system.IsArray())
	require.Contains(t, system.Get("0.text").String(), "server-normalized Anthropic /v1/messages")
	require.Contains(t, system.Get("0.text").String(), "no native Claude Code attestation")
	require.Contains(t, system.Get("1.text").String(), "Working directory:")
	require.Equal(t, "user system", system.Get("2.text").String())

	uidRaw := gjson.GetBytes(normalized, "metadata.user_id").String()
	require.NotEmpty(t, uidRaw)
	var uid map[string]string
	require.NoError(t, json.Unmarshal([]byte(uidRaw), &uid))
	require.NotEmpty(t, uid["session_id"])
	require.Equal(t, "compat-device-ref", uid["device_id"])
	require.Equal(t, "compat-account-ref", uid["account_uuid"])
}

func TestNormalizeAnthropicCompatMessagesBodyAddsNoFakeTools(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	require.Equal(t, "not_present", shape.ToolSearchMode)
	require.False(t, shape.CapabilityBacked)
	require.True(t, gjson.GetBytes(normalized, "tools").IsArray())
	require.Len(t, gjson.GetBytes(normalized, "tools").Array(), 0)
	require.Contains(t, strings.Join(shape.ServerFilledFields, ","), "tools")
}

func TestNormalizeAnthropicCompatMessagesBodyStripsNativeOnlyToolReferencesWithAudit(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello","tool_reference":{"id":"nested-ref"}}]}],"tools":[{"name":"regular","description":"regular anthropic tool","input_schema":{"type":"object"}},{"name":"native-search","type":"tool_search_tool_regex_20251119","tool_reference":{"id":"native-ref"},"custom":{"defer_loading":true},"input_schema":{"type":"object"}}],"tool_reference":{"id":"top-level-native"},"defer_loading":true,"eager_input_streaming":true}`)

	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	require.Equal(t, "strip_with_audit", shape.ToolSearchMode)
	require.True(t, shape.ToolReferencePresent)
	require.True(t, shape.DeferLoadingPresent)
	require.True(t, shape.EagerInputStreamingPresent)
	require.False(t, shape.CapabilityBacked)
	require.Contains(t, shape.ServerFilledFields, "tool_reference")
	require.Contains(t, shape.ServerFilledFields, "defer_loading")
	require.Contains(t, shape.ServerFilledFields, "tools.native_only")

	require.False(t, gjson.GetBytes(normalized, "tool_reference").Exists())
	require.False(t, gjson.GetBytes(normalized, "defer_loading").Exists())
	require.False(t, gjson.GetBytes(normalized, "eager_input_streaming").Exists())
	require.Len(t, gjson.GetBytes(normalized, "tools").Array(), 1)
	require.Equal(t, "regular", gjson.GetBytes(normalized, "tools.0.name").String())
	require.False(t, strings.Contains(string(normalized), "tool_search_tool_regex_20251119"))
	require.False(t, strings.Contains(string(normalized), "native-ref"))
	require.False(t, strings.Contains(string(normalized), "nested-ref"))
}

func TestNormalizeAnthropicCompatMessagesBodyPassesThroughPlainAnthropicToolsOnly(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","description":"plain tool","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`)

	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	require.Equal(t, "truthful_pass_through", shape.ToolSearchMode)
	require.False(t, shape.ToolReferencePresent)
	require.False(t, shape.DeferLoadingPresent)
	require.False(t, shape.CapabilityBacked)
	require.Len(t, gjson.GetBytes(normalized, "tools").Array(), 1)
	require.Equal(t, "lookup", gjson.GetBytes(normalized, "tools.0.name").String())
}

func TestNormalizeAnthropicCompatMessagesBodyPreservesToolSchemaParameterNames(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","description":"plain tool","input_schema":{"type":"object","properties":{"tool_reference":{"type":"string"},"defer_loading":{"type":"boolean"},"eager_input_streaming":{"type":"boolean"}}}}]}`)

	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	require.Equal(t, "truthful_pass_through", shape.ToolSearchMode)
	require.False(t, shape.ToolReferencePresent)
	require.False(t, shape.DeferLoadingPresent)
	require.False(t, shape.EagerInputStreamingPresent)
	require.True(t, gjson.GetBytes(normalized, "tools.0.input_schema.properties.tool_reference").Exists())
	require.True(t, gjson.GetBytes(normalized, "tools.0.input_schema.properties.defer_loading").Exists())
	require.True(t, gjson.GetBytes(normalized, "tools.0.input_schema.properties.eager_input_streaming").Exists())
}

func TestNormalizeAnthropicCompatMessagesBodyMovesSystemRoleMessagesToTopLevelSystem(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","system":[{"type":"text","text":"existing top-level system","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"system","content":"private system string sentinel"},{"role":"user","content":"first user"},{"role":"system","content":[{"type":"text","text":"private system block sentinel"},{"type":"image","source":{"type":"base64","data":"IMAGE_PROMPT_SENTINEL"}}]},{"role":"assistant","content":"assistant reply"},{"role":"user","content":"second user"}]}`)

	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	require.Contains(t, shape.ServerFilledFields, "system")

	messages := gjson.GetBytes(normalized, "messages").Array()
	require.Len(t, messages, 3)
	require.Equal(t, "user", messages[0].Get("role").String())
	require.Equal(t, "first user", messages[0].Get("content").String())
	require.Equal(t, "assistant", messages[1].Get("role").String())
	require.Equal(t, "assistant reply", messages[1].Get("content").String())
	require.Equal(t, "user", messages[2].Get("role").String())
	require.Equal(t, "second user", messages[2].Get("content").String())
	require.NotContains(t, string(normalized), `"role":"system"`)

	system := gjson.GetBytes(normalized, "system")
	require.True(t, system.IsArray())
	require.Contains(t, system.Get("0.text").String(), "server-normalized Anthropic /v1/messages")
	require.Contains(t, system.Get("1.text").String(), "Working directory:")
	require.Equal(t, "existing top-level system", system.Get("2.text").String())
	require.Equal(t, "ephemeral", system.Get("2.cache_control.type").String())
	require.Equal(t, "private system string sentinel", system.Get("3.text").String())
	require.Equal(t, "private system block sentinel", system.Get("4.text").String())
	require.NotContains(t, string(normalized), "IMAGE_PROMPT_SENTINEL")

	decision := AnthropicCompatIngressDecision{InboundRoute: AnthropicCompatInboundMessages, CCGatewayRoute: AnthropicCompatCCGatewayMessages, ClientType: AnthropicCompatClientType}
	summary := NewAnthropicCompatAuditSummaryWithShape(decision, shape)
	safe := BuildAnthropicCompatOpsRequestBodySummary(normalized, summary, "", false)
	for _, forbidden := range []string{"private system string sentinel", "private system block sentinel", "first user", "assistant reply", "second user", "IMAGE_PROMPT_SENTINEL"} {
		require.NotContains(t, string(safe), forbidden)
	}
}
