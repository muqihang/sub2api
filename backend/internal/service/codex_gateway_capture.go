package service

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type CodexGatewayCaptureTraceMeta struct {
	TraceID          string `json:"trace_id,omitempty"`
	Method           string `json:"method,omitempty"`
	Path             string `json:"path,omitempty"`
	Model            string `json:"model,omitempty"`
	Provider         string `json:"provider,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	ThreadID         string `json:"thread_id,omitempty"`
	TurnID           string `json:"turn_id,omitempty"`
	PreviousResponse string `json:"previous_response_id,omitempty"`
	ForceCapture     bool   `json:"-"`
}

type CodexGatewayCaptureFinishSummary struct {
	Status            string                    `json:"status,omitempty"`
	HTTPStatus        int                       `json:"http_status,omitempty"`
	ResponseID        string                    `json:"response_id,omitempty"`
	UpstreamRequestID string                    `json:"upstream_request_id,omitempty"`
	UpstreamModel     string                    `json:"upstream_model,omitempty"`
	ProviderUsage     CodexGatewayProviderUsage `json:"provider_usage,omitempty"`
	Additional        map[string]any            `json:"additional,omitempty"`
}

type CodexGatewayCaptureError struct {
	Origin               string `json:"origin,omitempty"`
	Stage                string `json:"stage,omitempty"`
	Provider             string `json:"provider,omitempty"`
	Model                string `json:"model,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	Attempt              int    `json:"attempt,omitempty"`
	HTTPStatus           int    `json:"http_status,omitempty"`
	ErrorType            string `json:"error_type,omitempty"`
	ErrorCode            string `json:"error_code,omitempty"`
	Retryable            bool   `json:"retryable,omitempty"`
	FailoverDecision     string `json:"failover_decision,omitempty"`
	VisibleOutputStarted bool   `json:"visible_output_started,omitempty"`
	TerminalEventSeen    bool   `json:"terminal_event_seen,omitempty"`
	ResponseContentType  string `json:"response_content_type,omitempty"`
	BodyHash             string `json:"body_hash,omitempty"`
	Message              string `json:"message,omitempty"`
}

type CodexGatewayCaptureManager struct {
	cfg      config.GatewayCodexCaptureConfig
	redact   *CodexGatewayCaptureRedactor
	queue    chan func()
	wg       sync.WaitGroup
	close    sync.Once
	closed   atomic.Bool
	disabled bool
}

type CodexGatewayTrace struct {
	ID        string
	Dir       string
	StartedAt time.Time
	Meta      CodexGatewayCaptureTraceMeta

	manager     *CodexGatewayCaptureManager
	sampled     atomic.Bool
	mu          sync.Mutex
	pending     []codexGatewayCapturePendingWrite
	cacheUsage  map[string]any
	toolClosure map[string]any
	bytes       atomic.Int64
	dropped     atomic.Int64
	seq         atomic.Int64
}

type codexGatewayCapturePendingWrite struct {
	name       string
	payload    []byte
	appendFile bool
}

func NewCodexGatewayCaptureManager(cfg config.GatewayCodexCaptureConfig) *CodexGatewayCaptureManager {
	cfg = NormalizeCodexGatewayCaptureConfig(cfg)
	m := &CodexGatewayCaptureManager{
		cfg:      cfg,
		disabled: !cfg.Enabled,
	}
	if m.disabled {
		return m
	}
	m.redact = NewCodexGatewayCaptureRedactor(cfg)
	m.queue = make(chan func(), cfg.AsyncQueueSize)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for op := range m.queue {
			func() {
				defer func() { _ = recover() }()
				op()
			}()
		}
	}()
	return m
}

func (m *CodexGatewayCaptureManager) Close() error {
	if m == nil || m.disabled {
		return nil
	}
	m.close.Do(func() {
		m.closed.Store(true)
		close(m.queue)
		m.wg.Wait()
	})
	return nil
}

func (m *CodexGatewayCaptureManager) StartTrace(_ context.Context, meta CodexGatewayCaptureTraceMeta) *CodexGatewayTrace {
	if m == nil || m.disabled {
		return nil
	}
	sampled := meta.ForceCapture || codexGatewayCaptureSample(m.cfg.CaptureSuccessSampleRate)
	if !sampled && !m.cfg.CaptureErrorsAlways {
		return nil
	}
	traceID := strings.TrimSpace(meta.TraceID)
	if traceID == "" {
		traceID = codexGatewayCaptureNewTraceID()
	}
	dir := filepath.Join(m.cfg.BaseDir, time.Now().Format("2006-01-02"), codexGatewayCaptureSafePath(traceID))
	trace := &CodexGatewayTrace{
		ID:         traceID,
		Dir:        dir,
		StartedAt:  time.Now(),
		Meta:       meta,
		manager:    m,
		cacheUsage: make(map[string]any),
		toolClosure: map[string]any{
			"emitted_calls":     []any{},
			"received_results":  []any{},
			"missing_results":   []any{},
			"duplicate_results": []any{},
			"orphan_results":    []any{},
			"linked_trace_ids":  []any{},
			"mapping_kind":      "responses_api",
		},
	}
	trace.sampled.Store(sampled)
	if sampled {
		m.enqueue(trace, func() {
			_ = os.MkdirAll(dir, 0o700)
		})
	}
	return trace
}

func (m *CodexGatewayCaptureManager) RecordClientRequest(trace *CodexGatewayTrace, headers http.Header, body []byte) {
	if !m.enabledTrace(trace) {
		return
	}
	if m.cfg.IncludeResponseHeader {
		m.writeJSON(trace, "client_request.headers.json", m.redact.RedactHeaders(headers))
	}
	shape, err := ExtractCodexGatewayCaptureShape(body, m.redact)
	if err != nil {
		m.RecordError(trace, CodexGatewayCaptureError{Origin: "client", Stage: "decode", Message: err.Error()})
		return
	}
	m.writeJSON(trace, "client_request.shape.json", shape)
	if m.cfg.RawPayloads && strings.EqualFold(m.cfg.Level, "full") {
		m.writeJSON(trace, "client_request.body.raw.json", m.redactRawJSON(body))
	}
	m.recordClientRequestDiagnostics(trace, body)
}

func (m *CodexGatewayCaptureManager) RecordProviderSelection(trace *CodexGatewayTrace, provider, model, accountIDHash string) {
	m.RecordProviderSelectionAttempt(trace, 0, provider, model, accountIDHash)
}

func (m *CodexGatewayCaptureManager) RecordProviderSelectionAttempt(trace *CodexGatewayTrace, attempt int, provider, model, accountIDHash string) {
	if !m.enabledTrace(trace) {
		return
	}
	record := map[string]any{
		"attempt":         attempt,
		"provider":        strings.TrimSpace(provider),
		"model":           strings.TrimSpace(model),
		"account_id_hash": strings.TrimSpace(accountIDHash),
	}
	m.writeJSON(trace, "gateway_provider.json", record)
	m.writeJSONL(trace, "provider_attempts.jsonl", record)
}

func (m *CodexGatewayCaptureManager) RecordModelCatalog(trace *CodexGatewayTrace, body []byte) {
	if !m.enabledTrace(trace) {
		return
	}
	shape, err := ExtractCodexGatewayCaptureShape(body, m.redact)
	if err != nil {
		m.RecordError(trace, CodexGatewayCaptureError{Origin: "gateway", Stage: "models_shape", Message: err.Error()})
		return
	}
	m.writeJSON(trace, "model_catalog.shape.json", shape)
}

func (m *CodexGatewayCaptureManager) RecordUpstreamRequest(trace *CodexGatewayTrace, provider string, headers http.Header, body []byte) {
	if !m.enabledTrace(trace) {
		return
	}
	if m.cfg.IncludeResponseHeader {
		m.writeJSON(trace, "upstream_request.headers.json", m.redact.RedactHeaders(headers))
	}
	shape, err := ExtractCodexGatewayCaptureShape(body, m.redact)
	if err != nil {
		m.RecordError(trace, CodexGatewayCaptureError{Origin: "gateway", Stage: "upstream_request_shape", Provider: provider, Message: err.Error()})
		return
	}
	m.writeJSON(trace, "upstream_request.shape.json", shape)
	m.writeJSONL(trace, "upstream_requests.events.jsonl", map[string]any{
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
		"provider": strings.TrimSpace(provider),
		"bytes":    len(body),
		"shape":    shape,
	})
}

func (m *CodexGatewayCaptureManager) RecordUpstreamResponse(trace *CodexGatewayTrace, headers http.Header, status int, body []byte) {
	if !m.enabledTrace(trace) {
		return
	}
	if m.cfg.IncludeResponseHeader {
		m.writeJSON(trace, "upstream_response.headers.json", m.redact.RedactHeaders(headers))
	}
	summary := map[string]any{
		"http_status": status,
		"bytes":       len(body),
	}
	if len(body) > 0 {
		summary["body_hash"] = m.redact.HashText(string(body))
	}
	m.writeJSON(trace, "upstream_response.shape.json", summary)
	m.writeJSONL(trace, "upstream_responses.events.jsonl", summary)
}

func (m *CodexGatewayCaptureManager) RecordStreamEvent(trace *CodexGatewayTrace, direction, eventName string, payload []byte) {
	if !m.enabledTrace(trace) {
		return
	}
	event := map[string]any{
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		"seq":        trace.seq.Add(1),
		"direction":  strings.TrimSpace(direction),
		"event_name": strings.TrimSpace(eventName),
		"bytes":      len(payload),
	}
	if len(payload) > 0 {
		event["payload_hash"] = m.redact.HashText(string(payload))
		if shape, err := ExtractCodexGatewayCaptureShape(payload, m.redact); err == nil {
			event["payload_shape"] = shape
		}
	}
	m.writeJSONL(trace, codexGatewayCaptureStreamFile(direction), event)
}

func (m *CodexGatewayCaptureManager) RecordError(trace *CodexGatewayTrace, errMeta CodexGatewayCaptureError) {
	if !m.enabledTrace(trace) {
		return
	}
	m.activateTrace(trace)
	m.writeJSONLCritical(trace, "errors.jsonl", errMeta)
}

func (m *CodexGatewayCaptureManager) FinishTrace(trace *CodexGatewayTrace, finish CodexGatewayCaptureFinishSummary) {
	if !m.enabledTrace(trace) {
		return
	}
	summary := map[string]any{
		"trace_id":               trace.ID,
		"started_at":             trace.StartedAt.UTC().Format(time.RFC3339Nano),
		"finished_at":            time.Now().UTC().Format(time.RFC3339Nano),
		"duration_ms":            time.Since(trace.StartedAt).Milliseconds(),
		"method":                 trace.Meta.Method,
		"path":                   trace.Meta.Path,
		"model":                  trace.Meta.Model,
		"provider":               trace.Meta.Provider,
		"status":                 finish.Status,
		"http_status":            finish.HTTPStatus,
		"response_id":            finish.ResponseID,
		"upstream_request_id":    finish.UpstreamRequestID,
		"upstream_model":         finish.UpstreamModel,
		"provider_usage":         finish.ProviderUsage,
		"capture_dropped_events": trace.dropped.Load(),
	}
	for key, value := range finish.Additional {
		summary[key] = value
	}
	if !trace.sampled.Load() {
		if strings.EqualFold(strings.TrimSpace(finish.Status), "failed") {
			m.activateTrace(trace)
		} else {
			return
		}
	}
	m.writeJSONCritical(trace, "summary.json", summary)
}

func (m *CodexGatewayCaptureManager) enabledTrace(trace *CodexGatewayTrace) bool {
	return m != nil && !m.disabled && trace != nil && trace.manager == m
}

func (m *CodexGatewayCaptureManager) writeJSON(trace *CodexGatewayTrace, name string, value any) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		trace.dropped.Add(1)
		return
	}
	payload = append(payload, '\n')
	m.writeBytes(trace, name, payload, false)
}

func (m *CodexGatewayCaptureManager) writeJSONCritical(trace *CodexGatewayTrace, name string, value any) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		trace.dropped.Add(1)
		return
	}
	payload = append(payload, '\n')
	m.writeBytesCritical(trace, name, payload, false)
}

func (m *CodexGatewayCaptureManager) writeJSONL(trace *CodexGatewayTrace, name string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		trace.dropped.Add(1)
		return
	}
	payload = append(payload, '\n')
	m.writeBytes(trace, name, payload, true)
}

func (m *CodexGatewayCaptureManager) writeJSONLCritical(trace *CodexGatewayTrace, name string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		trace.dropped.Add(1)
		return
	}
	payload = append(payload, '\n')
	m.writeBytesCritical(trace, name, payload, true)
}

func (m *CodexGatewayCaptureManager) writeBytes(trace *CodexGatewayTrace, name string, payload []byte, appendFile bool) {
	if !trace.sampled.Load() {
		trace.mu.Lock()
		if !trace.sampled.Load() {
			if len(trace.pending) < 256 && trace.bytes.Load()+int64(len(payload)) <= m.cfg.MaxTraceBytes {
				trace.pending = append(trace.pending, codexGatewayCapturePendingWrite{name: name, payload: append([]byte(nil), payload...), appendFile: appendFile})
				trace.bytes.Add(int64(len(payload)))
			} else {
				trace.dropped.Add(1)
			}
			trace.mu.Unlock()
			return
		}
		trace.mu.Unlock()
	}
	if int64(len(payload)) > m.cfg.MaxEventBytes && strings.HasSuffix(name, ".jsonl") {
		trace.dropped.Add(1)
		return
	}
	if int64(len(payload)) > m.cfg.MaxBodyBytes && strings.Contains(name, ".body.") {
		trace.dropped.Add(1)
		return
	}
	if trace.bytes.Add(int64(len(payload))) > m.cfg.MaxTraceBytes {
		trace.dropped.Add(1)
		return
	}
	m.enqueue(trace, func() {
		m.writeBytesSync(trace, name, payload, appendFile)
	})
}

func (m *CodexGatewayCaptureManager) writeBytesCritical(trace *CodexGatewayTrace, name string, payload []byte, appendFile bool) {
	m.enqueueCritical(trace, func() {
		m.writeBytesSync(trace, name, payload, appendFile)
	})
}

func (m *CodexGatewayCaptureManager) writeBytesSync(trace *CodexGatewayTrace, name string, payload []byte, appendFile bool) {
	_ = os.MkdirAll(trace.Dir, 0o700)
	path := filepath.Join(trace.Dir, codexGatewayCaptureSafePath(name))
	if appendFile {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			trace.dropped.Add(1)
			return
		}
		defer f.Close()
		_, _ = f.Write(payload)
		return
	}
	_ = os.WriteFile(path, payload, 0o600)
}

func (m *CodexGatewayCaptureManager) activateTrace(trace *CodexGatewayTrace) {
	if trace == nil || trace.sampled.Load() {
		return
	}
	trace.mu.Lock()
	if trace.sampled.Load() {
		trace.mu.Unlock()
		return
	}
	trace.sampled.Store(true)
	pending := append([]codexGatewayCapturePendingWrite(nil), trace.pending...)
	trace.pending = nil
	trace.mu.Unlock()
	m.enqueueCritical(trace, func() {
		_ = os.MkdirAll(trace.Dir, 0o700)
	})
	for _, item := range pending {
		item := item
		m.enqueue(trace, func() {
			m.writeBytesSync(trace, item.name, item.payload, item.appendFile)
		})
	}
}

func (m *CodexGatewayCaptureManager) enqueueCritical(trace *CodexGatewayTrace, op func()) {
	if m == nil || m.disabled {
		return
	}
	if m.closed.Load() {
		if trace != nil {
			trace.dropped.Add(1)
		}
		return
	}
	defer func() {
		if recover() != nil && trace != nil {
			trace.dropped.Add(1)
		}
	}()
	m.queue <- op
}

func (m *CodexGatewayCaptureManager) enqueue(trace *CodexGatewayTrace, op func()) {
	if m == nil || m.disabled {
		return
	}
	if m.closed.Load() {
		if trace != nil {
			trace.dropped.Add(1)
		}
		return
	}
	defer func() {
		if recover() != nil && trace != nil {
			trace.dropped.Add(1)
		}
	}()
	select {
	case m.queue <- op:
	default:
		if trace != nil {
			trace.dropped.Add(1)
		}
	}
}

func (m *CodexGatewayCaptureManager) redactRawJSON(body []byte) any {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return map[string]any{
			"bytes": len(body),
			"hash":  m.redact.HashText(string(body)),
		}
	}
	return m.redact.RedactJSONValue(value)
}

func codexGatewayCaptureStreamFile(direction string) string {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "upstream":
		return "upstream_stream.events.jsonl"
	default:
		return "client_stream.events.jsonl"
	}
}

func codexGatewayCaptureNewTraceID() string {
	return fmt.Sprintf("trace_%d", time.Now().UnixNano())
}

func codexGatewayCaptureSample(rate float64) bool {
	switch {
	case rate >= 1:
		return true
	case rate <= 0:
		return false
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return time.Now().UnixNano()%1000000 < int64(rate*1000000)
	}
	value := binary.BigEndian.Uint64(buf[:])
	return float64(value)/float64(^uint64(0)) < rate
}

func codexGatewayCaptureSafePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "empty"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(value)
}

func codexGatewayCaptureUpstreamRequest(trace *CodexGatewayTrace, provider string, headers http.Header, body []byte) {
	if trace == nil || trace.manager == nil {
		return
	}
	trace.manager.RecordUpstreamRequest(trace, provider, headers, body)
}

func codexGatewayCaptureUpstreamResponse(trace *CodexGatewayTrace, headers http.Header, status int, body []byte) {
	if trace == nil || trace.manager == nil {
		return
	}
	trace.manager.RecordUpstreamResponse(trace, headers, status, body)
}

func codexGatewayCaptureUpstreamStreamEvent(trace *CodexGatewayTrace, eventName string, payload []byte) {
	if trace == nil || trace.manager == nil {
		return
	}
	trace.manager.RecordStreamEvent(trace, "upstream", eventName, payload)
}
