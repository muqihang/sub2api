package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	AnthropicCompatFidelityL2 = "L2"
)

type AnthropicCompatShapeAudit struct {
	ClientType                 string   `json:"client_type"`
	ServerFilledShape          bool     `json:"server_filled_shape"`
	ServerFilledFields         []string `json:"server_filled_fields"`
	PersonaSource              string   `json:"persona_source"`
	CompatFidelityLevel        string   `json:"compat_fidelity_level"`
	ToolSearchMode             string   `json:"tool_search_mode"`
	ToolReferencePresent       bool     `json:"tool_reference_present"`
	DeferLoadingPresent        bool     `json:"defer_loading_present"`
	EagerInputStreamingPresent bool     `json:"eager_input_streaming_present"`
	CapabilityBacked           bool     `json:"capability_backed"`
}

// NormalizeAnthropicCompatMessagesBody converts a plain Anthropic Messages body
// into a server-auditable Claude-Code-compatible shape without claiming native
// Claude Code attestation. It preserves user-provided Anthropic capabilities and
// only fills missing server-owned structure that CC Gateway later rewrites/signs.
func NormalizeAnthropicCompatMessagesBody(body []byte) ([]byte, AnthropicCompatShapeAudit, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body, AnthropicCompatShapeAudit{}, err
	}
	if root == nil {
		return body, AnthropicCompatShapeAudit{}, fmt.Errorf("invalid anthropic messages body")
	}

	audit := AnthropicCompatShapeAudit{
		ClientType:          AnthropicCompatClientType,
		ServerFilledShape:   false,
		PersonaSource:       "server_selected",
		CompatFidelityLevel: AnthropicCompatFidelityL2,
		ToolSearchMode:      "not_present",
		CapabilityBacked:    false,
	}
	filled := map[string]struct{}{}
	markFilled := func(field string) {
		if field == "" {
			return
		}
		if _, ok := filled[field]; ok {
			return
		}
		filled[field] = struct{}{}
		audit.ServerFilledFields = append(audit.ServerFilledFields, field)
		audit.ServerFilledShape = true
	}

	stripWithAudit := false
	if tools, ok := root["tools"]; ok {
		toolsOut, stripped := stripNativeOnlyCompatToolMarkers(tools)
		if stripped.ToolReferencePresent {
			audit.ToolReferencePresent = true
			markFilled("tool_reference")
		}
		if stripped.DeferLoadingPresent {
			audit.DeferLoadingPresent = true
			markFilled("defer_loading")
		}
		if stripped.EagerInputStreamingPresent {
			audit.EagerInputStreamingPresent = true
			markFilled("eager_input_streaming")
		}
		if stripped.Changed {
			root["tools"] = toolsOut
			markFilled("tools.native_only")
			stripWithAudit = true
		}
		if isNonEmptyArray(root["tools"]) {
			audit.ToolSearchMode = "truthful_pass_through"
		}
	} else {
		root["tools"] = []any{}
		markFilled("tools")
	}
	cleanedRoot, strippedRoot := stripNativeOnlyCompatKeysDeep(root)
	if strippedRoot.Changed {
		if cleaned, ok := cleanedRoot.(map[string]any); ok {
			root = cleaned
		}
		if strippedRoot.ToolReferencePresent {
			audit.ToolReferencePresent = true
			markFilled("tool_reference")
		}
		if strippedRoot.DeferLoadingPresent {
			audit.DeferLoadingPresent = true
			markFilled("defer_loading")
		}
		if strippedRoot.EagerInputStreamingPresent {
			audit.EagerInputStreamingPresent = true
			markFilled("eager_input_streaming")
		}
		stripWithAudit = true
	}
	if stripWithAudit {
		audit.ToolSearchMode = "strip_with_audit"
	}

	if _, ok := root["metadata"].(map[string]any); !ok {
		root["metadata"] = map[string]any{}
		markFilled("metadata")
	}
	metadata := root["metadata"].(map[string]any)
	if userID, ok := metadata["user_id"].(string); !ok || strings.TrimSpace(userID) == "" {
		metadata["user_id"] = compatMetadataUserID()
		markFilled("metadata.user_id")
	}

	if messages, systemBlocks, changed := extractSystemRoleMessages(root["messages"]); changed {
		root["messages"] = messages
		root["system"] = mergeCompatSystemSources(root["system"], systemBlocks)
	}

	if _, ok := root["system"]; !ok || root["system"] == nil {
		root["system"] = compatSystemBlocks(nil)
		markFilled("system")
	} else {
		root["system"] = compatSystemBlocks(root["system"])
		markFilled("system")
	}

	out, err := json.Marshal(root)
	if err != nil {
		return body, AnthropicCompatShapeAudit{}, err
	}
	return out, audit, nil
}

func extractSystemRoleMessages(messages any) (any, []map[string]any, bool) {
	items, ok := messages.([]any)
	if !ok {
		return messages, nil, false
	}
	out := make([]any, 0, len(items))
	systemBlocks := []map[string]any{}
	changed := false
	for _, item := range items {
		msg, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" {
			out = append(out, item)
			continue
		}
		changed = true
		systemBlocks = append(systemBlocks, normalizeSystemToTextBlocks(msg["content"])...)
	}
	return out, systemBlocks, changed
}

func mergeCompatSystemSources(existing any, extracted []map[string]any) any {
	if len(extracted) == 0 {
		return existing
	}
	out := make([]any, 0, len(extracted)+1)
	for _, block := range normalizeSystemToTextBlocks(existing) {
		out = append(out, block)
	}
	for _, block := range extracted {
		out = append(out, block)
	}
	return out
}

func compatMetadataUserID() string {
	raw, _ := json.Marshal(map[string]string{
		"device_id":    "compat-device-ref",
		"account_uuid": "compat-account-ref",
		"session_id":   uuid.NewString(),
	})
	return string(raw)
}

func compatSystemBlocks(original any) []map[string]any {
	blocks := []map[string]any{
		{
			"type": "text",
			"text": "<system-reminder>\nThis request was server-normalized Anthropic /v1/messages for Claude-Code-compatible routing. It has no native Claude Code attestation; use only capabilities present in this request.\n</system-reminder>",
		},
		{
			"type": "text",
			"text": "<env>\nPlatform: server-selected\nShell: server-selected\nOS Version: server-selected\nWorking directory: server-selected\nHome directory: server-selected\n</env>",
		},
	}
	for _, block := range normalizeSystemToTextBlocks(original) {
		blocks = append(blocks, block)
	}
	return blocks
}

func normalizeSystemToTextBlocks(system any) []map[string]any {
	switch v := system.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []map[string]any{{"type": "text", "text": v}}
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			switch block := item.(type) {
			case string:
				if strings.TrimSpace(block) != "" {
					out = append(out, map[string]any{"type": "text", "text": block})
				}
			case map[string]any:
				if block["type"] == "text" {
					if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
						copyBlock := map[string]any{"type": "text", "text": text}
						if cc, ok := block["cache_control"]; ok {
							copyBlock["cache_control"] = cc
						}
						out = append(out, copyBlock)
					}
				}
			}
		}
		return out
	default:
		return nil
	}
}

func isNonEmptyArray(value any) bool {
	items, ok := value.([]any)
	return ok && len(items) > 0
}

func containsAnthropicToolSearchMarkers(value any) bool {
	return containsKeyDeep(value, "tool_reference") || containsKeyDeep(value, "defer_loading")
}

type nativeOnlyCompatToolStripResult struct {
	Changed                    bool
	ToolReferencePresent       bool
	DeferLoadingPresent        bool
	EagerInputStreamingPresent bool
}

func stripNativeOnlyCompatToolMarkers(value any) (any, nativeOnlyCompatToolStripResult) {
	items, ok := value.([]any)
	if !ok {
		return value, nativeOnlyCompatToolStripResult{}
	}
	out := make([]any, 0, len(items))
	result := nativeOnlyCompatToolStripResult{}
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if isNativeOnlyCompatTool(tool) {
			result.Changed = true
			result.ToolReferencePresent = result.ToolReferencePresent || containsKeyDeep(tool, "tool_reference")
			result.DeferLoadingPresent = result.DeferLoadingPresent || containsKeyDeep(tool, "defer_loading")
			result.EagerInputStreamingPresent = result.EagerInputStreamingPresent || containsKeyDeep(tool, "eager_input_streaming")
			continue
		}
		cleaned, stripped := stripNativeOnlyCompatKeysDeep(tool)
		if stripped.Changed {
			result.Changed = true
			result.ToolReferencePresent = result.ToolReferencePresent || stripped.ToolReferencePresent
			result.DeferLoadingPresent = result.DeferLoadingPresent || stripped.DeferLoadingPresent
			result.EagerInputStreamingPresent = result.EagerInputStreamingPresent || stripped.EagerInputStreamingPresent
			out = append(out, cleaned)
			continue
		}
		out = append(out, item)
	}
	return out, result
}

func isNativeOnlyCompatTool(tool map[string]any) bool {
	toolType, _ := tool["type"].(string)
	return strings.HasPrefix(strings.TrimSpace(toolType), "tool_search_tool_")
}

func stripNativeOnlyCompatKeysDeep(value any) (any, nativeOnlyCompatToolStripResult) {
	return stripNativeOnlyCompatKeysDeepWithPath(value, "")
}

func stripNativeOnlyCompatKeysDeepWithPath(value any, parentKey string) (any, nativeOnlyCompatToolStripResult) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		result := nativeOnlyCompatToolStripResult{}
		for key, item := range v {
			if parentKey == "properties" {
				cleaned, stripped := stripNativeOnlyCompatKeysDeepWithPath(item, key)
				if stripped.Changed {
					result.Changed = true
					result.ToolReferencePresent = result.ToolReferencePresent || stripped.ToolReferencePresent
					result.DeferLoadingPresent = result.DeferLoadingPresent || stripped.DeferLoadingPresent
					result.EagerInputStreamingPresent = result.EagerInputStreamingPresent || stripped.EagerInputStreamingPresent
				}
				out[key] = cleaned
				continue
			}
			switch key {
			case "tool_reference":
				result.Changed = true
				result.ToolReferencePresent = true
				continue
			case "defer_loading":
				result.Changed = true
				result.DeferLoadingPresent = true
				continue
			case "eager_input_streaming":
				result.Changed = true
				result.EagerInputStreamingPresent = true
				continue
			}
			cleaned, stripped := stripNativeOnlyCompatKeysDeepWithPath(item, key)
			if stripped.Changed {
				result.Changed = true
				result.ToolReferencePresent = result.ToolReferencePresent || stripped.ToolReferencePresent
				result.DeferLoadingPresent = result.DeferLoadingPresent || stripped.DeferLoadingPresent
				result.EagerInputStreamingPresent = result.EagerInputStreamingPresent || stripped.EagerInputStreamingPresent
			}
			out[key] = cleaned
		}
		return out, result
	case []any:
		out := make([]any, 0, len(v))
		result := nativeOnlyCompatToolStripResult{}
		for _, item := range v {
			cleaned, stripped := stripNativeOnlyCompatKeysDeepWithPath(item, parentKey)
			if stripped.Changed {
				result.Changed = true
				result.ToolReferencePresent = result.ToolReferencePresent || stripped.ToolReferencePresent
				result.DeferLoadingPresent = result.DeferLoadingPresent || stripped.DeferLoadingPresent
				result.EagerInputStreamingPresent = result.EagerInputStreamingPresent || stripped.EagerInputStreamingPresent
			}
			out = append(out, cleaned)
		}
		return out, result
	default:
		return value, nativeOnlyCompatToolStripResult{}
	}
}

func containsKeyDeep(value any, key string) bool {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			if k == key {
				return true
			}
			if containsKeyDeep(item, key) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsKeyDeep(item, key) {
				return true
			}
		}
	}
	return false
}

func shapeSummaryFromBody(body []byte) AnthropicCompatShapeAudit {
	return AnthropicCompatShapeAudit{
		ClientType:                 AnthropicCompatClientType,
		ServerFilledShape:          false,
		ServerFilledFields:         []string{},
		PersonaSource:              "server_selected",
		CompatFidelityLevel:        AnthropicCompatFidelityL2,
		ToolSearchMode:             toolModeFromBody(body),
		ToolReferencePresent:       gjson.GetBytes(body, "..tool_reference").Exists(),
		DeferLoadingPresent:        gjson.GetBytes(body, "..defer_loading").Exists(),
		EagerInputStreamingPresent: gjson.GetBytes(body, "..eager_input_streaming").Exists(),
		CapabilityBacked:           false,
	}
}

func toolModeFromBody(body []byte) string {
	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() && len(tools.Array()) > 0 {
		return "truthful_pass_through"
	}
	return "not_present"
}

// BuildAnthropicCompatOpsRequestBodySummary returns a bounded, prompt-free
// request summary for ops error records. Compat requests must never persist raw
// Anthropic messages, system text, metadata.user_id, tool schemas, or client
// credentials on error paths.
func BuildAnthropicCompatOpsRequestBodySummary(body []byte, audit AnthropicCompatAuditSummary, model string, stream bool) []byte {
	root := gjson.ParseBytes(body)
	if strings.TrimSpace(model) == "" {
		model = strings.TrimSpace(root.Get("model").String())
	}
	if root.Get("stream").Exists() {
		stream = root.Get("stream").Bool()
	}

	out := map[string]any{
		"client_type":                   AnthropicCompatClientType,
		"inbound_route":                 audit.InboundRoute,
		"cc_gateway_route":              audit.CCGatewayRoute,
		"server_filled_shape":           audit.ServerFilledShape,
		"server_filled_fields":          append([]string(nil), audit.ServerFilledFields...),
		"persona_source":                audit.PersonaSource,
		"compat_fidelity_level":         audit.CompatFidelityLevel,
		"tool_search_mode":              audit.ToolSearchMode,
		"tool_reference_present":        audit.ToolReferencePresent,
		"defer_loading_present":         audit.DeferLoadingPresent,
		"eager_input_streaming_present": audit.EagerInputStreamingPresent,
		"capability_backed":             audit.CapabilityBacked,
		"model":                         model,
		"stream":                        stream,
		"messages_count":                len(root.Get("messages").Array()),
		"tools_count":                   len(root.Get("tools").Array()),
		"system_present":                root.Get("system").Exists(),
		"thinking_present":              root.Get("thinking").Exists(),
		"context_management_present":    root.Get("context_management").Exists(),
		"output_config_present":         root.Get("output_config").Exists(),
	}
	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() && maxTokens.Type == gjson.Number {
		out["max_tokens"] = maxTokens.Int()
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return []byte(`{"client_type":"claude_code_compat","request_body_summary_error":true}`)
	}
	return encoded
}

func applyAnthropicCompatAuditHeaders(headers http.Header, audit AnthropicCompatAuditSummary) {
	setHeaderRaw(headers, AnthropicCompatInboundRouteHeader, audit.InboundRoute)
	setHeaderRaw(headers, AnthropicCompatCCGatewayRouteHeader, audit.CCGatewayRoute)
	setHeaderRaw(headers, AnthropicCompatClientTypeHeader, audit.ClientType)
	setHeaderRaw(headers, AnthropicCompatServerFilledShapeHeader, strconv.FormatBool(audit.ServerFilledShape))
	setHeaderRaw(headers, AnthropicCompatServerFilledFieldsHeader, strings.Join(audit.ServerFilledFields, ","))
	setHeaderRaw(headers, AnthropicCompatPersonaSourceHeader, audit.PersonaSource)
	setHeaderRaw(headers, AnthropicCompatFidelityLevelHeader, audit.CompatFidelityLevel)
	setHeaderRaw(headers, AnthropicCompatToolSearchModeHeader, audit.ToolSearchMode)
	setHeaderRaw(headers, AnthropicCompatCapabilityBackedHeader, strconv.FormatBool(audit.CapabilityBacked))
	setHeaderRaw(headers, AnthropicCompatToolReferencePresentHeader, strconv.FormatBool(audit.ToolReferencePresent))
	setHeaderRaw(headers, AnthropicCompatDeferLoadingPresentHeader, strconv.FormatBool(audit.DeferLoadingPresent))
	setHeaderRaw(headers, AnthropicCompatEagerInputStreamingHeader, strconv.FormatBool(audit.EagerInputStreamingPresent))
}
