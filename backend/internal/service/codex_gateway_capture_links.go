package service

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type codexGatewayCaptureToolLink struct {
	TraceID    string
	Trace      *CodexGatewayTrace
	CallIDHash string
	CallKind   string
	CallChars  int
	ToolName   string
	ToolKind   string
	Model      string
}

type codexGatewayCaptureToolResult struct {
	CallID string
	Kind   string
	Chars  int
}

func (m *CodexGatewayCaptureManager) registerEmittedToolCalls(trace *CodexGatewayTrace, calls []CodexGatewayStoredToolCall) []map[string]any {
	if !m.enabledTrace(trace) || len(calls) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			continue
		}
		callIDHash := m.redact.CorrelationHash("tool_call_id", callID)
		record := map[string]any{
			"call_id_hash":  callIDHash,
			"call_id_kind":  codexGatewayCaptureCallIDKind(callID),
			"call_id_chars": len([]rune(callID)),
			"tool_name":     strings.TrimSpace(call.Name),
			"tool_kind":     strings.TrimSpace(call.Type),
			"alias":         strings.TrimSpace(call.Alias),
			"status":        "emitted_pending",
		}
		out = append(out, record)
		link := codexGatewayCaptureToolLink{
			TraceID:    trace.ID,
			Trace:      trace,
			CallIDHash: callIDHash,
			CallKind:   codexGatewayCaptureCallIDKind(callID),
			CallChars:  len([]rune(callID)),
			ToolName:   strings.TrimSpace(call.Name),
			ToolKind:   strings.TrimSpace(call.Type),
			Model:      strings.TrimSpace(trace.Meta.Model),
		}
		m.linksMu.Lock()
		m.emittedCallBy[callIDHash] = link
		m.linksMu.Unlock()
		m.writeTraceIndex(trace, "emitted", link, "")
	}
	return out
}

func (m *CodexGatewayCaptureManager) registerReceivedToolResults(trace *CodexGatewayTrace, results []codexGatewayCaptureToolResult) ([]map[string]any, []map[string]any, []string) {
	if !m.enabledTrace(trace) || len(results) == 0 {
		return nil, nil, nil
	}
	received := make([]map[string]any, 0, len(results))
	orphan := make([]map[string]any, 0)
	linkedTraceIDs := make([]string, 0)
	linkedSeen := make(map[string]struct{})
	for _, result := range results {
		callID := strings.TrimSpace(result.CallID)
		if callID == "" {
			continue
		}
		callIDHash := m.redact.CorrelationHash("tool_call_id", callID)
		record := map[string]any{
			"call_id_hash":  callIDHash,
			"call_id_kind":  result.Kind,
			"call_id_chars": result.Chars,
			"status":        "result_received",
		}
		received = append(received, record)
		m.linksMu.Lock()
		link, ok := m.emittedCallBy[callIDHash]
		m.linksMu.Unlock()
		if ok {
			record["linked_trace_id"] = link.TraceID
			if _, exists := linkedSeen[link.TraceID]; !exists {
				linkedSeen[link.TraceID] = struct{}{}
				linkedTraceIDs = append(linkedTraceIDs, link.TraceID)
			}
			m.markEmittedToolCallResultReceived(link.Trace, callIDHash, trace.ID)
			m.writeTraceIndex(trace, "received", link, link.TraceID)
			continue
		}
		record["status"] = "orphan_result"
		orphan = append(orphan, record)
		m.writeTraceIndex(trace, "received", codexGatewayCaptureToolLink{
			TraceID:    trace.ID,
			Trace:      trace,
			CallIDHash: callIDHash,
			CallKind:   result.Kind,
			CallChars:  result.Chars,
			Model:      strings.TrimSpace(trace.Meta.Model),
		}, "")
	}
	sort.Strings(linkedTraceIDs)
	return received, orphan, linkedTraceIDs
}

func (m *CodexGatewayCaptureManager) markEmittedToolCallResultReceived(trace *CodexGatewayTrace, callIDHash, linkedTraceID string) {
	if !m.enabledTrace(trace) {
		return
	}
	trace.mu.Lock()
	calls, _ := trace.toolClosure["emitted_calls"].([]map[string]any)
	if len(calls) == 0 {
		if raw, ok := trace.toolClosure["emitted_calls"].([]any); ok {
			calls = make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				if obj, ok := item.(map[string]any); ok {
					calls = append(calls, obj)
				}
			}
		}
	}
	for _, call := range calls {
		if strings.TrimSpace(asCaptureString(call["call_id_hash"])) == callIDHash {
			call["status"] = "result_received"
			call["linked_result_trace_id"] = linkedTraceID
		}
	}
	trace.toolClosure["emitted_calls"] = calls
	linked := codexGatewayCaptureStringSlice(trace.toolClosure["linked_trace_ids"])
	linked = appendUniqueCaptureString(linked, linkedTraceID)
	trace.toolClosure["linked_trace_ids"] = linked
	snapshot := cloneCaptureMap(trace.toolClosure)
	trace.mu.Unlock()
	m.writeJSON(trace, "tool_closure.json", snapshot)
}

func (m *CodexGatewayCaptureManager) writeTraceIndex(trace *CodexGatewayTrace, direction string, link codexGatewayCaptureToolLink, linkedTraceID string) {
	if !m.enabledTrace(trace) {
		return
	}
	record := map[string]any{
		"schema_version": 1,
		"ts":             time.Now().UTC().Format(time.RFC3339Nano),
		"trace_id":       trace.ID,
		"direction":      strings.TrimSpace(direction),
		"call_id_hash":   link.CallIDHash,
		"call_id_kind":   link.CallKind,
		"call_id_chars":  link.CallChars,
		"tool_name":      link.ToolName,
		"tool_kind":      link.ToolKind,
		"model":          firstCaptureNonEmpty(link.Model, trace.Meta.Model),
		"thread_id_hash": m.redact.CorrelationHash("thread_id", trace.Meta.ThreadID),
	}
	if strings.TrimSpace(linkedTraceID) != "" {
		record["linked_trace_id"] = strings.TrimSpace(linkedTraceID)
	}
	m.writeJSONLAtPath(trace, filepath.Join(filepath.Dir(trace.Dir), "trace_index.jsonl"), record)
}

func codexGatewayCaptureCallIDKind(callID string) string {
	switch {
	case strings.HasPrefix(callID, "call_"):
		return "openai_call"
	case strings.HasPrefix(callID, "toolu_"):
		return "anthropic_toolu"
	default:
		return "unknown"
	}
}

func codexGatewayCaptureStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(asCaptureString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func appendUniqueCaptureString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func asCaptureString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
