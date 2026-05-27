package service

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (m *CodexGatewayCaptureManager) traceReport(trace *CodexGatewayTrace, summary map[string]any, terminalClassification string, cacheEfficiency map[string]any) map[string]any {
	trace.mu.Lock()
	cacheUsage := cloneCaptureMap(trace.cacheUsage)
	toolClosure := cloneCaptureMap(trace.toolClosure)
	requestDiag := cloneCaptureMap(trace.requestDiag)
	state := trace.state
	trace.mu.Unlock()
	report := map[string]any{
		"trace_id":                trace.ID,
		"model":                   summary["model"],
		"provider":                summary["provider"],
		"status":                  summary["status"],
		"http_status":             summary["http_status"],
		"duration_ms":             summary["duration_ms"],
		"terminal_classification": terminalClassification,
		"cache_usage":             cacheUsage,
		"tool_closure":            toolClosure,
		"request_diagnostics":     requestDiag,
		"stream":                  summary["stream"],
		"upstream_model":          summary["upstream_model"],
	}
	if len(cacheEfficiency) > 0 {
		report["cache_efficiency"] = cacheEfficiency
	}
	if state.UpstreamRequestID != "" {
		report["upstream_request_id_hash"] = m.redact.CorrelationHash("upstream_request_id", state.UpstreamRequestID)
	}
	if state.LastError != nil {
		report["error_class"] = state.LastError.ErrorClass
		report["error_origin"] = state.LastError.Origin
		report["error_stage"] = state.LastError.Stage
	}
	return report
}

func (m *CodexGatewayCaptureManager) writeSessionReport(trace *CodexGatewayTrace, summary map[string]any, terminalClassification string, cacheEfficiency map[string]any) {
	if !m.enabledTrace(trace) {
		return
	}
	trace.mu.Lock()
	cacheRead := trace.cacheUsage["cache_read_input_tokens"]
	state := trace.state
	requestDiag := cloneCaptureMap(trace.requestDiag)
	trace.mu.Unlock()
	record := map[string]any{
		"schema_version":          1,
		"trace_id":                trace.ID,
		"ts":                      time.Now().UTC().Format(time.RFC3339Nano),
		"model":                   summary["model"],
		"provider":                summary["provider"],
		"status":                  summary["status"],
		"duration_ms":             summary["duration_ms"],
		"terminal_classification": terminalClassification,
		"cache_read_input_tokens": cacheRead,
		"visible_output_started":  state.VisibleOutputStarted,
	}
	if len(cacheEfficiency) > 0 {
		record["prompt_cache_hit_tokens"] = cacheEfficiency["prompt_cache_hit_tokens"]
		record["prompt_cache_miss_tokens"] = cacheEfficiency["prompt_cache_miss_tokens"]
		record["cache_hit_ratio"] = cacheEfficiency["cache_hit_ratio"]
		record["cache_hit_rate"] = cacheEfficiency["cache_hit_rate"]
		record["cache_miss_input_tokens"] = cacheEfficiency["cache_miss_input_tokens"]
		record["cache_diagnostics"] = cacheEfficiency["diagnostics"]
		if missAttribution, ok := cacheEfficiency["miss_attribution"]; ok {
			record["cache_miss_attribution"] = missAttribution
		}
	}
	if trace.Meta.ThreadID != "" {
		record["thread_id_hash"] = m.redact.CorrelationHash("thread_id", trace.Meta.ThreadID)
	}
	if trace.Meta.SessionID != "" {
		record["session_id_hash"] = m.redact.CorrelationHash("session_id", trace.Meta.SessionID)
	}
	if background, ok := requestDiag["desktop_background_task"]; ok {
		record["desktop_background_task"] = background
	}
	if state.LastError != nil {
		record["error_class"] = state.LastError.ErrorClass
	}
	m.writeJSONLAtPath(trace, filepath.Join(filepath.Dir(trace.Dir), "session_report.jsonl"), record)
}

func (m *CodexGatewayCaptureManager) writeJSONLAtPath(trace *CodexGatewayTrace, path string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		trace.dropped.Add(1)
		return
	}
	payload = append(payload, '\n')
	m.enqueue(trace, func() {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			trace.dropped.Add(1)
			return
		}
		defer f.Close()
		n, err := io.WriteString(f, string(payload))
		if err != nil || n != len(payload) {
			trace.dropped.Add(1)
		}
	})
}

func codexGatewayCaptureCacheEfficiency(cacheUsage map[string]any, provider any) map[string]any {
	inputTokens, inputOK := codexGatewayCaptureIntValue(cacheUsage["input_tokens"])
	hitTokens, missTokens, hitRate, ok := codexGatewayCapturePromptCacheMetrics(cacheUsage)
	if !inputOK || !ok || inputTokens <= 0 {
		return nil
	}
	diagnostics := make([]string, 0, 2)
	if inputTokens > 1000 && hitTokens == 0 {
		diagnostics = append(diagnostics, "cache_cold_or_prefix_changed")
	}
	if inputTokens > 1000 && hitRate < 0.5 {
		diagnostics = append(diagnostics, "low_cache_hit_rate")
		if strings.EqualFold(strings.TrimSpace(codexGatewayCaptureStringValue(provider)), string(CodexGatewayProviderDeepSeek)) {
			diagnostics = append(diagnostics, "deepseek_account_or_prefix_changed")
		}
	}
	out := map[string]any{
		"input_tokens":             inputTokens,
		"prompt_cache_hit_tokens":  hitTokens,
		"prompt_cache_miss_tokens": missTokens,
		"cache_miss_input_tokens":  missTokens,
		"cache_hit_ratio":          hitRate,
		"cache_hit_rate":           hitRate,
		"diagnostics":              diagnostics,
	}
	if cacheReadTokens, readOK := codexGatewayCaptureIntValue(cacheUsage["cache_read_input_tokens"]); readOK {
		out["cache_read_input_tokens"] = cacheReadTokens
	}
	return out
}

func codexGatewayCaptureEnrichCacheUsage(cacheUsage map[string]any) map[string]any {
	hitTokens, missTokens, hitRate, ok := codexGatewayCapturePromptCacheMetrics(cacheUsage)
	if !ok {
		return cacheUsage
	}
	cacheUsage["prompt_cache_hit_tokens"] = hitTokens
	cacheUsage["prompt_cache_miss_tokens"] = missTokens
	cacheUsage["cache_hit_ratio"] = hitRate
	return cacheUsage
}

func codexGatewayCapturePromptCacheMetrics(cacheUsage map[string]any) (int, int, float64, bool) {
	inputTokens, inputOK := codexGatewayCaptureIntValue(cacheUsage["input_tokens"])
	if hitTokens, hitOK := codexGatewayCaptureIntValue(cacheUsage["prompt_cache_hit_tokens"]); hitOK {
		if missTokens, missOK := codexGatewayCaptureIntValue(cacheUsage["prompt_cache_miss_tokens"]); missOK {
			total := hitTokens + missTokens
			if total > 0 {
				return hitTokens, missTokens, float64(hitTokens) / float64(total), true
			}
		}
		if inputOK && inputTokens > 0 && hitTokens <= inputTokens {
			missTokens := inputTokens - hitTokens
			return hitTokens, missTokens, float64(hitTokens) / float64(inputTokens), true
		}
	}
	if missTokens, missOK := codexGatewayCaptureIntValue(cacheUsage["prompt_cache_miss_tokens"]); missOK && inputOK && inputTokens > 0 && missTokens <= inputTokens {
		hitTokens := inputTokens - missTokens
		return hitTokens, missTokens, float64(hitTokens) / float64(inputTokens), true
	}
	cacheReadTokens, readOK := codexGatewayCaptureIntValue(cacheUsage["cache_read_input_tokens"])
	if !inputOK || !readOK || inputTokens <= 0 {
		return 0, 0, 0, false
	}
	if cacheReadTokens < 0 {
		cacheReadTokens = 0
	}
	missTokens := inputTokens - cacheReadTokens
	if missTokens < 0 {
		missTokens = 0
	}
	return cacheReadTokens, missTokens, float64(cacheReadTokens) / float64(inputTokens), true
}

func codexGatewayCaptureIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func codexGatewayCaptureStringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
