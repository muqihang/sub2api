package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	CodexGatewayCaptureSourceCodexBuiltin           = "codex_builtin"
	CodexGatewayCaptureSourceCodexOpenSource        = "codex_open_source"
	CodexGatewayCaptureSourceDesktopBundledBuiltin  = "desktop_bundled_builtin"
	CodexGatewayCaptureSourceDesktopRuntime         = "desktop_runtime"
	CodexGatewayCaptureSourceUserConfig             = "user_config"
	CodexGatewayCaptureSourceProjectDoc             = "project_doc"
	CodexGatewayCaptureSourceSkillMetadata          = "skill_metadata"
	CodexGatewayCaptureSourceMCPRuntime             = "mcp_runtime"
	CodexGatewayCaptureSourceUnknown                = "unknown"
	CodexGatewayCaptureContentPolicyRawAllowed      = "raw_allowed"
	CodexGatewayCaptureContentPolicySummarized      = "summarized"
	CodexGatewayCaptureContentPolicyProtocolSummary = "protocol_summary"
)

func ExtractCodexGatewayCaptureShape(body []byte, redactor *CodexGatewayCaptureRedactor) (map[string]any, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, fmt.Errorf("decode codex gateway capture body shape: %w", err)
	}
	shape, ok := codexGatewayCaptureShapeValue("", value, redactor).(map[string]any)
	if !ok {
		return map[string]any{"type": codexGatewayCaptureTypeName(value)}, nil
	}
	return shape, nil
}

func codexGatewayCaptureShapeValue(key string, value any, redactor *CodexGatewayCaptureRedactor) any {
	return codexGatewayCaptureShapeValueAt(key, key, value, redactor)
}

func codexGatewayCaptureShapeValueAt(key, path string, value any, redactor *CodexGatewayCaptureRedactor) any {
	switch typed := value.(type) {
	case map[string]any:
		return codexGatewayCaptureShapeObjectAt(key, path, typed, redactor)
	case []any:
		items := make([]any, 0, len(typed))
		for i, item := range typed {
			if i >= 32 {
				break
			}
			items = append(items, codexGatewayCaptureShapeValueAt(key, path+"[]", item, redactor))
		}
		return map[string]any{
			"type":   "array",
			"length": len(typed),
			"items":  items,
		}
	case string:
		return codexGatewayCaptureShapeStringAt(key, path, typed, redactor)
	case bool:
		return map[string]any{"type": "bool", "value": typed}
	case float64:
		return map[string]any{"type": "number"}
	case nil:
		return map[string]any{"type": "null"}
	default:
		return map[string]any{"type": codexGatewayCaptureTypeName(value)}
	}
}

func codexGatewayCaptureShapeObject(key string, obj map[string]any, redactor *CodexGatewayCaptureRedactor) map[string]any {
	return codexGatewayCaptureShapeObjectAt(key, key, obj, redactor)
}

func codexGatewayCaptureShapeObjectAt(key, path string, obj map[string]any, redactor *CodexGatewayCaptureRedactor) map[string]any {
	if codexGatewayCaptureIsTextFormatSchemaPath(path) {
		return codexGatewayCaptureSafeJSONSchemaShape(obj, redactor)
	}
	keys := make([]string, 0, len(obj))
	for childKey := range obj {
		keys = append(keys, childKey)
	}
	sort.Strings(keys)
	out := map[string]any{
		"type": "object",
		"keys": keys,
	}
	if kind, ok := obj["type"].(string); ok && codexGatewayCaptureShouldExposeProtocolString(codexGatewayCaptureJoinShapePath(path, "type"), "type", kind) {
		out["protocol_type"] = strings.TrimSpace(kind)
	}
	if model, ok := obj["model"].(string); ok && codexGatewayCaptureShouldExposeProtocolString(codexGatewayCaptureJoinShapePath(path, "model"), "model", model) {
		out["model"] = strings.TrimSpace(model)
	}
	if promptCacheKey, ok := obj["prompt_cache_key"].(string); ok && strings.TrimSpace(promptCacheKey) != "" {
		out["prompt_cache_key"] = codexGatewayCaptureShapeString("prompt_cache_key", promptCacheKey, redactor)
	}
	if instructions, ok := obj["instructions"]; ok {
		out["instructions"] = codexGatewayCaptureInstructionShape(instructions, redactor)
	}
	if tools, ok := obj["tools"].([]any); ok {
		out["tools"] = codexGatewayCaptureToolsShape(tools)
	}
	if input, ok := obj["input"]; ok {
		out["input"] = codexGatewayCaptureInputShape(input, redactor)
	}
	if output, ok := obj["output"]; ok {
		out["output"] = codexGatewayCaptureToolResultShape(output, redactor)
	}
	children := make(map[string]any, len(obj))
	for _, childKey := range keys {
		if _, summarized := out[childKey]; summarized {
			continue
		}
		children[childKey] = codexGatewayCaptureShapeValueAt(childKey, codexGatewayCaptureJoinShapePath(path, childKey), obj[childKey], redactor)
	}
	if len(children) > 0 {
		out["fields"] = children
	}
	return out
}

func codexGatewayCaptureIsTextFormatSchemaPath(path string) bool {
	path = strings.Trim(path, ".")
	return path == "text.format.schema" || strings.HasSuffix(path, ".text.format.schema")
}

func codexGatewayCaptureJoinShapePath(parent, child string) string {
	parent = strings.Trim(parent, ".")
	child = strings.Trim(child, ".")
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	return parent + "." + child
}

func codexGatewayCaptureShapeString(key, value string, redactor *CodexGatewayCaptureRedactor) map[string]any {
	return codexGatewayCaptureShapeStringAt(key, key, value, redactor)
}

func codexGatewayCaptureShapeStringAt(key, path, value string, redactor *CodexGatewayCaptureRedactor) map[string]any {
	shape := map[string]any{
		"type":  "string",
		"chars": len([]rune(value)),
	}
	if redactor != nil {
		shape["hash"] = redactor.HashText(value)
	}
	if codexGatewayCaptureShouldExposeProtocolString(path, key, value) {
		shape["value"] = value
	}
	return shape
}

func codexGatewayCaptureInstructionShape(value any, redactor *CodexGatewayCaptureRedactor) map[string]any {
	source := CodexGatewayCaptureSourceUnknown
	policy := CodexGatewayCaptureContentPolicySummarized
	return map[string]any{
		"source":         source,
		"kind":           "instructions",
		"content_policy": policy,
		"shape":          codexGatewayCaptureShapeValue("instructions", value, redactor),
	}
}

func codexGatewayCaptureToolsShape(tools []any) map[string]any {
	items := make([]any, 0, len(tools))
	for _, tool := range tools {
		obj, ok := tool.(map[string]any)
		if !ok {
			items = append(items, map[string]any{"type": codexGatewayCaptureTypeName(tool)})
			continue
		}
		item := map[string]any{}
		if toolType, ok := obj["type"].(string); ok {
			item["type"] = strings.TrimSpace(toolType)
		}
		if name, ok := obj["name"].(string); ok {
			item["name"] = strings.TrimSpace(name)
		}
		if namespace, ok := obj["namespace"].(string); ok {
			item["namespace"] = strings.TrimSpace(namespace)
		}
		if parameters, ok := obj["parameters"].(map[string]any); ok {
			item["parameters"] = codexGatewayCaptureJSONSchemaShape(parameters)
		}
		items = append(items, item)
	}
	return map[string]any{
		"type":   "array",
		"length": len(tools),
		"items":  items,
	}
}

func codexGatewayCaptureJSONSchemaShape(schema map[string]any) map[string]any {
	out := map[string]any{}
	if typ, ok := schema["type"].(string); ok {
		out["type"] = strings.TrimSpace(typ)
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		fields := make([]string, 0, len(props))
		fieldTypes := make(map[string]string, len(props))
		for name, raw := range props {
			fields = append(fields, name)
			if obj, ok := raw.(map[string]any); ok {
				if typ, ok := obj["type"].(string); ok {
					fieldTypes[name] = strings.TrimSpace(typ)
				}
			}
		}
		sort.Strings(fields)
		out["fields"] = fields
		out["field_types"] = fieldTypes
	}
	if required, ok := schema["required"].([]any); ok {
		values := make([]string, 0, len(required))
		for _, item := range required {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		sort.Strings(values)
		out["required"] = values
	}
	return out
}

func codexGatewayCaptureSafeJSONSchemaShape(schema map[string]any, redactor *CodexGatewayCaptureRedactor) map[string]any {
	out := map[string]any{
		"type":           "json_schema",
		"content_policy": CodexGatewayCaptureContentPolicyProtocolSummary,
	}
	if typ, ok := schema["type"].(string); ok && strings.TrimSpace(typ) != "" {
		out["schema_type"] = strings.TrimSpace(typ)
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		keys := make([]string, 0, len(props))
		hashes := make([]string, 0, len(props))
		known := make([]string, 0)
		fieldTypes := map[string]string{}
		for name, raw := range props {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			keys = append(keys, name)
			if redactor != nil {
				hashes = append(hashes, redactor.CorrelationHash("json_schema_property", name))
			}
			if codexGatewayCaptureAllowedSchemaKey(name) {
				known = append(known, name)
				if obj, ok := raw.(map[string]any); ok {
					if typ, ok := obj["type"].(string); ok && strings.TrimSpace(typ) != "" {
						fieldTypes[name] = strings.TrimSpace(typ)
					}
				}
			}
		}
		sort.Strings(keys)
		sort.Strings(hashes)
		sort.Strings(known)
		out["field_count"] = len(keys)
		out["field_hashes"] = hashes
		out["known_fields"] = known
		if len(fieldTypes) > 0 {
			out["known_field_types"] = fieldTypes
		}
	}
	if required, ok := schema["required"].([]any); ok {
		knownRequired := make([]string, 0)
		for _, item := range required {
			if text, ok := item.(string); ok && codexGatewayCaptureAllowedSchemaKey(text) {
				knownRequired = append(knownRequired, text)
			}
		}
		sort.Strings(knownRequired)
		out["required_count"] = len(required)
		out["known_required"] = knownRequired
	}
	return out
}

func codexGatewayCaptureInputShape(input any, redactor *CodexGatewayCaptureRedactor) any {
	return codexGatewayCaptureShapeValue("input", input, redactor)
}

func codexGatewayCaptureToolResultShape(output any, redactor *CodexGatewayCaptureRedactor) any {
	return codexGatewayCaptureShapeValue("output", output, redactor)
}

func codexGatewayCaptureIsProtocolConstantKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "model", "slug", "upstream_model", "display_name", "provider", "visibility", "type", "role", "status", "object", "finish_reason", "tool_choice", "effort":
		return true
	default:
		return false
	}
}

func codexGatewayCaptureShouldExposeProtocolString(path, key, value string) bool {
	path = strings.Trim(path, ".")
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if !codexGatewayCaptureIsProtocolConstantKey(key) {
		return false
	}
	if !codexGatewayCaptureIsKnownProtocolValue(key, value) {
		return false
	}
	if strings.Contains(path, ".output.") ||
		strings.HasSuffix(path, ".output") ||
		strings.Contains(path, ".arguments.") ||
		strings.Contains(path, ".input.") ||
		strings.HasSuffix(path, ".input") {
		return false
	}
	switch path {
	case "type", "object", "status", "finish_reason", "tool_choice":
		return true
	default:
		return false
	}
}

func codexGatewayCaptureIsKnownProtocolValue(key, value string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	switch key {
	case "type":
		return codexGatewayCaptureKnownProtocolTypes[value]
	case "role":
		return codexGatewayCaptureKnownRoles[value]
	case "status":
		return codexGatewayCaptureKnownStatuses[value]
	case "object":
		return codexGatewayCaptureKnownObjects[value]
	case "finish_reason":
		return codexGatewayCaptureKnownFinishReasons[value]
	case "tool_choice":
		return codexGatewayCaptureKnownToolChoices[value]
	default:
		return false
	}
}

var codexGatewayCaptureKnownProtocolTypes = map[string]bool{
	"message":                                true,
	"input_text":                             true,
	"output_text":                            true,
	"function_call":                          true,
	"function_call_output":                   true,
	"custom_tool_call":                       true,
	"custom_tool_call_output":                true,
	"mcp_tool_call":                          true,
	"mcp_tool_call_output":                   true,
	"reasoning":                              true,
	"response.created":                       true,
	"response.in_progress":                   true,
	"response.completed":                     true,
	"response.failed":                        true,
	"response.output_item.added":             true,
	"response.output_item.done":              true,
	"response.content_part.added":            true,
	"response.content_part.done":             true,
	"response.output_text.delta":             true,
	"response.output_text.done":              true,
	"response.reasoning_text.delta":          true,
	"response.reasoning_text.done":           true,
	"response.function_call_arguments.delta": true,
	"response.function_call_arguments.done":  true,
}

var codexGatewayCaptureKnownRoles = map[string]bool{
	"system":    true,
	"developer": true,
	"user":      true,
	"assistant": true,
	"tool":      true,
}

var codexGatewayCaptureKnownStatuses = map[string]bool{
	"queued":      true,
	"in_progress": true,
	"completed":   true,
	"failed":      true,
	"cancelled":   true,
	"incomplete":  true,
}

var codexGatewayCaptureKnownObjects = map[string]bool{
	"response": true,
	"list":     true,
	"model":    true,
	"error":    true,
}

var codexGatewayCaptureKnownFinishReasons = map[string]bool{
	"stop":           true,
	"length":         true,
	"tool_calls":     true,
	"content_filter": true,
}

var codexGatewayCaptureKnownToolChoices = map[string]bool{
	"auto":     true,
	"none":     true,
	"required": true,
}

func codexGatewayCaptureTypeName(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", value)
	}
}
