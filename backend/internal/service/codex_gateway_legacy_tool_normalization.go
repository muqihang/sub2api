package service

import (
	"encoding/json"
	"strings"
)

func normalizeCodexGatewayLegacyToolRefs(req *CodexGatewayResponsesCreateRequest) error {
	if req == nil {
		return nil
	}

	if normalized, modified, err := normalizeCodexGatewayLegacyToolDefsRaw(req.Tools); err != nil {
		return err
	} else if modified {
		req.Tools = normalized
		if req.RawFields != nil {
			req.RawFields["tools"] = cloneCodexGatewayRawJSON(normalized)
		}
	}

	if normalized, modified, err := normalizeCodexGatewayLegacyToolChoiceRaw(req.ToolChoice); err != nil {
		return err
	} else if modified {
		req.ToolChoice = normalized
		if req.RawFields != nil {
			req.RawFields["tool_choice"] = cloneCodexGatewayRawJSON(normalized)
		}
	}

	if normalized, modified, err := normalizeCodexGatewayLegacyInputRaw(req.Input); err != nil {
		return err
	} else if modified {
		req.Input = normalized
		if req.RawFields != nil {
			req.RawFields["input"] = cloneCodexGatewayRawJSON(normalized)
		}
	}

	if normalized, modified, err := normalizeCodexGatewayToolChoiceForAvailableTools(req.ToolChoice, req.Tools); err != nil {
		return err
	} else if modified {
		req.ToolChoice = normalized
		if req.RawFields != nil {
			if len(normalized) == 0 {
				delete(req.RawFields, "tool_choice")
			} else {
				req.RawFields["tool_choice"] = cloneCodexGatewayRawJSON(normalized)
			}
		}
	}

	return nil
}

func normalizeCodexGatewayLegacyToolDefsRaw(raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, err
	}
	modified := normalizeCodexGatewayLegacyToolDefsValue(parsed)
	if !modified {
		return nil, false, nil
	}
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func normalizeCodexGatewayLegacyToolChoiceRaw(raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, err
	}
	if typed, ok := parsed.(string); ok {
		normalized := codexGatewayCanonicalToolName(typed)
		if normalized == typed {
			return nil, false, nil
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return nil, false, err
		}
		return encoded, true, nil
	}
	modified := normalizeCodexGatewayLegacyToolChoiceValue(parsed)
	if !modified {
		return nil, false, nil
	}
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func normalizeCodexGatewayLegacyInputRaw(raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, err
	}
	modified := normalizeCodexGatewayLegacyInputValue(parsed)
	if !modified {
		return nil, false, nil
	}
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func normalizeCodexGatewayLegacyToolDefsValue(value any) bool {
	tools, ok := value.([]any)
	if !ok {
		return false
	}
	modified := false
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		if normalizeCodexGatewayLegacyToolDef(tool) {
			modified = true
		}
	}
	return modified
}

func normalizeCodexGatewayLegacyToolDef(tool map[string]any) bool {
	modified := false
	toolType := firstNonEmptyString(tool["type"])
	if toolType == CodexGatewayToolKindNamespace {
		if nested, ok := tool["tools"].([]any); ok {
			for _, rawNested := range nested {
				nestedTool, ok := rawNested.(map[string]any)
				if !ok {
					continue
				}
				if normalizeCodexGatewayLegacyToolDef(nestedTool) {
					modified = true
				}
			}
		}
	}

	if name := firstNonEmptyString(tool["name"]); name != "" {
		if normalized := codexGatewayCanonicalToolName(name); normalized != name {
			tool["name"] = normalized
			modified = true
		}
	}
	if fn, ok := tool["function"].(map[string]any); ok {
		if name := firstNonEmptyString(fn["name"]); name != "" {
			if normalized := codexGatewayCanonicalToolName(name); normalized != name {
				fn["name"] = normalized
				modified = true
			}
		}
	}
	return modified
}

func normalizeCodexGatewayLegacyToolChoiceValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		modified := false
		if name := firstNonEmptyString(typed["name"]); name != "" {
			if normalized := codexGatewayCanonicalToolName(name); normalized != name {
				typed["name"] = normalized
				modified = true
			}
		}
		if fn, ok := typed["function"].(map[string]any); ok {
			if name := firstNonEmptyString(fn["name"]); name != "" {
				if normalized := codexGatewayCanonicalToolName(name); normalized != name {
					fn["name"] = normalized
					modified = true
				}
			}
		}
		return modified
	default:
		return false
	}
}

func normalizeCodexGatewayLegacyInputValue(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	modified := false
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		switch firstNonEmptyString(item["type"]) {
		case "function_call", "custom_tool_call":
			if name := firstNonEmptyString(item["name"]); name != "" {
				if normalized := codexGatewayCanonicalToolName(name); normalized != name {
					item["name"] = normalized
					modified = true
				}
			}
		}
	}
	return modified
}

func codexGatewayCanonicalToolName(name string) string {
	trimmed := firstNonEmptyString(name)
	if trimmed == "" {
		return trimmed
	}
	if normalized, ok := codexToolNameMapping[trimmed]; ok {
		return normalized
	}
	return trimmed
}

func codexGatewayClientVisibleToolName(entry CodexGatewayToolNameMapEntry) string {
	name := strings.TrimSpace(entry.Name)
	if entry.Kind == CodexGatewayToolKindCustom && name == "edit" {
		return "apply_patch"
	}
	return name
}

func codexGatewayClientVisibleToolItemType(entry CodexGatewayToolNameMapEntry) string {
	if entry.Kind == CodexGatewayToolKindCustom {
		return CodexGatewayOutputItemTypeCustomToolCall
	}
	return CodexGatewayOutputItemTypeFunctionCall
}

func codexGatewayIsToolSearchEntry(entry CodexGatewayToolNameMapEntry) bool {
	if entry.Kind == CodexGatewayToolKindCustom {
		return false
	}
	if strings.TrimSpace(entry.Alias) == codexGatewayToolSearchType {
		return true
	}
	return strings.TrimSpace(entry.Name) == codexGatewayToolSearchType &&
		strings.TrimSpace(entry.Namespace) == "" &&
		strings.TrimSpace(entry.NamespacePath) == ""
}

func codexGatewayIsClientVisibleLocalShellTool(entry CodexGatewayToolNameMapEntry) bool {
	return codexGatewayClientVisibleToolItemType(entry) == CodexGatewayOutputItemTypeLocalShellCall
}

func normalizeCodexGatewayToolChoiceForAvailableTools(toolChoiceRaw, toolsRaw json.RawMessage) (json.RawMessage, bool, error) {
	if len(toolChoiceRaw) == 0 {
		return nil, false, nil
	}

	var parsed any
	if err := json.Unmarshal(toolChoiceRaw, &parsed); err != nil {
		return nil, false, err
	}

	if keep, err := codexGatewayToolChoiceMatchesAvailableTools(parsed, toolsRaw); err != nil {
		return nil, false, err
	} else if keep {
		return nil, false, nil
	}

	return json.RawMessage("null"), true, nil
}

func codexGatewayToolChoiceMatchesAvailableTools(choice any, toolsRaw json.RawMessage) (bool, error) {
	switch typed := choice.(type) {
	case string:
		normalized := strings.TrimSpace(strings.ToLower(typed))
		switch normalized {
		case "", "auto", "none", "required":
			return true, nil
		default:
			return codexGatewayStaleToolChoiceShouldBeKept(typed, toolsRaw)
		}
	case map[string]any:
		choiceType := strings.TrimSpace(strings.ToLower(firstNonEmptyString(typed["type"])))
		switch choiceType {
		case "", "auto", "none", "required", "any":
			return true, nil
		case "function", CodexGatewayToolKindCustom, CodexGatewayToolKindNamespace:
			name := strings.TrimSpace(firstNonEmptyString(typed["name"]))
			if name == "" {
				if fn, ok := typed["function"].(map[string]any); ok {
					name = strings.TrimSpace(firstNonEmptyString(fn["name"]))
				}
			}
			if name == "" {
				return len(toolsRaw) != 0, nil
			}
			return codexGatewayStaleToolChoiceShouldBeKept(name, toolsRaw)
		default:
			return len(toolsRaw) != 0, nil
		}
	default:
		return len(toolsRaw) != 0, nil
	}
}

func codexGatewayStaleToolChoiceShouldBeKept(name string, toolsRaw json.RawMessage) (bool, error) {
	matched, err := codexGatewayToolChoiceNameMatchesTools(name, toolsRaw)
	if err != nil || matched {
		return matched, err
	}
	if !codexGatewayIsLegacyToolResidualName(name) {
		return true, nil
	}
	return false, nil
}

func codexGatewayToolChoiceNameMatchesTools(name string, toolsRaw json.RawMessage) (bool, error) {
	available, err := codexGatewayAvailableToolNames(toolsRaw)
	if err != nil {
		return false, err
	}
	for _, candidate := range codexGatewayToolChoiceCandidateNames(name) {
		if _, ok := available[candidate]; ok {
			return true, nil
		}
	}
	return false, nil
}

func codexGatewayAvailableToolNames(raw json.RawMessage) (map[string]struct{}, error) {
	available := make(map[string]struct{})
	if len(raw) == 0 {
		return available, nil
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if tools, ok := parsed.([]any); ok {
		for _, tool := range tools {
			codexGatewayCollectAvailableToolNames(tool, available)
		}
	}
	return available, nil
}

func codexGatewayCollectAvailableToolNames(value any, available map[string]struct{}) {
	tool, ok := value.(map[string]any)
	if !ok {
		return
	}
	codexGatewayAddToolNameCandidates(available, firstNonEmptyString(tool["name"]))
	if fn, ok := tool["function"].(map[string]any); ok {
		codexGatewayAddToolNameCandidates(available, firstNonEmptyString(fn["name"]))
	}
	if nested, ok := tool["tools"].([]any); ok {
		for _, child := range nested {
			codexGatewayCollectAvailableToolNames(child, available)
		}
	}
}

func codexGatewayAddToolNameCandidates(available map[string]struct{}, name string) {
	for _, candidate := range codexGatewayToolChoiceCandidateNames(name) {
		available[candidate] = struct{}{}
	}
}

func codexGatewayToolChoiceCandidateNames(name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}
	candidates := []string{
		trimmed,
		codexGatewayCanonicalToolName(trimmed),
		strings.ReplaceAll(trimmed, ".", "__"),
		codexGatewayCanonicalToolName(strings.ReplaceAll(trimmed, ".", "__")),
	}
	if strings.HasPrefix(trimmed, "custom__") {
		candidates = append(candidates, strings.TrimPrefix(trimmed, "custom__"))
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func codexGatewayIsLegacyToolResidualName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	if _, ok := codexToolNameMapping[trimmed]; ok {
		return true
	}
	for _, mapped := range codexToolNameMapping {
		if trimmed == mapped || trimmed == "custom__"+mapped {
			return true
		}
	}
	return false
}

func codexGatewayFallbackLegacyCustomToolAlias(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", false
	}
	baseName := strings.TrimPrefix(trimmed, "custom__")
	normalized := codexGatewayCanonicalToolName(baseName)
	if normalized == "" {
		return "", false
	}
	if !codexGatewayIsLegacyToolResidualName(trimmed) &&
		!codexGatewayIsLegacyToolResidualName(baseName) &&
		!codexGatewayIsLegacyToolResidualName(normalized) {
		return "", false
	}
	return buildCodexGatewayToolAlias("", "custom", normalized), true
}
