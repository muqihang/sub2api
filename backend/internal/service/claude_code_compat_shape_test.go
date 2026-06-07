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
