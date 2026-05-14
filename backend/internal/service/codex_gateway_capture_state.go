package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

func (m *CodexGatewayCaptureManager) recordClientRequestDiagnostics(trace *CodexGatewayTrace, body []byte) {
	if !m.enabledTrace(trace) {
		return
	}
	cacheUsage := map[string]any{
		"request_prefix_hash": m.redact.HashText(codexGatewayCaptureStablePrefix(body)),
	}
	if promptCacheKey := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); promptCacheKey != "" {
		cacheUsage["prompt_cache_key_hash"] = m.redact.HashText(promptCacheKey)
	}
	m.mergeCacheUsage(trace, cacheUsage)

	results := codexGatewayCaptureFindToolResults(body)
	if len(results) > 0 {
		m.mergeToolClosure(trace, map[string]any{
			"received_results":  results,
			"duplicate_results": codexGatewayCaptureDuplicateStrings(results),
		})
	}
}

func (m *CodexGatewayCaptureManager) RecordProviderResult(trace *CodexGatewayTrace, result CodexGatewayProviderResult) {
	if !m.enabledTrace(trace) {
		return
	}
	calls := make([]map[string]any, 0, len(result.ToolCalls))
	for _, call := range result.ToolCalls {
		calls = append(calls, map[string]any{
			"call_id":   strings.TrimSpace(call.ID),
			"tool_name": strings.TrimSpace(call.Name),
			"tool_kind": strings.TrimSpace(call.Type),
			"alias":     strings.TrimSpace(call.Alias),
		})
	}
	m.mergeToolClosure(trace, map[string]any{
		"emitted_calls": calls,
	})
	m.mergeCacheUsage(trace, map[string]any{
		"upstream_model":                 strings.TrimSpace(result.UpstreamModel),
		"input_tokens":                   result.Usage.InputTokens,
		"output_tokens":                  result.Usage.OutputTokens,
		"total_tokens":                   result.Usage.TotalTokens,
		"cache_read_input_tokens":        result.Usage.CacheReadInputTokens,
		"cache_creation_input_tokens":    result.Usage.CacheCreationInputTokens,
		"cache_creation_5m_tokens":       result.Usage.CacheCreation5mTokens,
		"cache_creation_1h_tokens":       result.Usage.CacheCreation1hTokens,
		"provider_usage_extra_available": result.Usage.ProviderUsageExtra != nil,
	})
}

func (m *CodexGatewayCaptureManager) mergeCacheUsage(trace *CodexGatewayTrace, values map[string]any) {
	trace.mu.Lock()
	if trace.cacheUsage == nil {
		trace.cacheUsage = make(map[string]any)
	}
	for key, value := range values {
		trace.cacheUsage[key] = value
	}
	snapshot := cloneCaptureMap(trace.cacheUsage)
	trace.mu.Unlock()
	m.writeJSON(trace, "cache_usage.json", snapshot)
}

func (m *CodexGatewayCaptureManager) mergeToolClosure(trace *CodexGatewayTrace, values map[string]any) {
	trace.mu.Lock()
	if trace.toolClosure == nil {
		trace.toolClosure = make(map[string]any)
	}
	for key, value := range values {
		trace.toolClosure[key] = value
	}
	snapshot := cloneCaptureMap(trace.toolClosure)
	trace.mu.Unlock()
	m.writeJSON(trace, "tool_closure.json", snapshot)
}

func cloneCaptureMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func codexGatewayCaptureFindToolResults(body []byte) []string {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil
	}
	results := make([]string, 0)
	codexGatewayCaptureWalkJSON(value, func(obj map[string]any) {
		typ, _ := obj["type"].(string)
		if typ != "function_call_output" && typ != "tool_result" {
			return
		}
		if callID, ok := obj["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			results = append(results, strings.TrimSpace(callID))
		}
	})
	sort.Strings(results)
	return results
}

func codexGatewayCaptureWalkJSON(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			codexGatewayCaptureWalkJSON(child, visit)
		}
	case []any:
		for _, child := range typed {
			codexGatewayCaptureWalkJSON(child, visit)
		}
	}
}

func codexGatewayCaptureDuplicateStrings(values []string) []string {
	seen := make(map[string]int, len(values))
	out := make([]string, 0)
	for _, value := range values {
		seen[value]++
		if seen[value] == 2 {
			out = append(out, value)
		}
	}
	return out
}

func codexGatewayCaptureStablePrefix(body []byte) string {
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	instructions := strings.TrimSpace(gjson.GetBytes(body, "instructions").Raw)
	tools := strings.TrimSpace(gjson.GetBytes(body, "tools").Raw)
	return model + "\n" + instructions + "\n" + tools
}
