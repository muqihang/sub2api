package service

import "strings"

const (
	AnthropicCompatFidelityL0 = "L0"
	AnthropicCompatFidelityL1 = "L1"
	AnthropicCompatFidelityL3 = "L3"
)

var AnthropicCompatShapeHealthcheckFields = []string{
	"inbound_route",
	"cc_gateway_route",
	"client_type",
	"server_filled_shape",
	"server_filled_fields",
	"persona_source",
	"compat_fidelity_level",
	"tool_search_mode",
	"tool_reference_present",
	"defer_loading_present",
	"eager_input_streaming_present",
	"capability_backed",
	"model_preserved",
	"max_tokens_preserved",
	"stream_preserved",
	"thinking_preserved",
	"tools_preserved_or_stripped_with_audit",
	"context_management_preserved",
	"output_config_preserved",
	"raw_body_omitted",
}

type AnthropicCompatShapeHealthcheck struct {
	Level       string   `json:"level"`
	Fields      []string `json:"fields"`
	Passed      int      `json:"passed"`
	Denominator int      `json:"denominator"`
}

func EvaluateAnthropicCompatShapeHealthcheck(summary AnthropicCompatAuditSummary, checks map[string]bool) AnthropicCompatShapeHealthcheck {
	fields := append([]string(nil), AnthropicCompatShapeHealthcheckFields...)
	passed := 0
	for _, field := range fields {
		if checks[field] {
			passed++
		}
	}
	return AnthropicCompatShapeHealthcheck{
		Level:       ClassifyAnthropicCompatFidelity(summary, checks),
		Fields:      fields,
		Passed:      passed,
		Denominator: len(fields),
	}
}

func ClassifyAnthropicCompatFidelity(summary AnthropicCompatAuditSummary, checks map[string]bool) string {
	if summary.ClientType != AnthropicCompatClientType || summary.InboundRoute != AnthropicCompatInboundMessages || summary.CCGatewayRoute != AnthropicCompatCCGatewayMessages {
		return AnthropicCompatFidelityL0
	}
	passed, denominator := anthopicCompatHealthcheckPassCount(checks)
	if denominator == 0 || passed < denominator {
		return AnthropicCompatFidelityL1
	}
	if summary.CapabilityBacked && summary.ToolSearchMode == "capability_backed" && checks["capability_backed"] {
		return AnthropicCompatFidelityL3
	}
	if summary.ServerFilledShape && summary.PersonaSource == "server_selected" && summary.CompatFidelityLevel == AnthropicCompatFidelityL2 {
		return AnthropicCompatFidelityL2
	}
	return AnthropicCompatFidelityL1
}

func anthopicCompatHealthcheckPassCount(checks map[string]bool) (passed int, denominator int) {
	denominator = len(AnthropicCompatShapeHealthcheckFields)
	for _, field := range AnthropicCompatShapeHealthcheckFields {
		if checks[field] {
			passed++
		}
	}
	return passed, denominator
}

func HasAnthropicCompatHealthcheckField(field string) bool {
	field = strings.TrimSpace(field)
	for _, item := range AnthropicCompatShapeHealthcheckFields {
		if item == field {
			return true
		}
	}
	return false
}
