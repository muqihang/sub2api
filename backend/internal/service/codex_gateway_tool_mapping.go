package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var codexGatewayToolSafeNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const codexGatewayNamespacePathSeparator = "\x1f"

var codexGatewayHostedResponsesToolTypes = map[string]struct{}{
	"computer_use_preview": {},
	"file_search":          {},
	"image_generation":     {},
	"web_search":           {},
}

type codexGatewayToolMappingRecord struct {
	alias string
	entry CodexGatewayToolNameMapEntry
	tool  map[string]any
}

func BuildCodexGatewayToolMapping(raw json.RawMessage, cfg CodexGatewayToolMappingConfig) (CodexGatewayToolMappingResult, error) {
	result := CodexGatewayToolMappingResult{
		NameMap:         make(map[string]CodexGatewayToolNameMapEntry),
		originalToAlias: make(map[string]string),
	}
	if len(raw) == 0 {
		return result, nil
	}

	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return CodexGatewayToolMappingResult{}, fmt.Errorf("decode tools: %w", err)
	}

	var flattened []map[string]any
	for _, tool := range tools {
		records, ignored, err := flattenCodexGatewayTool(tool, "", "", cfg)
		if err != nil {
			return CodexGatewayToolMappingResult{}, err
		}
		result.IgnoredHostedToolTypes = append(result.IgnoredHostedToolTypes, ignored...)
		for _, record := range records {
			if existing, ok := result.NameMap[record.alias]; ok {
				if existing != record.entry {
					return CodexGatewayToolMappingResult{}, fmt.Errorf("tool alias collision for %q", record.alias)
				}
				return CodexGatewayToolMappingResult{}, fmt.Errorf("duplicate tool alias %q", record.alias)
			}
			result.NameMap[record.alias] = record.entry
			result.originalToAlias[toolMappingOriginalKey(record.entry.Kind, record.entry.NamespacePath, record.entry.Name)] = record.alias
			flattened = append(flattened, recordToDeepSeekTool(record, cfg))
		}
	}
	result.Tools = flattened
	return result, nil
}

func flattenCodexGatewayTool(raw any, namespacePrefix, parentKind string, cfg CodexGatewayToolMappingConfig) ([]codexGatewayToolMappingRecord, []string, error) {
	tool, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("tool definition must be an object")
	}

	toolType := strings.TrimSpace(firstCodexGatewayToolString(tool["type"]))
	if toolType == "" {
		toolType = CodexGatewayToolKindFunction
	}
	if isCodexGatewayHostedResponsesToolType(toolType) {
		record, err := flattenCodexGatewayHostedTool(tool, toolType)
		if err != nil {
			return nil, nil, err
		}
		return []codexGatewayToolMappingRecord{record}, nil, nil
	}

	switch toolType {
	case CodexGatewayToolKindNamespace:
		return flattenCodexGatewayNamespaceTool(tool, namespacePrefix, cfg)
	case CodexGatewayToolKindCustom:
		record, err := flattenCodexGatewayCustomTool(tool, namespacePrefix, cfg)
		if err != nil {
			return nil, nil, err
		}
		return []codexGatewayToolMappingRecord{record}, nil, nil
	default:
		record, err := flattenCodexGatewayFunctionTool(tool, namespacePrefix, parentKind, cfg)
		if err != nil {
			return nil, nil, err
		}
		return []codexGatewayToolMappingRecord{record}, nil, nil
	}
}

func flattenCodexGatewayHostedTool(tool map[string]any, toolType string) (codexGatewayToolMappingRecord, error) {
	name := normalizeCodexGatewayHostedToolName(toolType)
	if override := strings.TrimSpace(firstCodexGatewayToolString(tool["name"])); override != "" && strings.HasPrefix(name, "web_search") {
		name = "web_search"
	}
	alias := sanitizeCodexGatewayToolName(name)
	if alias == "" {
		return codexGatewayToolMappingRecord{}, fmt.Errorf("hosted tool %q produced empty alias", toolType)
	}
	function := map[string]any{
		"name":        alias,
		"description": codexGatewayHostedToolDescription(alias),
		"parameters":  codexGatewayHostedToolParameters(alias),
	}
	return codexGatewayToolMappingRecord{
		alias: alias,
		entry: CodexGatewayToolNameMapEntry{
			Alias: alias,
			Kind:  CodexGatewayToolKindHosted,
			Name:  alias,
		},
		tool: map[string]any{
			"type":     "function",
			"function": function,
		},
	}, nil
}

func flattenCodexGatewayNamespaceTool(tool map[string]any, parentNamespace string, cfg CodexGatewayToolMappingConfig) ([]codexGatewayToolMappingRecord, []string, error) {
	namespace := strings.TrimSpace(firstCodexGatewayToolString(tool["name"]))
	if namespace == "" {
		namespace = strings.TrimSpace(firstCodexGatewayToolString(tool["namespace"]))
	}
	if namespace == "" {
		return nil, nil, fmt.Errorf("namespace tool requires a name")
	}
	namespace = appendCodexGatewayNamespacePath(parentNamespace, namespace)

	var nested []any
	if toolsRaw, ok := tool["tools"]; ok {
		if arr, ok := toolsRaw.([]any); ok {
			nested = arr
		}
	}
	if len(nested) == 0 {
		return nil, nil, fmt.Errorf("namespace tool %q requires nested tools", namespace)
	}

	var out []codexGatewayToolMappingRecord
	var ignored []string
	for _, child := range nested {
		records, childIgnored, err := flattenCodexGatewayTool(child, namespace, CodexGatewayToolKindNamespace, cfg)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, records...)
		ignored = append(ignored, childIgnored...)
	}
	return out, ignored, nil
}

func flattenCodexGatewayFunctionTool(tool map[string]any, namespacePrefix, parentKind string, cfg CodexGatewayToolMappingConfig) (codexGatewayToolMappingRecord, error) {
	fn := tool
	if functionRaw, ok := tool["function"]; ok {
		if function, ok := functionRaw.(map[string]any); ok && function != nil {
			fn = function
		}
	}
	name := strings.TrimSpace(firstCodexGatewayToolString(fn["name"]))
	if name == "" {
		return codexGatewayToolMappingRecord{}, fmt.Errorf("function tool requires a name")
	}
	alias := buildCodexGatewayToolAlias(namespacePrefix, name)
	if alias == "" {
		return codexGatewayToolMappingRecord{}, fmt.Errorf("function tool %q produced empty alias", name)
	}
	params := firstCodexGatewayToolValue(fn["parameters"], tool["parameters"], fn["input_schema"], tool["input_schema"])
	strict, strictSet := codexGatewayToolStrictValue(fn, tool)
	schema, err := prepareCodexGatewayToolSchema(params, strictSet && strict, cfg)
	if err != nil {
		return codexGatewayToolMappingRecord{}, err
	}
	function := map[string]any{
		"name": alias,
	}
	if desc := strings.TrimSpace(firstCodexGatewayToolString(fn["description"], tool["description"])); desc != "" {
		function["description"] = desc
	}
	if schema != nil {
		function["parameters"] = schema
	}
	if strictSet && cfg.EnableStrictBeta {
		function["strict"] = strict
	}
	kind := CodexGatewayToolKindFunction
	if namespacePrefix != "" || parentKind == CodexGatewayToolKindNamespace {
		kind = CodexGatewayToolKindNamespace
	}
	return codexGatewayToolMappingRecord{
		alias: alias,
		entry: CodexGatewayToolNameMapEntry{
			Alias:         alias,
			Kind:          kind,
			Namespace:     codexGatewayNamespaceDisplay(namespacePrefix),
			NamespacePath: namespacePrefix,
			Name:          name,
		},
		tool: map[string]any{
			"type":     "function",
			"function": function,
		},
	}, nil
}

func flattenCodexGatewayCustomTool(tool map[string]any, namespacePrefix string, cfg CodexGatewayToolMappingConfig) (codexGatewayToolMappingRecord, error) {
	name := strings.TrimSpace(firstCodexGatewayToolString(tool["name"]))
	if name == "" {
		name = "custom"
	}
	alias := buildCodexGatewayToolAlias(namespacePrefix, "custom", name)
	spec, _ := tool["custom"].(map[string]any)
	desc := strings.TrimSpace(firstCodexGatewayToolString(spec["description"], tool["description"]))
	format := firstCodexGatewayToolValue(spec["format"], tool["format"])
	desc = codexGatewayCustomToolDescription(desc, format)
	params := firstCodexGatewayToolValue(spec["input_schema"], tool["input_schema"])
	if params == nil && format != nil {
		params = codexGatewayCustomFormatInputSchema(format)
	}
	strict, strictSet := codexGatewayToolStrictValue(spec, tool)
	schema, err := prepareCodexGatewayToolSchema(params, strictSet && strict, cfg)
	if err != nil {
		return codexGatewayToolMappingRecord{}, err
	}
	function := map[string]any{
		"name": alias,
	}
	if desc != "" {
		function["description"] = desc
	}
	if schema != nil {
		function["parameters"] = schema
	}
	if strictSet && cfg.EnableStrictBeta {
		function["strict"] = strict
	}
	return codexGatewayToolMappingRecord{
		alias: alias,
		entry: CodexGatewayToolNameMapEntry{
			Alias:         alias,
			Kind:          CodexGatewayToolKindCustom,
			Namespace:     codexGatewayNamespaceDisplay(namespacePrefix),
			NamespacePath: namespacePrefix,
			Name:          name,
		},
		tool: map[string]any{
			"type":     "function",
			"function": function,
		},
	}, nil
}

func codexGatewayCustomToolDescription(desc string, format any) string {
	if format == nil {
		return desc
	}
	notes := "For this Codex custom tool, call the function with a JSON object containing string field \"input\". The input field must contain the exact raw input for the custom tool; the gateway unwraps it before invoking Codex."
	if formatDesc := codexGatewayCustomFormatDescription(format); formatDesc != "" {
		notes += "\nCustom tool format:\n" + formatDesc
	}
	if strings.TrimSpace(desc) == "" {
		return notes
	}
	return strings.TrimSpace(desc) + "\n\n" + notes
}

func codexGatewayCustomFormatInputSchema(format any) map[string]any {
	inputDescription := "Exact raw freeform payload for the custom tool. Put the full custom-tool input here as a string; do not nest it under another key."
	if formatDesc := codexGatewayCustomFormatDescription(format); formatDesc != "" {
		inputDescription += "\n\nExpected format:\n" + formatDesc
	}
	return map[string]any{
		"type":     "object",
		"required": []any{"input"},
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": inputDescription,
			},
		},
		"additionalProperties": false,
	}
}

func codexGatewayCustomFormatDescription(format any) string {
	formatMap, ok := format.(map[string]any)
	if !ok {
		if raw, err := json.Marshal(format); err == nil {
			return string(raw)
		}
		return ""
	}
	parts := make([]string, 0, 3)
	if typ := strings.TrimSpace(firstCodexGatewayToolString(formatMap["type"])); typ != "" {
		parts = append(parts, "type: "+typ)
	}
	if syntax := strings.TrimSpace(firstCodexGatewayToolString(formatMap["syntax"])); syntax != "" {
		parts = append(parts, "syntax: "+syntax)
	}
	if definition := strings.TrimSpace(firstCodexGatewayToolString(formatMap["definition"])); definition != "" {
		parts = append(parts, definition)
	}
	return strings.Join(parts, "\n")
}

func recordToDeepSeekTool(record codexGatewayToolMappingRecord, cfg CodexGatewayToolMappingConfig) map[string]any {
	if record.tool == nil {
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": record.alias,
			},
		}
	}
	return record.tool
}

func isCodexGatewayHostedResponsesToolType(toolType string) bool {
	normalized := strings.TrimSpace(toolType)
	if strings.HasPrefix(normalized, "web_search") {
		return true
	}
	_, ok := codexGatewayHostedResponsesToolTypes[normalized]
	return ok
}

func normalizeCodexGatewayHostedToolName(toolType string) string {
	normalized := strings.TrimSpace(toolType)
	switch {
	case normalized == "google_search" || strings.HasPrefix(normalized, "web_search"):
		return "web_search"
	case normalized == "computer_use_preview":
		return "computer_use_preview"
	case normalized == "file_search":
		return "file_search"
	case normalized == "image_generation":
		return "image_generation"
	default:
		return normalized
	}
}

func codexGatewayHostedToolDescription(name string) string {
	switch name {
	case "web_search":
		return "Search the web for current information."
	case "computer_use_preview":
		return "Use the computer environment when the client provides computer-use capability."
	case "file_search":
		return "Search indexed files when the client provides file-search capability."
	case "image_generation":
		return "Generate or edit images when the client provides image-generation capability."
	default:
		return "Use hosted tool capability " + name + " when available."
	}
}

func codexGatewayHostedToolParameters(name string) map[string]any {
	switch name {
	case "web_search":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		}
	default:
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": true,
		}
	}
}

func codexGatewayIsServerHandledHostedTool(kind, name string) bool {
	return strings.TrimSpace(kind) == CodexGatewayToolKindHosted && strings.TrimSpace(name) == "web_search"
}

func toolMappingOriginalKey(kind, namespace, name string) string {
	return kind + "|" + namespace + "|" + name
}

func firstCodexGatewayToolString(values ...any) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		if s, ok := value.(string); ok {
			if strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func firstCodexGatewayToolValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func codexGatewayToolStrictValue(tool, fallback map[string]any) (bool, bool) {
	for _, src := range []map[string]any{tool, fallback} {
		if src == nil {
			continue
		}
		if strict, ok := src["strict"].(bool); ok {
			return strict, true
		}
	}
	return false, false
}

func sanitizeCodexGatewayToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	segments := strings.Split(name, "__")
	safeSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		safe := sanitizeCodexGatewayToolSegment(segment)
		if safe == "" {
			sum := sha256.Sum256([]byte(segment))
			safe = "seg_" + hex.EncodeToString(sum[:4])
		}
		safeSegments = append(safeSegments, safe)
	}
	cleaned := strings.Trim(strings.Join(safeSegments, "__"), "_-")
	if cleaned == "" {
		cleaned = "tool"
	}
	if len(cleaned) <= 64 && codexGatewayToolSafeNameRe.MatchString(cleaned) {
		return cleaned
	}
	sum := sha256.Sum256([]byte(cleaned))
	suffix := hex.EncodeToString(sum[:6])
	trimLen := 64 - 1 - len(suffix)
	if trimLen < 1 {
		trimLen = 1
	}
	prefix := strings.Trim(cleaned[:min(len(cleaned), trimLen)], "_-")
	if prefix == "" {
		prefix = "tool"
	}
	return prefix + "_" + suffix
}

func appendCodexGatewayNamespacePath(prefix, segment string) string {
	if prefix == "" {
		return segment
	}
	return prefix + codexGatewayNamespacePathSeparator + segment
}

func codexGatewayNamespacePathSegments(prefix string) []string {
	if strings.TrimSpace(prefix) == "" {
		return nil
	}
	return strings.Split(prefix, codexGatewayNamespacePathSeparator)
}

func codexGatewayNamespaceDisplay(prefix string) string {
	parts := codexGatewayNamespacePathSegments(prefix)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "__")
}

func buildCodexGatewayToolAlias(namespacePrefix string, parts ...string) string {
	rawParts := make([]string, 0, len(parts)+len(codexGatewayNamespacePathSegments(namespacePrefix)))
	rawParts = append(rawParts, codexGatewayNamespacePathSegments(namespacePrefix)...)
	rawParts = append(rawParts, parts...)

	safeParts := make([]string, 0, len(rawParts))
	for _, raw := range rawParts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		safeParts = append(safeParts, codexGatewayToolAliasPart(raw))
	}
	return finalizeCodexGatewayAlias(strings.Join(safeParts, "__"))
}

func codexGatewayToolAliasPart(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "__") {
		sum := sha256.Sum256([]byte(raw))
		readable := sanitizeCodexGatewayToolSegment(strings.ReplaceAll(raw, "__", "_"))
		if readable == "" {
			return "seg_" + hex.EncodeToString(sum[:4])
		}
		return readable + "_" + hex.EncodeToString(sum[:4])
	}
	safe := sanitizeCodexGatewayToolSegment(raw)
	if safe == "" {
		sum := sha256.Sum256([]byte(raw))
		return "seg_" + hex.EncodeToString(sum[:4])
	}
	return safe
}

func finalizeCodexGatewayAlias(cleaned string) string {
	cleaned = strings.Trim(cleaned, "_-")
	if cleaned == "" {
		cleaned = "tool"
	}
	if len(cleaned) <= 64 && codexGatewayToolSafeNameRe.MatchString(cleaned) {
		return cleaned
	}
	sum := sha256.Sum256([]byte(cleaned))
	suffix := hex.EncodeToString(sum[:6])
	trimLen := 64 - 1 - len(suffix)
	if trimLen < 1 {
		trimLen = 1
	}
	prefix := strings.Trim(cleaned[:min(len(cleaned), trimLen)], "_-")
	if prefix == "" {
		prefix = "tool"
	}
	return prefix + "_" + suffix
}

func sanitizeCodexGatewayToolSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range segment {
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_-")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeCodexGatewayToolSchema(schema any, cfg CodexGatewayToolMappingConfig) (any, error) {
	if schema == nil {
		return nil, nil
	}
	normalized, unsupported := stripUnsupportedCodexGatewaySchemaConstraints(schema)
	if cfg.RejectUnsupportedStrictSchemas && unsupported {
		return nil, fmt.Errorf("unsupported strict tool schema constraints")
	}
	return normalized, nil
}

func prepareCodexGatewayToolSchema(schema any, strict bool, cfg CodexGatewayToolMappingConfig) (any, error) {
	if !strict || !cfg.EnableStrictBeta {
		return schema, nil
	}
	return sanitizeCodexGatewayToolSchema(schema, cfg)
}

var codexGatewayUnsupportedSchemaKeys = map[string]struct{}{
	"oneOf":                 {},
	"allOf":                 {},
	"not":                   {},
	"if":                    {},
	"then":                  {},
	"else":                  {},
	"patternProperties":     {},
	"dependentSchemas":      {},
	"unevaluatedProperties": {},
	"unevaluatedItems":      {},
	"minLength":             {},
	"maxLength":             {},
	"minItems":              {},
	"maxItems":              {},
}

func stripUnsupportedCodexGatewaySchemaConstraints(value any) (any, bool) {
	return stripUnsupportedCodexGatewaySchemaConstraintsWithContext(value, false)
}

func stripUnsupportedCodexGatewaySchemaConstraintsWithContext(value any, propertyContainer bool) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		unsupported := false
		for key, item := range typed {
			if propertyContainer {
				next, childUnsupported := stripUnsupportedCodexGatewaySchemaConstraintsWithContext(item, false)
				if childUnsupported {
					unsupported = true
				}
				out[key] = next
				continue
			}
			if key == "properties" {
				next, childUnsupported := stripUnsupportedCodexGatewaySchemaConstraintsWithContext(item, true)
				if childUnsupported {
					unsupported = true
				}
				out[key] = next
				continue
			}
			if _, drop := codexGatewayUnsupportedSchemaKeys[key]; drop {
				unsupported = true
				continue
			}
			next, childUnsupported := stripUnsupportedCodexGatewaySchemaConstraintsWithContext(item, false)
			if childUnsupported {
				unsupported = true
			}
			out[key] = next
		}
		return out, unsupported
	case []any:
		out := make([]any, len(typed))
		unsupported := false
		for i, item := range typed {
			next, childUnsupported := stripUnsupportedCodexGatewaySchemaConstraintsWithContext(item, false)
			if childUnsupported {
				unsupported = true
			}
			out[i] = next
		}
		return out, unsupported
	default:
		return value, false
	}
}
