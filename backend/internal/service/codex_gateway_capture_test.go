package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexGatewayCaptureConfigDefaults(t *testing.T) {
	cfg := config.GatewayCodexCaptureConfig{}
	normalized := NormalizeCodexGatewayCaptureConfig(cfg)

	require.False(t, normalized.Enabled)
	require.Equal(t, "summary", normalized.Level)
	require.False(t, normalized.RawPayloads)
	require.Zero(t, normalized.RetentionDays)
	require.Positive(t, normalized.MaxTraceBytes)
	require.Positive(t, normalized.MaxBodyBytes)
	require.Positive(t, normalized.MaxEventBytes)
	require.True(t, normalized.CaptureErrorsAlways)
	require.Zero(t, normalized.CaptureSuccessSampleRate)
	require.Positive(t, normalized.AsyncQueueSize)
	require.Equal(t, "hmac-sha256", normalized.HashMode)
	require.True(t, normalized.Redact.Enabled)
	require.NotEmpty(t, normalized.Redact.HeaderNames)
	require.NotEmpty(t, normalized.RequireRawPayloadsUnlockEnv)
}

func TestCodexGatewayCaptureRedactHeadersAndJSON(t *testing.T) {
	redactor := NewCodexGatewayCaptureRedactor(config.GatewayCodexCaptureConfig{
		HashKeyFile: t.TempDir() + "/capture.key",
	})

	headers := http.Header{
		"Authorization": []string{"Bearer sk-secret"},
		"Cookie":        []string{"sid=private"},
		"Set-Cookie":    []string{"sid=private; HttpOnly"},
		"X-Api-Key":     []string{"sk-key"},
		"Api-Key":       []string{"sk-key-2"},
		"Content-Type":  []string{"application/json"},
	}
	out := redactor.RedactHeaders(headers)
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Authorization"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Cookie"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Set-Cookie"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("X-Api-Key"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Api-Key"))
	require.Equal(t, []string{"application/json"}, out.Values("Content-Type"))

	payload := map[string]any{
		"api_key": "sk-json-secret",
		"nested": map[string]any{
			"authorization": "Bearer sk-nested",
			"text":          "please call Bearer sk-inline-token now",
		},
	}
	redacted := redactor.RedactJSONValue(payload).(map[string]any)
	require.Equal(t, "[REDACTED]", redacted["api_key"])
	nested := redacted["nested"].(map[string]any)
	require.Equal(t, "[REDACTED]", nested["authorization"])
	require.NotContains(t, nested["text"], "sk-inline-token")
	require.Contains(t, nested["text"], "Bearer [REDACTED]")
}

func TestCodexGatewayCaptureHashUsesLocalKeyedHMACByDefault(t *testing.T) {
	redactorA := NewCodexGatewayCaptureRedactor(config.GatewayCodexCaptureConfig{
		HashKeyFile: t.TempDir() + "/capture.key",
	})
	redactorB := NewCodexGatewayCaptureRedactor(config.GatewayCodexCaptureConfig{
		HashKeyFile: t.TempDir() + "/capture.key",
	})

	hashA1 := redactorA.HashText("short prompt")
	hashA2 := redactorA.HashText("short prompt")
	hashB := redactorB.HashText("short prompt")

	require.Equal(t, hashA1, hashA2)
	require.NotEqual(t, hashA1, hashB)
	require.Contains(t, hashA1, "hmac-sha256:")
	require.Contains(t, hashA1, "chars=12")
}

func TestCodexGatewayCaptureRawModeGuard(t *testing.T) {
	t.Setenv("SUB2API_CODEX_CAPTURE_RAW_UNLOCK", "")
	cfg := config.GatewayCodexCaptureConfig{
		Enabled:     true,
		RawPayloads: true,
	}
	require.ErrorContains(t, ValidateCodexGatewayCaptureRuntime(cfg, "release"), "requires")

	t.Setenv("SUB2API_CODEX_CAPTURE_RAW_UNLOCK", codexGatewayCaptureRawUnlockValue)
	require.NoError(t, ValidateCodexGatewayCaptureRuntime(cfg, "release"))
	require.ErrorContains(t, ValidateCodexGatewayCaptureRuntime(cfg, "production"), "not allowed in production")
}

func TestCodexGatewayCaptureShapeResponsesRequestOmitsRawPromptAndToolOutput(t *testing.T) {
	redactor := NewCodexGatewayCaptureRedactor(config.GatewayCodexCaptureConfig{
		HashKeyFile: t.TempDir() + "/capture.key",
	})
	body := []byte(`{
		"model": "deepseek-v4-pro",
		"prompt_cache_key": "cache-123",
		"instructions": "private desktop/user mixed instructions",
		"input": [
			{"role":"user","content":[{"type":"input_text","text":"do not store this prompt"}]},
			{"type":"function_call_output","call_id":"call_1","output":"do not store this output"}
		],
		"tools": [
			{
				"type":"function",
				"name":"shell",
				"description":"run commands",
				"parameters":{
					"type":"object",
					"properties":{"cmd":{"type":"string"},"timeout_ms":{"type":"integer"}},
					"required":["cmd"]
				}
			}
		],
		"reasoning": {"effort":"high"},
		"stream": true
	}`)

	shape, err := ExtractCodexGatewayCaptureShape(body, redactor)
	require.NoError(t, err)
	require.Equal(t, "object", shape["type"])
	require.ElementsMatch(t, []string{"input", "instructions", "model", "prompt_cache_key", "reasoning", "stream", "tools"}, shape["keys"])
	require.NotContains(t, fmtAny(shape), "do not store this prompt")
	require.NotContains(t, fmtAny(shape), "do not store this output")
	require.Contains(t, fmtAny(shape), "hmac-sha256:")
	require.Contains(t, fmtAny(shape), "shell")
	require.Contains(t, fmtAny(shape), "timeout_ms")
	require.Contains(t, fmtAny(shape), "function_call_output")
}

func TestCodexGatewayCaptureShapeClassifiesInstructionSource(t *testing.T) {
	redactor := NewCodexGatewayCaptureRedactor(config.GatewayCodexCaptureConfig{
		HashKeyFile: t.TempDir() + "/capture.key",
	})
	shape, err := ExtractCodexGatewayCaptureShape([]byte(`{"instructions":"local custom text"}`), redactor)
	require.NoError(t, err)
	require.Contains(t, fmtAny(shape), "unknown")
	require.Contains(t, fmtAny(shape), "summarized")
	require.NotContains(t, fmtAny(shape), "local custom text")
}

func fmtAny(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestCodexGatewayCaptureTraceManagerWritesSummaryEventsAndLimits(t *testing.T) {
	baseDir := t.TempDir()
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		MaxBodyBytes:             64,
		MaxEventBytes:            512,
		MaxTraceBytes:            8192,
		AsyncQueueSize:           8,
		CaptureErrorsAlways:      true,
		IncludeResponseHeader:    true,
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()

	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID: "trace_test",
		Method:  "POST",
		Path:    "/codex/v1/responses",
		Model:   "deepseek-v4-pro",
	})
	require.NotNil(t, trace)
	manager.RecordClientRequest(trace, http.Header{"Authorization": []string{"Bearer sk-secret"}}, []byte(`{"model":"deepseek-v4-pro","input":"private prompt"}`))
	manager.RecordProviderSelection(trace, "deepseek", "deepseek-v4-pro", "acct_hash")
	manager.RecordStreamEvent(trace, "client", "response.output_text.delta", []byte(`{"type":"response.output_text.delta","delta":"private text"}`))
	manager.RecordStreamEvent(trace, "client", "oversized", []byte(`{"payload":"this event is intentionally too large to fit the tiny event cap configured by the test................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................"}`))
	manager.RecordError(trace, CodexGatewayCaptureError{
		Origin:    "upstream",
		Stage:     "stream",
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		ErrorType: "upstream_error",
		Message:   "simulated",
	})
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "failed"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "trace_test")
	summaryBytes, err := os.ReadFile(filepath.Join(traceDir, "summary.json"))
	require.NoError(t, err)
	require.Contains(t, string(summaryBytes), `"trace_id": "trace_test"`)
	require.Contains(t, string(summaryBytes), `"capture_dropped_events"`)
	require.NotContains(t, string(summaryBytes), "private prompt")
	require.NotContains(t, string(summaryBytes), "sk-secret")

	eventsBytes, err := os.ReadFile(filepath.Join(traceDir, "client_stream.events.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(eventsBytes), "response.output_text.delta")
	require.NotContains(t, string(eventsBytes), "private text")

	headersBytes, err := os.ReadFile(filepath.Join(traceDir, "client_request.headers.json"))
	require.NoError(t, err)
	require.Contains(t, string(headersBytes), "[REDACTED]")

	errorsBytes, err := os.ReadFile(filepath.Join(traceDir, "errors.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(errorsBytes), `"origin":"upstream"`)
}

func TestCodexGatewayCaptureTraceManagerSamplingAndErrorCapture(t *testing.T) {
	baseDir := t.TempDir()
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 0,
		CaptureErrorsAlways:      true,
	})
	defer manager.Close()

	successTrace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "success"})
	require.NotNil(t, successTrace)
	manager.FinishTrace(successTrace, CodexGatewayCaptureFinishSummary{Status: "ok"})

	errorTrace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "error"})
	require.NotNil(t, errorTrace)
	manager.RecordError(errorTrace, CodexGatewayCaptureError{Origin: "gateway", Stage: "decode", Message: "bad json"})
	manager.FinishTrace(errorTrace, CodexGatewayCaptureFinishSummary{Status: "failed"})
	require.NoError(t, manager.Close())

	_, successErr := os.Stat(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "success", "summary.json"))
	require.True(t, os.IsNotExist(successErr))
	_, err := os.Stat(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "error", "errors.jsonl"))
	require.NoError(t, err)
}

func TestCodexGatewayCaptureRetentionOnlyRemovesExpiredTraceDateDirs(t *testing.T) {
	baseDir := t.TempDir()
	oldDir := filepath.Join(baseDir, time.Now().AddDate(0, 0, -10).Format("2006-01-02"), "trace_old")
	newDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "trace_new")
	unrelatedDir := filepath.Join(baseDir, "not-a-date", "keep")
	require.NoError(t, os.MkdirAll(oldDir, 0o700))
	require.NoError(t, os.MkdirAll(newDir, 0o700))
	require.NoError(t, os.MkdirAll(unrelatedDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "summary.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(newDir, "summary.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(unrelatedDir, "file.txt"), []byte("keep"), 0o600))

	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:       true,
		BaseDir:       baseDir,
		RetentionDays: 7,
		HashKeyFile:   filepath.Join(baseDir, ".key"),
	})
	defer manager.Close()
	require.NoError(t, manager.ApplyRetention(time.Now()))

	_, oldErr := os.Stat(oldDir)
	require.True(t, os.IsNotExist(oldErr))
	_, newErr := os.Stat(newDir)
	require.NoError(t, newErr)
	_, unrelatedErr := os.Stat(unrelatedDir)
	require.NoError(t, unrelatedErr)
}

func TestCodexGatewayCaptureStreamWriterPassesThroughAndParsesSplitFrames(t *testing.T) {
	baseDir := t.TempDir()
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "stream"})
	require.NotNil(t, trace)

	var dst bytes.Buffer
	writer := NewCodexGatewayCaptureStreamWriter(&dst, manager, trace, "client")
	_, err := writer.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\""))
	require.NoError(t, err)
	_, err = writer.Write([]byte("secret\"}\n\n: keepalive\n\ndata: [DONE]\n\n"))
	require.NoError(t, err)
	if flusher, ok := writer.(*CodexGatewayCaptureStreamWriter); ok {
		flusher.FlushPending()
	}
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, manager.Close())

	require.Equal(t, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"secret\"}\n\n: keepalive\n\ndata: [DONE]\n\n", dst.String())
	eventsBytes, err := os.ReadFile(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "stream", "client_stream.events.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(eventsBytes), "response.output_text.delta")
	require.Contains(t, string(eventsBytes), "done")
	require.NotContains(t, string(eventsBytes), "secret")
}

func TestCodexGatewayCaptureToolClosureAndCacheDiagnostics(t *testing.T) {
	baseDir := t.TempDir()
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "diagnostics"})
	require.NotNil(t, trace)
	manager.RecordClientRequest(trace, nil, []byte(`{
		"model":"claude-sonnet-4.6",
		"prompt_cache_key":"cache-key",
		"input":[
			{"type":"function_call_output","call_id":"call_a","output":"private output"},
			{"type":"function_call_output","call_id":"call_a","output":"private output duplicate"}
		],
		"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]
	}`))
	manager.RecordProviderResult(trace, CodexGatewayProviderResult{
		UpstreamModel: "claude-sonnet-4.6",
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:   "call_b",
			Type: CodexGatewayToolKindFunction,
			Name: "shell",
		}},
		Usage: CodexGatewayProviderUsage{
			InputTokens:          100,
			OutputTokens:         20,
			CacheReadInputTokens: 80,
		},
	})
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "diagnostics")
	toolClosure, err := os.ReadFile(filepath.Join(traceDir, "tool_closure.json"))
	require.NoError(t, err)
	require.Contains(t, string(toolClosure), "call_b")
	require.Contains(t, string(toolClosure), "call_a")
	require.NotContains(t, string(toolClosure), "private output")

	cacheUsage, err := os.ReadFile(filepath.Join(traceDir, "cache_usage.json"))
	require.NoError(t, err)
	require.Contains(t, string(cacheUsage), "prompt_cache_key_hash")
	require.Contains(t, string(cacheUsage), "request_prefix_hash")
	require.Contains(t, string(cacheUsage), "cache_read_input_tokens")
	require.Contains(t, string(cacheUsage), "80")
	require.NotContains(t, string(cacheUsage), "cache-key")
}

func TestCodexGatewayCaptureDisabledDoesNotCreateHashKey(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, "capture.key")
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:     false,
		BaseDir:     baseDir,
		HashKeyFile: keyPath,
	})
	require.NoError(t, manager.Close())
	_, err := os.Stat(keyPath)
	require.True(t, os.IsNotExist(err))
}

func TestCodexGatewayCaptureExistingHashKeyPermissionIsTightened(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "capture.key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o644))
	_ = NewCodexGatewayCaptureRedactor(config.GatewayCodexCaptureConfig{HashKeyFile: keyPath})
	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCodexGatewayCaptureSummaryAndErrorsBypassTraceByteLimit(t *testing.T) {
	baseDir := t.TempDir()
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              filepath.Join(baseDir, ".key"),
		MaxTraceBytes:            128,
		MaxEventBytes:            64,
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()
	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{TraceID: "critical"})
	require.NotNil(t, trace)
	for i := 0; i < 10; i++ {
		manager.RecordStreamEvent(trace, "client", "large", []byte(`{"payload":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"}`))
	}
	manager.RecordError(trace, CodexGatewayCaptureError{Origin: "upstream", Stage: "stream", Message: "must persist"})
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "failed"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "critical")
	summary, err := os.ReadFile(filepath.Join(traceDir, "summary.json"))
	require.NoError(t, err)
	require.Contains(t, string(summary), "capture_dropped_events")
	errorsBytes, err := os.ReadFile(filepath.Join(traceDir, "errors.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(errorsBytes), "must persist")
}
