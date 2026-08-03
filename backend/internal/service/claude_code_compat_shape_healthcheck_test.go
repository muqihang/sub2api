package service

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func loadCompatFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "claude_code_compat", name))
	require.NoError(t, err)
	return body
}

func TestAnthropicCompatShapeHealthcheckDenominatorFields(t *testing.T) {
	require.Len(t, AnthropicCompatShapeHealthcheckFields, 20)
	for _, field := range []string{"inbound_route", "cc_gateway_route", "client_type", "server_filled_shape", "server_filled_fields", "compat_fidelity_level", "tool_search_mode", "capability_backed", "raw_body_omitted"} {
		require.True(t, HasAnthropicCompatHealthcheckField(field), field)
	}
}

func TestAnthropicCompatFidelityLevelsL0L1L2L3(t *testing.T) {
	decision := AnthropicCompatIngressDecision{InboundRoute: AnthropicCompatInboundMessages, CCGatewayRoute: AnthropicCompatCCGatewayMessages, ClientType: AnthropicCompatClientType}
	base := NewAnthropicCompatAuditSummary(decision)
	require.Equal(t, AnthropicCompatFidelityL0, ClassifyAnthropicCompatFidelity(AnthropicCompatAuditSummary{}, nil))
	require.Equal(t, AnthropicCompatFidelityL1, ClassifyAnthropicCompatFidelity(base, nil))
	allChecks := allAnthropicCompatHealthcheckChecks(true)

	l2 := NewAnthropicCompatAuditSummaryWithShape(decision, AnthropicCompatShapeAudit{ClientType: AnthropicCompatClientType, ServerFilledShape: true, ServerFilledFields: []string{"system"}, PersonaSource: "server_selected", CompatFidelityLevel: AnthropicCompatFidelityL2, ToolSearchMode: "truthful_pass_through"})
	require.Equal(t, AnthropicCompatFidelityL1, ClassifyAnthropicCompatFidelity(l2, map[string]bool{"raw_body_omitted": true}), "partial denominator coverage must not classify as L2")
	require.Equal(t, AnthropicCompatFidelityL2, ClassifyAnthropicCompatFidelity(l2, allChecks))

	l3 := l2
	l3.CompatFidelityLevel = AnthropicCompatFidelityL3
	l3.ToolSearchMode = "capability_backed"
	l3.CapabilityBacked = true
	require.Equal(t, AnthropicCompatFidelityL3, ClassifyAnthropicCompatFidelity(l3, allChecks))
}

func allAnthropicCompatHealthcheckChecks(value bool) map[string]bool {
	out := make(map[string]bool, len(AnthropicCompatShapeHealthcheckFields))
	for _, field := range AnthropicCompatShapeHealthcheckFields {
		out[field] = value
	}
	return out
}

func TestAnthropicCompatHealthcheckFixtureRouteAndServerFilledAudit(t *testing.T) {
	body := loadCompatFixture(t, "anthropic_messages_plain.json")
	decision, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, "/v1/messages", body)
	require.NoError(t, err)
	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	summary := NewAnthropicCompatAuditSummaryWithShape(decision, shape)

	require.Equal(t, "/v1/messages", summary.InboundRoute)
	require.Equal(t, "/v1/messages?beta=true", summary.CCGatewayRoute)
	require.Equal(t, AnthropicCompatClientType, summary.ClientType)
	require.True(t, summary.ServerFilledShape)
	require.Equal(t, "server_selected", summary.PersonaSource)
	require.Equal(t, AnthropicCompatFidelityL2, summary.CompatFidelityLevel)
	require.Equal(t, "truthful_pass_through", summary.ToolSearchMode)
	require.False(t, summary.CapabilityBacked)
	require.ElementsMatch(t, []string{"metadata", "metadata.user_id", "system"}, summary.ServerFilledFields)

	checks := allAnthropicCompatHealthcheckChecks(true)
	health := EvaluateAnthropicCompatShapeHealthcheck(summary, checks)
	require.Equal(t, AnthropicCompatFidelityL2, health.Level)
	require.Equal(t, len(AnthropicCompatShapeHealthcheckFields), health.Denominator)
	require.Equal(t, health.Denominator, health.Passed)
	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(normalized, "model").String())
	require.Equal(t, int64(32000), gjson.GetBytes(normalized, "max_tokens").Int())
	require.True(t, gjson.GetBytes(normalized, "thinking").Exists())
	require.True(t, gjson.GetBytes(normalized, "context_management").Exists())
	require.True(t, gjson.GetBytes(normalized, "output_config").Exists())
}

func TestAnthropicCompatHealthcheckFixtureOpenAIFailClosedAndSafeError(t *testing.T) {
	_, err := ValidateAnthropicOnlyCompatIngress(http.MethodPost, "/v1/messages", loadCompatFixture(t, "openai_chat_body.json"))
	require.Error(t, err)
	protocolErr, ok := err.(*AnthropicCompatProtocolError)
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, protocolErr.Status)
	require.Equal(t, "unsupported_body_shape", protocolErr.Code)

	actualErrorShape, err := json.Marshal(map[string]any{"status": protocolErr.Status, "code": protocolErr.Code, "message": protocolErr.Message})
	require.NoError(t, err)
	safeError := loadCompatFixture(t, "safe_error.json")
	require.Equal(t, "unsupported_protocol", gjson.GetBytes(safeError, "code").String())
	for _, safe := range [][]byte{safeError, actualErrorShape} {
		for _, forbidden := range []string{"fixture-content-marker", "authorization", "cookie", "raw_body", "raw_prompt"} {
			require.NotContains(t, strings.ToLower(string(safe)), forbidden)
		}
	}
}

func TestAnthropicCompatHealthcheckFixtureToolSearchStripWithAudit(t *testing.T) {
	body := loadCompatFixture(t, "native_only_tool_markers.json")
	normalized, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	require.Equal(t, "strip_with_audit", shape.ToolSearchMode)
	require.True(t, shape.ToolReferencePresent)
	require.True(t, shape.DeferLoadingPresent)
	require.True(t, shape.EagerInputStreamingPresent)
	require.False(t, shape.CapabilityBacked)
	require.Contains(t, shape.ServerFilledFields, "tools.native_only")
	require.False(t, strings.Contains(string(normalized), "tool_search_tool_regex_20251119"))
	require.False(t, strings.Contains(string(normalized), "nested-ref-placeholder"))
	require.Len(t, gjson.GetBytes(normalized, "tools").Array(), 1)
}

func TestAnthropicCompatHealthcheckSafeSummaryOmitsRawBody(t *testing.T) {
	body := loadCompatFixture(t, "anthropic_messages_plain.json")
	decision := AnthropicCompatIngressDecision{InboundRoute: AnthropicCompatInboundMessages, CCGatewayRoute: AnthropicCompatCCGatewayMessages, ClientType: AnthropicCompatClientType}
	_, shape, err := NormalizeAnthropicCompatMessagesBody(body)
	require.NoError(t, err)
	summary := NewAnthropicCompatAuditSummaryWithShape(decision, shape)
	safe := BuildAnthropicCompatOpsRequestBodySummary(body, summary, "", false)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(safe, &decoded))
	require.Equal(t, AnthropicCompatClientType, decoded["client_type"])
	require.Equal(t, float64(1), decoded["messages_count"])
	require.Equal(t, float64(1), decoded["tools_count"])
	for _, forbidden := range []string{"fixture-content-marker", `"messages":[`, `"content"`, `"metadata":`, `"system":`, "input_schema"} {
		require.NotContains(t, string(safe), forbidden)
	}
}
