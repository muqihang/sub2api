package service

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

func (m *CodexGatewayCaptureManager) traceReport(trace *CodexGatewayTrace, summary map[string]any, terminalClassification string) map[string]any {
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

func (m *CodexGatewayCaptureManager) writeSessionReport(trace *CodexGatewayTrace, summary map[string]any, terminalClassification string) {
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
