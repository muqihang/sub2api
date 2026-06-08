package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	AnthropicCompatClientType                 = "claude_code_compat"
	AnthropicCompatInboundMessages            = "/v1/messages"
	AnthropicCompatCCGatewayMessages          = "/v1/messages?beta=true"
	AnthropicCompatInboundRouteHeader         = "x-sub2api-compat-inbound-route"
	AnthropicCompatCCGatewayRouteHeader       = "x-sub2api-compat-cc-gateway-route"
	AnthropicCompatClientTypeHeader           = "x-sub2api-compat-client-type"
	AnthropicCompatServerFilledShapeHeader    = "x-sub2api-compat-server-filled-shape"
	AnthropicCompatServerFilledFieldsHeader   = "x-sub2api-compat-server-filled-fields"
	AnthropicCompatPersonaSourceHeader        = "x-sub2api-compat-persona-source"
	AnthropicCompatFidelityLevelHeader        = "x-sub2api-compat-fidelity-level"
	AnthropicCompatToolSearchModeHeader       = "x-sub2api-compat-tool-search-mode"
	AnthropicCompatCapabilityBackedHeader     = "x-sub2api-compat-capability-backed"
	AnthropicCompatToolReferencePresentHeader = "x-sub2api-compat-tool-reference-present"
	AnthropicCompatDeferLoadingPresentHeader  = "x-sub2api-compat-defer-loading-present"
	AnthropicCompatEagerInputStreamingHeader  = "x-sub2api-compat-eager-input-streaming-present"
	anthropicCompatUnsupportedMessage         = "Only Anthropic /v1/messages protocol is supported for Claude Code compatibility"
)

var anthropicCompatOpenAIOnlyTopLevelFields = []string{
	"audio",
	"frequency_penalty",
	"function_call",
	"functions",
	"input",
	"instructions",
	"logit_bias",
	"logprobs",
	"max_completion_tokens",
	"modalities",
	"parallel_tool_calls",
	"presence_penalty",
	"prompt",
	"response_format",
	"seed",
	"store",
	"top_logprobs",
}

type AnthropicCompatAuditSummary struct {
	InboundRoute               string   `json:"inbound_route"`
	CCGatewayRoute             string   `json:"cc_gateway_route"`
	ClientType                 string   `json:"client_type"`
	PersonaSource              string   `json:"persona_source"`
	ServerFilledShape          bool     `json:"server_filled_shape"`
	ServerFilledFields         []string `json:"server_filled_fields"`
	CompatFidelityLevel        string   `json:"compat_fidelity_level"`
	ToolSearchMode             string   `json:"tool_search_mode"`
	ToolReferencePresent       bool     `json:"tool_reference_present"`
	DeferLoadingPresent        bool     `json:"defer_loading_present"`
	EagerInputStreamingPresent bool     `json:"eager_input_streaming_present"`
	CapabilityBacked           bool     `json:"capability_backed"`
}

type anthropicCompatAuditSummaryContextKey struct{}

func NewAnthropicCompatAuditSummary(decision AnthropicCompatIngressDecision) AnthropicCompatAuditSummary {
	return NewAnthropicCompatAuditSummaryWithShape(decision, AnthropicCompatShapeAudit{
		ClientType:          decision.ClientType,
		PersonaSource:       "server_selected",
		CompatFidelityLevel: AnthropicCompatFidelityL2,
		ToolSearchMode:      "not_present",
		CapabilityBacked:    false,
	})
}

func NewAnthropicCompatAuditSummaryWithShape(decision AnthropicCompatIngressDecision, shape AnthropicCompatShapeAudit) AnthropicCompatAuditSummary {
	clientType := shape.ClientType
	if clientType == "" {
		clientType = decision.ClientType
	}
	personaSource := shape.PersonaSource
	if personaSource == "" {
		personaSource = "server_selected"
	}
	compatFidelityLevel := shape.CompatFidelityLevel
	if compatFidelityLevel == "" {
		compatFidelityLevel = AnthropicCompatFidelityL2
	}
	toolSearchMode := shape.ToolSearchMode
	if toolSearchMode == "" {
		toolSearchMode = "not_present"
	}
	return AnthropicCompatAuditSummary{
		InboundRoute:               decision.InboundRoute,
		CCGatewayRoute:             decision.CCGatewayRoute,
		ClientType:                 clientType,
		PersonaSource:              personaSource,
		ServerFilledShape:          shape.ServerFilledShape,
		ServerFilledFields:         append([]string(nil), shape.ServerFilledFields...),
		CompatFidelityLevel:        compatFidelityLevel,
		ToolSearchMode:             toolSearchMode,
		ToolReferencePresent:       shape.ToolReferencePresent,
		DeferLoadingPresent:        shape.DeferLoadingPresent,
		EagerInputStreamingPresent: shape.EagerInputStreamingPresent,
		CapabilityBacked:           shape.CapabilityBacked,
	}
}

func WithAnthropicCompatAuditSummary(ctx context.Context, summary AnthropicCompatAuditSummary) context.Context {
	return context.WithValue(ctx, anthropicCompatAuditSummaryContextKey{}, summary)
}

func AnthropicCompatAuditSummaryFromContext(ctx context.Context) (AnthropicCompatAuditSummary, bool) {
	summary, ok := ctx.Value(anthropicCompatAuditSummaryContextKey{}).(AnthropicCompatAuditSummary)
	return summary, ok
}

type AnthropicCompatIngressDecision struct {
	InboundRoute   string
	CCGatewayRoute string
	ClientType     string
}

type AnthropicCompatProtocolError struct {
	Status  int
	Code    string
	Message string
}

func (e *AnthropicCompatProtocolError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func ValidateAnthropicOnlyCompatIngress(method, rawRoute string, body []byte) (AnthropicCompatIngressDecision, error) {
	pathname, query := splitCompatRoute(rawRoute)
	if method != http.MethodPost {
		return AnthropicCompatIngressDecision{}, anthropicCompatError(http.StatusNotFound, "unsupported_route")
	}
	if pathname != AnthropicCompatInboundMessages {
		return AnthropicCompatIngressDecision{}, anthropicCompatError(http.StatusBadRequest, "unsupported_protocol")
	}
	if query != "" && query != "beta=true" {
		return AnthropicCompatIngressDecision{}, anthropicCompatError(http.StatusNotFound, "unsupported_route")
	}
	if err := validateAnthropicCompatMessagesBody(body); err != nil {
		return AnthropicCompatIngressDecision{}, err
	}
	return AnthropicCompatIngressDecision{
		InboundRoute:   AnthropicCompatInboundMessages,
		CCGatewayRoute: AnthropicCompatCCGatewayMessages,
		ClientType:     AnthropicCompatClientType,
	}, nil
}

func anthropicCompatError(status int, code string) *AnthropicCompatProtocolError {
	return &AnthropicCompatProtocolError{Status: status, Code: code, Message: anthropicCompatUnsupportedMessage}
}

func AnthropicCompatUnsupportedProtocolMessage() string {
	return anthropicCompatUnsupportedMessage
}

func SanitizeAnthropicCompatInboundHeaders(headers http.Header) http.Header {
	out := http.Header{}
	for key, values := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if shouldStripAnthropicCompatInboundHeader(lower) {
			continue
		}
		for _, value := range values {
			out.Add(key, value)
		}
	}
	return out
}

func shouldStripAnthropicCompatInboundHeader(lower string) bool {
	if lower == "" {
		return true
	}
	if lower == "authorization" || lower == "x-api-key" || lower == "cookie" || lower == "set-cookie" || lower == "proxy-authorization" || lower == "proxy-connection" {
		return true
	}
	if lower == "anthropic-beta" || lower == "x-app" || lower == "user-agent" || lower == "anthropic-dangerous-direct-browser-access" || lower == "x-anthropic-billing-header" {
		return true
	}
	return strings.HasPrefix(lower, "x-claude-code-") ||
		strings.HasPrefix(lower, "x-stainless-") ||
		strings.HasPrefix(lower, "x-sub2api-") ||
		strings.HasPrefix(lower, "x-cc-") ||
		strings.Contains(lower, "cch") ||
		strings.Contains(lower, "billing")
}

func splitCompatRoute(rawRoute string) (string, string) {
	if rawRoute == "" {
		return "", ""
	}
	if strings.HasPrefix(rawRoute, "http://") || strings.HasPrefix(rawRoute, "https://") {
		if u, err := url.Parse(rawRoute); err == nil {
			return u.Path, u.RawQuery
		}
	}
	path, query, ok := strings.Cut(rawRoute, "?")
	if !ok {
		return rawRoute, ""
	}
	return path, query
}

func validateAnthropicCompatMessagesBody(body []byte) error {
	if !gjson.ValidBytes(body) {
		return anthropicCompatError(http.StatusBadRequest, "invalid_json")
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
	}
	for _, field := range anthropicCompatOpenAIOnlyTopLevelFields {
		if gjson.GetBytes(body, field).Exists() {
			return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
		}
	}
	model := gjson.GetBytes(body, "model")
	if !model.Exists() || model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
	}
	if !strings.HasPrefix(strings.TrimSpace(model.String()), "claude-") {
		return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
	}
	badMessageRole := false
	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role")
		if role.Type != gjson.String {
			badMessageRole = true
			return false
		}
		switch role.String() {
		case "system", "user", "assistant":
			return true
		default:
			badMessageRole = true
			return false
		}
	})
	if badMessageRole {
		return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
	}
	tools := gjson.GetBytes(body, "tools")
	if tools.Exists() {
		if !tools.IsArray() {
			return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
		}
		openAIShape := false
		tools.ForEach(func(_, tool gjson.Result) bool {
			if tool.Get("function").Exists() {
				openAIShape = true
				return false
			}
			return true
		})
		if openAIShape {
			return anthropicCompatError(http.StatusBadRequest, "unsupported_body_shape")
		}
	}
	return nil
}
