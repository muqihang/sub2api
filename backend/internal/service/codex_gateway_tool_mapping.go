package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var codexGatewayToolSafeNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const codexGatewayNamespacePathSeparator = "\x1f"
const codexGatewayToolSearchType = "tool_search"

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

	var records []codexGatewayToolMappingRecord
	for _, tool := range tools {
		flattened, ignored, err := flattenCodexGatewayTool(tool, "", "", cfg)
		if err != nil {
			return CodexGatewayToolMappingResult{}, err
		}
		result.IgnoredHostedToolTypes = append(result.IgnoredHostedToolTypes, ignored...)
		for _, record := range flattened {
			if existing, ok := result.NameMap[record.alias]; ok {
				if !codexGatewayToolNameMapEntriesEqual(existing, record.entry) {
					return CodexGatewayToolMappingResult{}, fmt.Errorf("tool alias collision for %q", record.alias)
				}
				return CodexGatewayToolMappingResult{}, fmt.Errorf("duplicate tool alias %q", record.alias)
			}
			result.NameMap[record.alias] = record.entry
			result.originalToAlias[toolMappingOriginalKey(record.entry.Kind, record.entry.NamespacePath, record.entry.Name)] = record.alias
			records = append(records, record)
		}
	}
	sortCodexGatewayToolMappingRecords(records)
	flattened := make([]map[string]any, 0, len(records))
	for _, record := range records {
		flattened = append(flattened, recordToDeepSeekTool(record, cfg))
	}
	result.Tools = flattened
	return result, nil
}

func sortCodexGatewayToolMappingRecords(records []codexGatewayToolMappingRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		left := records[i].entry
		right := records[j].entry
		for _, pair := range [][2]string{
			{left.Kind, right.Kind},
			{left.NamespacePath, right.NamespacePath},
			{left.Name, right.Name},
			{left.Alias, right.Alias},
		} {
			if pair[0] == pair[1] {
				continue
			}
			return pair[0] < pair[1]
		}
		return false
	})
}

func codexGatewayToolNameMapEntriesEqual(a, b CodexGatewayToolNameMapEntry) bool {
	return a.Alias == b.Alias &&
		a.Kind == b.Kind &&
		a.Namespace == b.Namespace &&
		a.NamespacePath == b.NamespacePath &&
		a.Name == b.Name
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
	case codexGatewayToolSearchType:
		record, err := flattenCodexGatewayToolSearchTool(tool, cfg)
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

func flattenCodexGatewayToolSearchTool(tool map[string]any, cfg CodexGatewayToolMappingConfig) (codexGatewayToolMappingRecord, error) {
	alias := codexGatewayToolSearchType
	params := firstCodexGatewayToolValue(tool["parameters"], tool["input_schema"])
	if params == nil {
		params = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			},
			"required": []any{"query"},
		}
	}
	schema, err := prepareCodexGatewayToolSchema(params, false, cfg)
	if err != nil {
		return codexGatewayToolMappingRecord{}, err
	}
	function := map[string]any{
		"name":        alias,
		"description": codexGatewayToolSearchDescription(tool["description"]),
	}
	if schema != nil {
		function["parameters"] = schema
	}
	return codexGatewayToolMappingRecord{
		alias: alias,
		entry: CodexGatewayToolNameMapEntry{
			Alias: alias,
			Kind:  CodexGatewayToolKindFunction,
			Name:  alias,
		},
		tool: map[string]any{
			"type":     "function",
			"function": function,
		},
	}, nil
}

func codexGatewayToolSearchDescription(raw any) string {
	desc := strings.TrimSpace(firstCodexGatewayToolString(raw))
	if desc != "" {
		return desc
	}
	return "Search available deferred tool metadata and return matching tools for the next model turn."
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
		return canonicalizeCodexGatewayToolSchema(schema), nil
	}
	normalized, err := sanitizeCodexGatewayToolSchema(schema, cfg)
	if err != nil {
		return nil, err
	}
	return canonicalizeCodexGatewayToolSchema(normalized), nil
}

func canonicalizeCodexGatewayToolSchema(value any) any {
	return canonicalizeCodexGatewayToolSchemaWithContext(value, false)
}

func canonicalizeCodexGatewayToolSchemaWithContext(value any, propertyContainer bool) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if propertyContainer {
				if child == nil {
					out[key] = map[string]any{}
				} else {
					out[key] = canonicalizeCodexGatewayToolSchemaWithContext(child, false)
				}
				continue
			}
			if key == "properties" {
				out[key] = canonicalizeCodexGatewayToolSchemaWithContext(child, true)
				continue
			}
			if key == "required" {
				if required, ok := normalizeCodexGatewayRequiredSchemaKeys(child); ok {
					out[key] = required
				}
				continue
			}
			if key == "dependentRequired" {
				if dependent, ok := normalizeCodexGatewayDependentRequiredSchemaKeys(child); ok {
					out[key] = dependent
				}
				continue
			}
			out[key] = canonicalizeCodexGatewayToolSchemaWithContext(child, false)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, canonicalizeCodexGatewayToolSchemaWithContext(child, false))
		}
		return out
	default:
		return value
	}
}

func normalizeCodexGatewayRequiredSchemaKeys(value any) ([]any, bool) {
	var out []any
	switch items := value.(type) {
	case []any:
		out = make([]any, 0, len(items))
		for _, item := range items {
			if key, ok := item.(string); ok && strings.TrimSpace(key) != "" {
				out = append(out, key)
			}
		}
	case []string:
		out = make([]any, 0, len(items))
		for _, item := range items {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
	default:
		return nil, false
	}
	if len(out) == 0 {
		return nil, false
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, leftOK := out[i].(string)
		right, rightOK := out[j].(string)
		if !leftOK || !rightOK {
			return false
		}
		return left < right
	})
	return out, true
}

func normalizeCodexGatewayDependentRequiredSchemaKeys(value any) (map[string]any, bool) {
	items, ok := value.(map[string]any)
	if !ok || len(items) == 0 {
		return nil, false
	}
	out := make(map[string]any, len(items))
	for key, raw := range items {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if required, ok := normalizeCodexGatewayRequiredSchemaKeys(raw); ok {
			out[key] = required
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func codexGatewayDeepSeekAdaptToolMapping(mapping CodexGatewayToolMappingResult, cfg CodexGatewayToolMappingConfig) CodexGatewayToolMappingResult {
	if len(mapping.Tools) == 0 || len(mapping.NameMap) == 0 {
		return mapping
	}
	for _, tool := range mapping.Tools {
		function, _ := tool["function"].(map[string]any)
		if function == nil {
			continue
		}
		alias := strings.TrimSpace(firstCodexGatewayToolString(function["name"]))
		if alias == "" {
			continue
		}
		entry, ok := mapping.NameMap[alias]
		if !ok {
			continue
		}
		if desc := codexGatewayDeepSeekToolDescription(function["description"], entry); desc != "" {
			function["description"] = desc
		}
		if schema, ok := function["parameters"].(map[string]any); ok && schema != nil {
			if flattened, paths, changed := codexGatewayDeepSeekFlattenToolParameters(schema, cfg); changed {
				function["parameters"] = flattened
				entry.FlattenedArgs = paths
			}
			if params, ok := function["parameters"].(map[string]any); ok {
				codexGatewayDeepSeekEnhanceToolParameters(params, entry)
			}
		}
		if _, strictSet := function["strict"]; strictSet && !codexGatewayDeepSeekStrictEligible(function, entry) {
			delete(function, "strict")
		}
		mapping.NameMap[alias] = entry
	}
	return mapping
}

func codexGatewayDeepSeekStrictEligible(function map[string]any, entry CodexGatewayToolNameMapEntry) bool {
	if function == nil || entry.Kind != CodexGatewayToolKindFunction || len(entry.FlattenedArgs) > 0 {
		return false
	}
	schema, _ := function["parameters"].(map[string]any)
	if schema == nil || !strings.EqualFold(strings.TrimSpace(firstCodexGatewayToolString(schema["type"])), "object") {
		return false
	}
	if additional, ok := schema["additionalProperties"].(bool); ok && additional {
		return false
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 || len(properties) > 16 {
		return false
	}
	for _, value := range properties {
		prop, ok := value.(map[string]any)
		if !ok || !codexGatewayDeepSeekStrictEligibleProperty(prop) {
			return false
		}
	}
	return true
}

func codexGatewayDeepSeekStrictEligibleProperty(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	if _, ok := schema["properties"]; ok {
		return false
	}
	if _, ok := schema["items"]; ok {
		return false
	}
	typ := strings.TrimSpace(firstCodexGatewayToolString(schema["type"]))
	switch typ {
	case "string", "number", "integer", "boolean":
		return true
	default:
		return false
	}
}

func codexGatewayDeepSeekToolDescription(raw any, entry CodexGatewayToolNameMapEntry) string {
	desc := strings.TrimSpace(firstCodexGatewayToolString(raw))
	extras := make([]string, 0, 4)
	if codexGatewayDeepSeekIsShellLikeTool(entry) {
		extras = append(extras,
			"Put the full shell command in `cmd`.",
			"For multi-line file creation or rewrites, prefer `python3 - <<'PY' ... PY` when it is safer than many small edits.",
		)
	}
	if codexGatewayDeepSeekIsPythonTool(entry) {
		extras = append(extras, "For multi-line file creation or rewrites, prefer `python3 - <<'PY' ... PY` when it is safer than many small edits.")
	}
	if codexGatewayDeepSeekIsApplyPatchTool(entry) {
		extras = append(extras, "Put the exact raw patch text in the custom input field. Do not wrap it in extra prose or another object.")
	}
	extras = append(extras, codexGatewayDeepSeekComputerUseToolDescriptionExtras(entry)...)
	extras = uniqueCodexGatewayStrings(extras)
	if len(extras) == 0 {
		return desc
	}
	if desc == "" {
		return strings.Join(extras, "\n")
	}
	return desc + "\n\n" + strings.Join(extras, "\n")
}

func codexGatewayDeepSeekComputerUseToolDescriptionExtras(entry CodexGatewayToolNameMapEntry) []string {
	if !codexGatewayDeepSeekIsComputerUseToolIdentity(
		entry.Name,
		entry.Alias,
		firstCodexGatewayToolString(entry.Namespace, entry.NamespacePath),
	) {
		return nil
	}
	switch strings.TrimSpace(entry.Name) {
	case "list_apps":
		return []string{"Use list_apps to discover the app bundle identifier; pass the bundle identifier to later Computer Use calls instead of localized display names."}
	case "get_app_state":
		return []string{"Prefer bundle identifier app values from list_apps over localized display names. Read visible_text and operable_lines before relying on screenshots, and refresh with get_app_state once if an element_index becomes stale."}
	case "set_value":
		return []string{"For Electron/chat apps, prefer set_value on the current settable text input from get_app_state, then call press_key Return and get_app_state to confirm/send/read the result."}
	case "press_key":
		return []string{"After set_value in chat-style apps, press_key Return is the preferred send action before a follow-up get_app_state."}
	case "type_text":
		return []string{"For Electron/chat inputs, prefer set_value when a settable element_index is available; use type_text only when set_value is not suitable."}
	case "click":
		return []string{"Avoid blind clicking. Prefer set_value for text inputs; if an element_index is stale, refresh with get_app_state and retry the new element_index once."}
	case "scroll":
		return []string{"Avoid scrolling unless visible_text or operable_lines show the needed reply/control is off-screen; read visible_text after get_app_state first."}
	default:
		return []string{"For Computer Use, prefer bundle identifier app values and operate from current get_app_state visible_text/operable_lines rather than guessing from screenshots."}
	}
}

func codexGatewayDeepSeekEnhanceToolParameters(schema map[string]any, entry CodexGatewayToolNameMapEntry) {
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return
	}
	if codexGatewayDeepSeekIsShellLikeTool(entry) {
		if prop, ok := properties["cmd"].(map[string]any); ok {
			prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Put the full shell command here.")
		}
	}
	if codexGatewayDeepSeekIsApplyPatchTool(entry) {
		if prop, ok := properties["input"].(map[string]any); ok {
			prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Put the exact raw patch text in this custom input field.")
		}
	}
	if codexGatewayDeepSeekIsComputerUseToolIdentity(entry.Name, entry.Alias, firstCodexGatewayToolString(entry.Namespace, entry.NamespacePath)) {
		codexGatewayDeepSeekEnhanceComputerUseToolParameters(properties, entry)
	}
}

func codexGatewayDeepSeekEnhanceComputerUseToolParameters(properties map[string]any, entry CodexGatewayToolNameMapEntry) {
	if prop, ok := properties["app"].(map[string]any); ok {
		prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Prefer a bundle identifier from list_apps; localized display names may be invalid app values.")
	}
	if prop, ok := properties["element_index"].(map[string]any); ok {
		prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Use the current element_index from the latest get_app_state; if stale, call get_app_state again and retry once.")
	}
	switch strings.TrimSpace(entry.Name) {
	case "press_key":
		if prop, ok := properties["key"].(map[string]any); ok {
			prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Use Return after set_value to send chat-style messages when the input is focused.")
		}
	case "set_value":
		if prop, ok := properties["value"].(map[string]any); ok {
			prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Put the full intended input text here before press_key Return.")
		}
	case "type_text":
		if prop, ok := properties["text"].(map[string]any); ok {
			prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Use only when set_value is not suitable for the current settable input.")
		}
	case "scroll":
		if prop, ok := properties["direction"].(map[string]any); ok {
			prop["description"] = codexGatewayAppendToolDescription(prop["description"], "Scroll only after visible_text/operable_lines indicate the needed content is off-screen.")
		}
	}
}

func codexGatewayAppendToolDescription(raw any, extra string) string {
	base := strings.TrimSpace(firstCodexGatewayToolString(raw))
	extra = strings.TrimSpace(extra)
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	case strings.Contains(base, extra):
		return base
	default:
		return base + " " + extra
	}
}

type codexGatewayDeepSeekSchemaLeaf struct {
	FlatKey  string
	Path     []string
	Schema   map[string]any
	Required bool
}

func codexGatewayDeepSeekFlattenToolParameters(schema map[string]any, cfg CodexGatewayToolMappingConfig) (map[string]any, []CodexGatewayToolArgumentPath, bool) {
	if !cfg.EnableDeepSeekSchemaFlattening {
		return schema, nil, false
	}
	if schema == nil || !strings.EqualFold(strings.TrimSpace(firstCodexGatewayToolString(schema["type"])), "object") {
		return schema, nil, false
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return schema, nil, false
	}

	leaves := make([]codexGatewayDeepSeekSchemaLeaf, 0, len(properties))
	maxDepth := 0
	nestedLeaf := false
	rootRequired := codexGatewayToolSchemaRequiredSet(schema["required"])
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		child, ok := properties[key].(map[string]any)
		if !ok || child == nil {
			continue
		}
		codexGatewayDeepSeekCollectSchemaLeaves([]string{key}, child, rootRequired[key], &leaves, &maxDepth, &nestedLeaf)
	}
	minDepth := cfg.DeepSeekFlattenMinDepth
	if minDepth <= 0 {
		minDepth = 3
	}
	minLeaves := cfg.DeepSeekFlattenMinLeaves
	if minLeaves <= 0 {
		minLeaves = 4
	}
	if !nestedLeaf || (maxDepth < minDepth && len(leaves) < minLeaves) {
		return schema, nil, false
	}

	flattened := make(map[string]any, len(schema))
	for key, value := range schema {
		if key == "properties" || key == "required" {
			continue
		}
		flattened[key] = value
	}
	propertiesOut := make(map[string]any, len(leaves))
	requiredOut := make([]any, 0, len(leaves))
	paths := make([]CodexGatewayToolArgumentPath, 0, len(leaves))
	seenFlatKeys := make(map[string]struct{}, len(leaves))
	for _, leaf := range leaves {
		if _, exists := seenFlatKeys[leaf.FlatKey]; exists {
			return schema, nil, false
		}
		seenFlatKeys[leaf.FlatKey] = struct{}{}
		propertiesOut[leaf.FlatKey] = leaf.Schema
		if leaf.Required {
			requiredOut = append(requiredOut, leaf.FlatKey)
		}
		paths = append(paths, CodexGatewayToolArgumentPath{
			FlatKey: leaf.FlatKey,
			Path:    append([]string(nil), leaf.Path...),
		})
	}
	flattened["type"] = "object"
	flattened["properties"] = propertiesOut
	if len(requiredOut) > 0 {
		flattened["required"] = requiredOut
	}
	return flattened, paths, true
}

func codexGatewayDeepSeekCollectSchemaLeaves(path []string, schema map[string]any, required bool, leaves *[]codexGatewayDeepSeekSchemaLeaf, maxDepth *int, nestedLeaf *bool) {
	if len(path) > *maxDepth {
		*maxDepth = len(path)
	}
	if strings.EqualFold(strings.TrimSpace(firstCodexGatewayToolString(schema["type"])), "object") {
		properties, _ := schema["properties"].(map[string]any)
		if len(properties) > 0 {
			requiredSet := codexGatewayToolSchemaRequiredSet(schema["required"])
			keys := make([]string, 0, len(properties))
			for key := range properties {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child, ok := properties[key].(map[string]any)
				if !ok || child == nil {
					continue
				}
				nextPath := append(append([]string(nil), path...), key)
				codexGatewayDeepSeekCollectSchemaLeaves(nextPath, child, required && requiredSet[key], leaves, maxDepth, nestedLeaf)
			}
			return
		}
	}
	flatKey := strings.Join(path, ".")
	if len(path) > 1 {
		*nestedLeaf = true
	}
	*leaves = append(*leaves, codexGatewayDeepSeekSchemaLeaf{
		FlatKey:  flatKey,
		Path:     append([]string(nil), path...),
		Schema:   schema,
		Required: required,
	})
}

func codexGatewayToolSchemaRequiredSet(raw any) map[string]bool {
	required := make(map[string]bool)
	values, _ := raw.([]any)
	for _, value := range values {
		if name, ok := value.(string); ok && strings.TrimSpace(name) != "" {
			required[strings.TrimSpace(name)] = true
		}
	}
	return required
}

func codexGatewayPrepareDeepSeekToolArguments(entry CodexGatewayToolNameMapEntry, raw string) (string, bool, string) {
	trimmed := strings.TrimSpace(raw)
	if entry.Kind == CodexGatewayToolKindCustom {
		trimmed = codexGatewayNormalizeLiteralNewlinesInJSONStrings(trimmed)
		if trimmed != "" && strings.HasPrefix(trimmed, "{") && !json.Valid([]byte(trimmed)) {
			if repaired, ok := codexGatewayRepairTruncatedJSON(trimmed); ok {
				if codexGatewayDeepSeekIsApplyPatchTool(entry) {
					if strings.TrimSpace(codexGatewayDeepSeekCustomToolInput(repaired, entry)) == "" {
						return "", false, "malformed_tool_arguments"
					}
					return repaired, true, ""
				}
				if codexGatewayDeepSeekIsMutatingTool(entry) {
					return "", false, "malformed_tool_arguments"
				}
				return repaired, true, ""
			}
			return "", false, "malformed_tool_arguments"
		}
		return raw, true, ""
	}

	normalized := normalizeCodexGatewayToolArguments(raw)
	if !json.Valid([]byte(normalized)) {
		if repaired, ok := codexGatewayRepairTruncatedJSON(normalized); ok {
			if codexGatewayDeepSeekIsMutatingTool(entry) {
				return "", false, "malformed_tool_arguments"
			}
			normalized = repaired
		} else {
			return "", false, "malformed_tool_arguments"
		}
	}
	if next, changed := codexGatewayDeepSeekUnflattenToolArguments(normalized, entry); changed {
		normalized = next
	}
	if next, changed := codexGatewayNormalizeFunctionToolArguments(codexGatewayClientVisibleToolName(entry), normalized); changed {
		normalized = next
	}
	return normalized, true, ""
}

func codexGatewayDeepSeekUnflattenToolArguments(raw string, entry CodexGatewayToolNameMapEntry) (string, bool) {
	if len(entry.FlattenedArgs) == 0 || strings.TrimSpace(raw) == "" || !json.Valid([]byte(raw)) {
		return raw, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload == nil {
		return raw, false
	}
	changed := false
	for _, path := range entry.FlattenedArgs {
		if len(path.Path) == 0 || strings.TrimSpace(path.FlatKey) == "" {
			continue
		}
		value, ok := payload[path.FlatKey]
		if !ok {
			continue
		}
		delete(payload, path.FlatKey)
		codexGatewaySetNestedToolArgument(payload, path.Path, value)
		changed = true
	}
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return string(encoded), true
}

func codexGatewaySetNestedToolArgument(root map[string]any, path []string, value any) {
	current := root
	for i, segment := range path {
		if i == len(path)-1 {
			current[segment] = value
			return
		}
		next, _ := current[segment].(map[string]any)
		if next == nil {
			next = make(map[string]any)
			current[segment] = next
		}
		current = next
	}
}

func codexGatewayRepairTruncatedJSON(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || json.Valid([]byte(raw)) {
		return raw, false
	}
	stack := make([]rune, 0, 8)
	inString := false
	escape := false
	for _, r := range raw {
		if inString {
			if escape {
				escape = false
				continue
			}
			switch r {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != r {
				return raw, false
			}
			stack = stack[:len(stack)-1]
		}
	}
	var suffix strings.Builder
	if inString {
		suffix.WriteRune('"')
	}
	for i := len(stack) - 1; i >= 0; i-- {
		suffix.WriteRune(stack[i])
	}
	if suffix.Len() == 0 {
		return raw, false
	}
	candidate := raw + suffix.String()
	if !json.Valid([]byte(candidate)) {
		return raw, false
	}
	return candidate, true
}

func codexGatewayDeepSeekDangerousToolCallKey(entry CodexGatewayToolNameMapEntry, arguments string) string {
	name := strings.TrimSpace(codexGatewayClientVisibleToolName(entry))
	if name == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(entry.Namespace)) + "|" + strings.ToLower(name) + "|" + strings.TrimSpace(arguments)
}

func codexGatewayDeepSeekIsMutatingTool(entry CodexGatewayToolNameMapEntry) bool {
	name := strings.ToLower(strings.TrimSpace(codexGatewayClientVisibleToolName(entry)))
	namespace := strings.ToLower(strings.TrimSpace(entry.Namespace))
	alias := strings.ToLower(strings.TrimSpace(entry.Alias))
	switch {
	case codexGatewayDeepSeekIsApplyPatchTool(entry):
		return true
	case codexGatewayDeepSeekIsShellLikeTool(entry):
		return true
	case name == "python", strings.Contains(alias, "python"):
		return true
	case name == "exec_command", strings.Contains(alias, "exec_command"):
		return true
	case namespace == "shell" && name == "exec":
		return true
	default:
		return false
	}
}

func codexGatewayDeepSeekIsShellLikeTool(entry CodexGatewayToolNameMapEntry) bool {
	name := strings.ToLower(strings.TrimSpace(codexGatewayClientVisibleToolName(entry)))
	namespace := strings.ToLower(strings.TrimSpace(entry.Namespace))
	alias := strings.ToLower(strings.TrimSpace(entry.Alias))
	return name == "exec_command" || (namespace == "shell" && name == "exec") || strings.Contains(alias, "shell__exec")
}

func codexGatewayDeepSeekIsPythonTool(entry CodexGatewayToolNameMapEntry) bool {
	name := strings.ToLower(strings.TrimSpace(codexGatewayClientVisibleToolName(entry)))
	alias := strings.ToLower(strings.TrimSpace(entry.Alias))
	return name == "python" || strings.Contains(alias, "python")
}

func codexGatewayDeepSeekIsApplyPatchTool(entry CodexGatewayToolNameMapEntry) bool {
	name := strings.ToLower(strings.TrimSpace(codexGatewayClientVisibleToolName(entry)))
	alias := strings.ToLower(strings.TrimSpace(entry.Alias))
	return name == "apply_patch" || name == "edit" || strings.Contains(alias, "apply_patch")
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
