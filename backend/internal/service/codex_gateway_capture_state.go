package service

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

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
		received, orphan, linkedTraceIDs := m.registerReceivedToolResults(trace, results)
		m.mergeToolClosure(trace, map[string]any{
			"received_results":  received,
			"duplicate_results": codexGatewayCaptureDuplicateToolResults(results, m.redact),
			"orphan_results":    orphan,
			"linked_trace_ids":  linkedTraceIDs,
		})
	}
	diagnostics := m.codexGatewayCaptureRequestDiagnostics(body)
	if len(diagnostics) > 0 {
		m.mergeRequestDiagnostics(trace, diagnostics)
	}
}

func (m *CodexGatewayCaptureManager) RecordProviderResult(trace *CodexGatewayTrace, result CodexGatewayProviderResult) {
	if !m.enabledTrace(trace) {
		return
	}
	trace.mu.Lock()
	if strings.TrimSpace(result.ResponseID) != "" {
		trace.state.ResponseID = strings.TrimSpace(result.ResponseID)
	}
	if strings.TrimSpace(result.UpstreamRequestID) != "" {
		trace.state.UpstreamRequestID = strings.TrimSpace(result.UpstreamRequestID)
	}
	if strings.TrimSpace(result.UpstreamModel) != "" {
		trace.state.UpstreamModel = strings.TrimSpace(result.UpstreamModel)
	}
	if codexGatewayProviderUsagePresent(result.Usage) {
		trace.state.ProviderUsage = result.Usage
	}
	trace.mu.Unlock()
	calls := m.registerEmittedToolCalls(trace, result.ToolCalls)
	if len(calls) > 0 {
		m.mergeToolClosure(trace, map[string]any{
			"emitted_calls": calls,
		})
	}
	cacheUsage := map[string]any{
		"upstream_model":                 strings.TrimSpace(result.UpstreamModel),
		"input_tokens":                   result.Usage.InputTokens,
		"output_tokens":                  result.Usage.OutputTokens,
		"total_tokens":                   result.Usage.TotalTokens,
		"cache_read_input_tokens":        result.Usage.CacheReadInputTokens,
		"cache_creation_input_tokens":    result.Usage.CacheCreationInputTokens,
		"cache_creation_5m_tokens":       result.Usage.CacheCreation5mTokens,
		"cache_creation_1h_tokens":       result.Usage.CacheCreation1hTokens,
		"provider_usage_extra_available": result.Usage.ProviderUsageExtra != nil,
	}
	if strings.EqualFold(strings.TrimSpace(trace.Meta.Provider), string(CodexGatewayProviderAgnes)) {
		cacheUsage["provider_prompt_cache_status"] = "unsupported"
		cacheUsage["provider_prompt_cache_detail"] = "AGNES upstream usage does not expose prompt cache hit fields"
		cacheUsage["provider_prompt_cache_diagnostics"] = []string{"provider_prompt_cache_unsupported"}
	}
	if len(result.Usage.ProviderUsageExtra) > 0 {
		if value, ok := result.Usage.ProviderUsageExtra["prompt_cache_hit_tokens"]; ok {
			cacheUsage["prompt_cache_hit_tokens"] = value
		}
		if value, ok := result.Usage.ProviderUsageExtra["prompt_cache_miss_tokens"]; ok {
			cacheUsage["prompt_cache_miss_tokens"] = value
		}
	}
	m.mergeCacheUsage(trace, cacheUsage)
}

func (m *CodexGatewayCaptureManager) updateStreamState(trace *CodexGatewayTrace, direction, eventName string) {
	now := time.Now().UTC()
	direction = strings.ToLower(strings.TrimSpace(direction))
	eventName = strings.TrimSpace(eventName)
	trace.mu.Lock()
	defer trace.mu.Unlock()
	switch direction {
	case "upstream":
		trace.state.UpstreamEventCount++
		if trace.state.UpstreamFirstEventAt.IsZero() {
			trace.state.UpstreamFirstEventAt = now
		}
		trace.state.UpstreamLastEventAt = now
		if codexGatewayCaptureIsTerminalEvent(eventName) {
			trace.state.UpstreamTerminalEvent = eventName
		}
	default:
		trace.state.ClientEventCount++
		if trace.state.ClientFirstEventAt.IsZero() {
			trace.state.ClientFirstEventAt = now
		}
		trace.state.ClientLastEventAt = now
		if codexGatewayCaptureIsVisibleOutputEvent(eventName) {
			trace.state.VisibleOutputStarted = true
		}
		if codexGatewayCaptureIsTerminalEvent(eventName) {
			trace.state.ClientTerminalEvent = eventName
		}
	}
}

func codexGatewayCaptureTerminalClassification(state codexGatewayCaptureTraceState, finish CodexGatewayCaptureFinishSummary) string {
	if state.LastError != nil {
		if state.LastError.ErrorClass == codexGatewayCaptureErrorClassCanceledAfterVisibleOutput {
			return "client_canceled_after_visible_output"
		}
		if state.VisibleOutputStarted {
			return "upstream_failed_after_visible_output"
		}
		return "upstream_failed_before_visible_output"
	}
	if strings.TrimSpace(state.ClientTerminalEvent) != "" || strings.TrimSpace(state.UpstreamTerminalEvent) != "" {
		return "completed"
	}
	if strings.EqualFold(strings.TrimSpace(finish.Status), "ok") {
		return "completed"
	}
	return "missing_terminal_event"
}

func codexGatewayCaptureIsTerminalEvent(eventName string) bool {
	switch strings.TrimSpace(eventName) {
	case "response.completed", "response.failed", "response.incomplete", "message_stop", "deepseek.done", "done":
		return true
	default:
		return false
	}
}

func codexGatewayCaptureIsVisibleOutputEvent(eventName string) bool {
	eventName = strings.TrimSpace(eventName)
	return eventName == "response.output_text.delta" ||
		eventName == "response.content_part.added" ||
		strings.Contains(eventName, "output_text")
}

func codexGatewayCaptureSpanMillis(first, last time.Time) int64 {
	if first.IsZero() || last.IsZero() || last.Before(first) {
		return 0
	}
	return last.Sub(first).Milliseconds()
}

func codexGatewayProviderUsagePresent(usage CodexGatewayProviderUsage) bool {
	return usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.TotalTokens != 0 ||
		usage.CacheCreationInputTokens != 0 ||
		usage.CacheReadInputTokens != 0 ||
		usage.CacheCreation5mTokens != 0 ||
		usage.CacheCreation1hTokens != 0 ||
		len(usage.ProviderUsageExtra) > 0
}

func (m *CodexGatewayCaptureManager) mergeCacheUsage(trace *CodexGatewayTrace, values map[string]any) {
	providerCacheUnsupported := codexGatewayCaptureProviderPromptCacheUnsupported(values)
	if !providerCacheUnsupported {
		values = codexGatewayCaptureEnrichCacheUsage(values)
	}
	trace.mu.Lock()
	if trace.cacheUsage == nil {
		trace.cacheUsage = make(map[string]any)
	}
	if providerCacheUnsupported {
		codexGatewayCaptureClearDerivedPromptCacheMetrics(trace.cacheUsage)
	}
	for key, value := range values {
		trace.cacheUsage[key] = value
	}
	snapshot := cloneCaptureMap(trace.cacheUsage)
	trace.mu.Unlock()
	m.writeJSON(trace, "cache_usage.json", snapshot)
}

func codexGatewayCaptureClearDerivedPromptCacheMetrics(cacheUsage map[string]any) {
	if len(cacheUsage) == 0 {
		return
	}
	delete(cacheUsage, "prompt_cache_hit_tokens")
	delete(cacheUsage, "prompt_cache_miss_tokens")
	delete(cacheUsage, "cache_hit_ratio")
	delete(cacheUsage, "cache_hit_rate")
	delete(cacheUsage, "cache_miss_input_tokens")
	delete(cacheUsage, "cache_miss_attribution")
	delete(cacheUsage, "prefix_hash_changed_reason")
}

func codexGatewayCaptureProviderPromptCacheUnsupported(values map[string]any) bool {
	if len(values) == 0 {
		return false
	}
	status := strings.TrimSpace(codexGatewayCaptureStringValue(values["provider_prompt_cache_status"]))
	return strings.EqualFold(status, "unsupported")
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

func codexGatewayCaptureFindToolResults(body []byte) []codexGatewayCaptureToolResult {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil
	}
	results := make([]codexGatewayCaptureToolResult, 0)
	codexGatewayCaptureWalkJSON(value, func(obj map[string]any) {
		typ, _ := obj["type"].(string)
		if typ != "function_call_output" && typ != "custom_tool_call_output" && typ != "mcp_tool_call_output" && typ != "tool_result" {
			return
		}
		if callID, ok := obj["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			callID = strings.TrimSpace(callID)
			results = append(results, codexGatewayCaptureToolResult{
				CallID: callID,
				Kind:   codexGatewayCaptureCallIDKind(callID),
				Chars:  len([]rune(callID)),
			})
		}
	})
	sort.Slice(results, func(i, j int) bool {
		return results[i].CallID < results[j].CallID
	})
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

func codexGatewayCaptureDuplicateToolResults(values []codexGatewayCaptureToolResult, redactor *CodexGatewayCaptureRedactor) []map[string]any {
	seen := make(map[string]int, len(values))
	out := make([]map[string]any, 0)
	for _, value := range values {
		callID := strings.TrimSpace(value.CallID)
		if callID == "" {
			continue
		}
		seen[callID]++
		if seen[callID] == 2 {
			item := map[string]any{
				"call_id_kind":  value.Kind,
				"call_id_chars": value.Chars,
			}
			if redactor != nil {
				item["call_id_hash"] = redactor.CorrelationHash("tool_call_id", callID)
			}
			out = append(out, item)
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
