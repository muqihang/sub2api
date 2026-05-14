package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

func (m *CodexGatewayCaptureManager) codexGatewayCaptureRequestDiagnostics(body []byte) map[string]any {
	if m == nil || m.redact == nil {
		return nil
	}
	out := map[string]any{}
	if background := m.codexGatewayCaptureDesktopBackgroundTaskDiagnostics(body); len(background) > 0 {
		out["desktop_background_task"] = background
	}
	return out
}

func (m *CodexGatewayCaptureManager) codexGatewayCaptureDesktopBackgroundTaskDiagnostics(body []byte) map[string]any {
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	format := gjson.GetBytes(body, "text.format")
	formatType := strings.TrimSpace(format.Get("type").String())
	formatName := strings.TrimSpace(format.Get("name").String())
	schema := format.Get("schema")
	if !strings.EqualFold(model, "gpt-5.4-mini") && !strings.EqualFold(formatType, "json_schema") {
		return nil
	}
	out := map[string]any{
		"detected":            true,
		"model":               model,
		"text_format_type":    formatType,
		"reasoning_effort":    strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String()),
		"parallel_tool_calls": gjson.GetBytes(body, "parallel_tool_calls").Bool(),
	}
	if formatName != "" {
		out["text_format_name_hash"] = m.redact.CorrelationHash("text_format_name", formatName)
	}
	keys := codexGatewayCaptureJSONSchemaPropertyKeys(schema.Raw)
	if len(keys) > 0 {
		hashes := make([]string, 0, len(keys))
		known := make([]string, 0, len(keys))
		for _, key := range keys {
			hashes = append(hashes, m.redact.CorrelationHash("json_schema_property", key))
			if codexGatewayCaptureAllowedSchemaKey(key) {
				known = append(known, key)
			}
		}
		sort.Strings(hashes)
		sort.Strings(known)
		out["text_format_schema_key_hashes"] = hashes
		out["text_format_schema_key_count"] = len(keys)
		out["text_format_schema_known_keys"] = known
	}
	required := codexGatewayCaptureJSONSchemaRequiredKeys(schema.Raw)
	if len(required) > 0 {
		knownRequired := make([]string, 0, len(required))
		for _, key := range required {
			if codexGatewayCaptureAllowedSchemaKey(key) {
				knownRequired = append(knownRequired, key)
			}
		}
		sort.Strings(knownRequired)
		out["text_format_required_known_keys"] = knownRequired
		out["text_format_required_key_count"] = len(required)
	}
	return out
}

func codexGatewayCaptureJSONSchemaPropertyKeys(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil
	}
	props, _ := schema["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for key := range props {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func codexGatewayCaptureJSONSchemaRequiredKeys(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil
	}
	required, _ := schema["required"].([]any)
	keys := make([]string, 0, len(required))
	for _, item := range required {
		if key, ok := item.(string); ok && strings.TrimSpace(key) != "" {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	sort.Strings(keys)
	return keys
}

func codexGatewayCaptureAllowedSchemaKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "title":
		return true
	default:
		return false
	}
}
