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
		"X-Request-Id":  []string{"req-secret"},
		"Content-Type":  []string{"application/json"},
	}
	out := redactor.RedactHeaders(headers)
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Authorization"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Cookie"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Set-Cookie"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("X-Api-Key"))
	require.Equal(t, []string{"[REDACTED]"}, out.Values("Api-Key"))
	require.NotContains(t, out.Get("X-Request-Id"), "req-secret")
	require.Contains(t, out.Get("X-Request-Id"), "hmac-sha256:")
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
			{"type":"function_call_output","call_id":"call_1","output":{"status":"PRIVATE_STRUCTURED_STATUS","type":"PRIVATE_STRUCTURED_TYPE","model":"PRIVATE_STRUCTURED_MODEL","text":"do not store this output"}}
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
	require.NotContains(t, fmtAny(shape), "PRIVATE_STRUCTURED_STATUS")
	require.NotContains(t, fmtAny(shape), "PRIVATE_STRUCTURED_TYPE")
	require.NotContains(t, fmtAny(shape), "PRIVATE_STRUCTURED_MODEL")
	require.Contains(t, fmtAny(shape), "hmac-sha256:")
	require.Contains(t, fmtAny(shape), "shell")
	require.Contains(t, fmtAny(shape), "timeout_ms")
	require.NotContains(t, fmtAny(shape), "function_call_output")
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
	require.Contains(t, string(errorsBytes), `"message_hash"`)
	require.NotContains(t, string(errorsBytes), "simulated")
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
	manager.RecordStreamEvent(trace, "client", "private_payload", []byte(`{"type":"PRIVATE_EVENT_TYPE_SENTINEL","status":"PRIVATE_EVENT_STATUS_SENTINEL"}`))
	if flusher, ok := writer.(*CodexGatewayCaptureStreamWriter); ok {
		flusher.FlushPending()
	}
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{
		Status: "ok",
		Additional: map[string]any{
			"response_id": "resp_additional_secret",
			"private_url": "https://private.example/repo",
			"note":        "ADDITIONAL_PRIVATE_NOTE",
			"nested": map[string]any{
				"private_project_slug": "PRIVATE_NESTED_VALUE",
			},
		},
	})
	require.NoError(t, manager.Close())

	require.Equal(t, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"secret\"}\n\n: keepalive\n\ndata: [DONE]\n\n", dst.String())
	eventsBytes, err := os.ReadFile(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "stream", "client_stream.events.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(eventsBytes), "response.output_text.delta")
	require.Contains(t, string(eventsBytes), "done")
	require.NotContains(t, string(eventsBytes), "secret")
	require.NotContains(t, string(eventsBytes), "PRIVATE_EVENT_TYPE_SENTINEL")
	require.NotContains(t, string(eventsBytes), "PRIVATE_EVENT_STATUS_SENTINEL")

	summaryBytes, err := os.ReadFile(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "stream", "summary.json"))
	require.NoError(t, err)
	require.Contains(t, string(summaryBytes), "additional")
	require.NotContains(t, string(summaryBytes), "resp_additional_secret")
	require.NotContains(t, string(summaryBytes), "https://private.example/repo")
	require.NotContains(t, string(summaryBytes), "ADDITIONAL_PRIVATE_NOTE")
	require.NotContains(t, string(summaryBytes), "private_project_slug")
	require.NotContains(t, string(summaryBytes), "PRIVATE_NESTED_VALUE")
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
	require.Contains(t, string(toolClosure), "call_id_hash")
	require.Contains(t, string(toolClosure), "call_ids_hashed")
	require.NotContains(t, string(toolClosure), "call_b")
	require.NotContains(t, string(toolClosure), "call_a")
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
	require.Contains(t, string(errorsBytes), "message_hash")
	require.NotContains(t, string(errorsBytes), "must persist")
}

func TestCodexGatewayCaptureV2SummaryReportAndSanitizedCodexHeaders(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o600))
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              keyPath,
		CorrelationHashKeyFile:   keyPath,
		CaptureSuccessSampleRate: 1,
		IncludeResponseHeader:    true,
	})
	defer manager.Close()

	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID: "v2_summary",
		Method:  "POST",
		Path:    "/codex/v1/responses",
		Model:   "deepseek-v4-pro",
	})
	require.NotNil(t, trace)
	headers := http.Header{
		"Authorization":       []string{"Bearer sk-test-secret"},
		"Session_id":          []string{"019eaaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"},
		"Thread_id":           []string{"019effff-bbbb-7ccc-8ddd-eeeeeeeeeeee"},
		"X-Client-Request-Id": []string{"019e1111-bbbb-7ccc-8ddd-eeeeeeeeeeee"},
		"X-Codex-Window-Id":   []string{"019eaaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee:0"},
		"X-OpenAI-Subagent":   []string{"collab_spawn"},
		"X-Codex-Turn-Metadata": []string{`{
			"session_id":"019eaaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee",
			"thread_id":"019effff-bbbb-7ccc-8ddd-eeeeeeeeeeee",
			"turn_id":"019e2222-bbbb-7ccc-8ddd-eeeeeeeeeeee",
			"thread_source":"subagent",
			"workspaces":{
				"/Users/alice/private/repo":{
					"associated_remote_urls":{"origin":"https://github.com/org/private.git","ssh":"git@github.com:org/private.git"},
					"latest_git_commit_hash":"0123456789abcdef0123456789abcdef01234567",
					"branch":"secret-branch",
					"has_changes":true
				}
			},
			"sandbox":"none"
		}`},
	}
	manager.RecordClientRequest(trace, headers, []byte(`{
		"model":"deepseek-v4-pro",
		"prompt_cache_key":"cache-secret",
		"input":[
			{"role":"user","content":[{"type":"input_text","text":"PRIVATE_PROMPT_SENTINEL"}]},
			{"role":"PRIVATE_ROLE_SENTINEL","content":[{"type":"PRIVATE_CONTENT_TYPE_SENTINEL","text":"PRIVATE_STRUCTURED_PROMPT_SENTINEL"}]},
			{"type":"function_call_output","call_id":"call_secret","output":"PRIVATE_COMMAND_OUTPUT_SENTINEL"}
		],
		"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}],
		"stream":true
	}`))
	manager.RecordUpstreamResponse(trace, http.Header{"X-Request-Id": []string{"up_req_123"}}, http.StatusOK, nil)
	manager.RecordStreamEvent(trace, "upstream", "message_stop", []byte(`{"type":"message_stop"}`))
	manager.RecordStreamEvent(trace, "client", "response.output_text.delta", []byte(`{"type":"response.output_text.delta","delta":"PRIVATE_BROWSER_PAGE_SENTINEL"}`))
	manager.RecordStreamEvent(trace, "client", "response.completed", []byte(`{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`))
	manager.RecordProviderResult(trace, CodexGatewayProviderResult{
		ResponseID:        "resp_1",
		UpstreamRequestID: "up_req_123",
		UpstreamModel:     "deepseek-v4-pro",
		Usage: CodexGatewayProviderUsage{
			InputTokens:          100,
			OutputTokens:         20,
			TotalTokens:          120,
			CacheReadInputTokens: 80,
		},
	})
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "v2_summary")
	summary := readCaptureJSONFile(t, filepath.Join(traceDir, "summary.json"))
	require.Equal(t, float64(http.StatusOK), summary["http_status"])
	require.Equal(t, "completed", summary["terminal_classification"])
	require.Contains(t, summary, "response_id_hash")
	require.Contains(t, summary, "upstream_request_id_hash")
	require.NotContains(t, fmtAny(summary), "resp_1")
	require.NotContains(t, fmtAny(summary), "up_req_123")
	require.NotContains(t, fmtAny(summary), "resp_additional_secret")
	require.NotContains(t, fmtAny(summary), "https://private.example/repo")
	require.NotContains(t, fmtAny(summary), "ADDITIONAL_PRIVATE_NOTE")
	require.NotContains(t, fmtAny(summary), "private_project_slug")
	require.NotContains(t, fmtAny(summary), "PRIVATE_NESTED_VALUE")
	providerUsage := summary["provider_usage"].(map[string]any)
	require.Equal(t, float64(100), providerUsage["InputTokens"])
	require.Equal(t, float64(80), providerUsage["CacheReadInputTokens"])

	headersOut := readCaptureJSONFile(t, filepath.Join(traceDir, "client_request.headers.json"))
	require.Contains(t, headersOut, "codex_context")
	require.Contains(t, fmtAny(headersOut), "session_id_hash")
	require.Contains(t, fmtAny(headersOut), "path_hash")
	require.NotContains(t, fmtAny(headersOut), "/Users/alice/private/repo")
	require.NotContains(t, fmtAny(headersOut), "https://github.com/org/private.git")
	require.NotContains(t, fmtAny(headersOut), "0123456789abcdef0123456789abcdef01234567")
	require.NotContains(t, fmtAny(headersOut), "019eaaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee")

	report := readCaptureJSONFile(t, filepath.Join(traceDir, "trace_report.json"))
	require.Equal(t, "v2_summary", report["trace_id"])
	require.Equal(t, "completed", report["terminal_classification"])
	require.Equal(t, "deepseek-v4-pro", report["model"])

	sessionReport, err := os.ReadFile(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "session_report.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(sessionReport), "v2_summary")

	assertCaptureDirDoesNotContain(t, traceDir,
		"PRIVATE_PROMPT_SENTINEL",
		"PRIVATE_COMMAND_OUTPUT_SENTINEL",
		"PRIVATE_BROWSER_PAGE_SENTINEL",
		"PRIVATE_ROLE_SENTINEL",
		"PRIVATE_CONTENT_TYPE_SENTINEL",
		"PRIVATE_STRUCTURED_PROMPT_SENTINEL",
		"sk-test-secret",
		"/Users/alice/private/repo",
		"https://github.com/org/private.git",
		"git@github.com:org/private.git",
		"secret-branch",
		"0123456789abcdef0123456789abcdef01234567",
		"019eaaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee",
		"resp_1",
		"up_req_123",
	)
}

func TestCodexGatewayCaptureV2ErrorClassification(t *testing.T) {
	cases := []struct {
		name      string
		err       CodexGatewayCaptureError
		wantClass string
	}{
		{
			name:      "openai account unavailable",
			err:       CodexGatewayCaptureError{Origin: "upstream", Stage: "stream", Provider: "openai", Model: "gpt-5.4-mini", ErrorCode: "upstream_error", Message: "no available OpenAI accounts supporting model: gpt-5.4-mini"},
			wantClass: "model_unavailable_background_task",
		},
		{
			name:      "cloudflare html timeout",
			err:       CodexGatewayCaptureError{Origin: "upstream", Stage: "stream", Provider: "anthropic", HTTPStatus: 524, Message: "<!DOCTYPE html><title>524: A timeout occurred</title>"},
			wantClass: "cloudflare_html",
		},
		{
			name:      "visible output canceled",
			err:       CodexGatewayCaptureError{Origin: "upstream", Stage: "stream", Provider: "deepseek", Message: "context canceled", VisibleOutputStarted: true},
			wantClass: "canceled_after_visible_output",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codexGatewayCaptureClassifyError(tc.err)
			require.Equal(t, tc.wantClass, got)
		})
	}
}

func TestCodexGatewayCaptureV2RecognizesFailedAndIncompleteTerminalEvents(t *testing.T) {
	require.True(t, codexGatewayCaptureIsTerminalEvent("response.failed"))
	require.True(t, codexGatewayCaptureIsTerminalEvent("response.incomplete"))

	failed := codexGatewayCaptureTerminalClassification(codexGatewayCaptureTraceState{
		ClientTerminalEvent: "response.failed",
	}, CodexGatewayCaptureFinishSummary{Status: "failed"})
	require.Equal(t, "completed", failed)

	incomplete := codexGatewayCaptureTerminalClassification(codexGatewayCaptureTraceState{
		UpstreamTerminalEvent: "response.incomplete",
	}, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.Equal(t, "completed", incomplete)
}

func TestCodexGatewayCaptureV2TerminalClassificationPrefersErrors(t *testing.T) {
	state := codexGatewayCaptureTraceState{
		UpstreamTerminalEvent: "message_stop",
		VisibleOutputStarted:  true,
		LastError:             &CodexGatewayCaptureError{ErrorClass: codexGatewayCaptureErrorClassCanceledAfterVisibleOutput},
		ClientTerminalEvent:   "",
		ClientEventCount:      2,
		UpstreamEventCount:    2,
		UpstreamFirstEventAt:  time.Now(),
		UpstreamLastEventAt:   time.Now(),
		ClientFirstEventAt:    time.Now(),
		ClientLastEventAt:     time.Now(),
	}
	got := codexGatewayCaptureTerminalClassification(state, CodexGatewayCaptureFinishSummary{Status: "failed"})
	require.Equal(t, "client_canceled_after_visible_output", got)
}

func TestCodexGatewayCaptureV2CrossTraceToolLinkageUsesHashedCallIDs(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o600))
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              keyPath,
		CorrelationHashKeyFile:   keyPath,
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()

	traceA := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID: "trace_A",
		Method:  "POST",
		Path:    "/codex/v1/responses",
		Model:   "deepseek-v4-pro",
	})
	require.NotNil(t, traceA)
	manager.RecordProviderResult(traceA, CodexGatewayProviderResult{
		ToolCalls: []CodexGatewayStoredToolCall{{
			ID:   "call_cross_trace_secret",
			Type: CodexGatewayToolKindFunction,
			Name: "exec_command",
		}},
	})

	traceB := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID: "trace_B",
		Method:  "POST",
		Path:    "/codex/v1/responses",
		Model:   "deepseek-v4-flash",
	})
	require.NotNil(t, traceB)
	manager.RecordClientRequest(traceB, nil, []byte(`{
		"model":"deepseek-v4-flash",
		"input":[{"type":"function_call_output","call_id":"call_cross_trace_secret","output":"PRIVATE_TOOL_OUTPUT"}]
	}`))
	manager.FinishTrace(traceA, CodexGatewayCaptureFinishSummary{Status: "ok"})
	manager.FinishTrace(traceB, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, manager.Close())

	dateDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"))
	closureA, err := os.ReadFile(filepath.Join(dateDir, "trace_A", "tool_closure.json"))
	require.NoError(t, err)
	require.Contains(t, string(closureA), "result_received")
	require.Contains(t, string(closureA), "trace_B")
	require.NotContains(t, string(closureA), "call_cross_trace_secret")
	require.NotContains(t, string(closureA), "PRIVATE_TOOL_OUTPUT")

	closureB, err := os.ReadFile(filepath.Join(dateDir, "trace_B", "tool_closure.json"))
	require.NoError(t, err)
	require.Contains(t, string(closureB), "trace_A")
	require.Contains(t, string(closureB), "call_id_hash")
	require.NotContains(t, string(closureB), "call_cross_trace_secret")
	require.NotContains(t, string(closureB), "PRIVATE_TOOL_OUTPUT")

	index, err := os.ReadFile(filepath.Join(dateDir, "trace_index.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(index), `"direction":"emitted"`)
	require.Contains(t, string(index), `"direction":"received"`)
	require.Contains(t, string(index), "exec_command")
	require.NotContains(t, string(index), "call_cross_trace_secret")
}

func TestCodexGatewayCaptureV2GPTMiniBackgroundTaskDiagnostics(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o600))
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              keyPath,
		CorrelationHashKeyFile:   keyPath,
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()

	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID: "mini_background",
		Method:  "POST",
		Path:    "/codex/v1/responses",
		Model:   "gpt-5.4-mini",
	})
	require.NotNil(t, trace)
	manager.RecordClientRequest(trace, nil, []byte(`{
		"model":"gpt-5.4-mini",
		"reasoning":{"effort":"low"},
		"parallel_tool_calls":true,
		"text":{"format":{"type":"json_schema","name":"private_title_schema","schema":{"type":"object","properties":{"title":{"type":"string"},"private_project_slug":{"type":"string"}},"required":["title"]}}},
		"input":[{"role":"user","content":[{"type":"input_text","text":"PRIVATE_BACKGROUND_PROMPT"}]}]
	}`))
	manager.RecordError(trace, CodexGatewayCaptureError{
		Origin:   "upstream",
		Stage:    "stream",
		Provider: "openai",
		Model:    "gpt-5.4-mini",
		Message:  "No available accounts",
	})
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "failed"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "mini_background")
	report := readCaptureJSONFile(t, filepath.Join(traceDir, "trace_report.json"))
	require.Equal(t, "model_unavailable_background_task", report["error_class"])
	require.Contains(t, fmtAny(report), "desktop_background_task")
	require.Contains(t, fmtAny(report), "title")
	require.NotContains(t, fmtAny(report), "private_title_schema")
	require.NotContains(t, fmtAny(report), "private_project_slug")
	require.NotContains(t, fmtAny(report), "PRIVATE_BACKGROUND_PROMPT")

	diagnostics := readCaptureJSONFile(t, filepath.Join(traceDir, "client_request.diagnostics.json"))
	require.Contains(t, fmtAny(diagnostics), "desktop_background_task")
	require.Contains(t, fmtAny(diagnostics), "text_format_schema_key_count")
	require.NotContains(t, fmtAny(diagnostics), "private_title_schema")
	require.NotContains(t, fmtAny(diagnostics), "private_project_slug")
	assertCaptureDirDoesNotContain(t, traceDir,
		"private_title_schema",
		"private_project_slug",
		"PRIVATE_BACKGROUND_PROMPT",
		"No available accounts",
	)
}

func TestCodexGatewayCaptureV2DeepSeekCacheMissAttribution(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o600))
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              keyPath,
		CorrelationHashKeyFile:   keyPath,
		CaptureSuccessSampleRate: 1,
	})

	model := CodexGatewayModel{
		Slug:          "deepseek-v4-pro",
		Provider:      "deepseek",
		UpstreamModel: "deepseek-v4-pro",
	}
	buildTrace := func(traceID string, tools json.RawMessage, ctx CodexGatewayDeepSeekRequestContext) {
		trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
			TraceID:  traceID,
			Method:   "POST",
			Path:     "/codex/v1/responses",
			Model:    "deepseek-v4-pro",
			Provider: "deepseek",
		})
		require.NotNil(t, trace)
		_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
			Model:        "deepseek-v4-pro",
			Instructions: json.RawMessage(`"Keep answers short."`),
			Input: json.RawMessage(`[
				{"type":"message","role":"developer","content":[{"type":"input_text","text":"Use local tools when possible."}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect the repository"}]}
			]`),
			Tools: tools,
		}, nil, CodexGatewayDeepSeekRequestContext{
			SessionKey:           ctx.SessionKey,
			IsolationKey:         ctx.IsolationKey,
			WorkspaceKey:         ctx.WorkspaceKey,
			ManagedSessionBucket: ctx.ManagedSessionBucket,
			UserID:               ctx.UserID,
			CaptureTrace:         trace,
		}, CodexGatewayDeepSeekRequestConfig{})
		require.NoError(t, err)
		manager.RecordProviderResult(trace, CodexGatewayProviderResult{
			UpstreamModel: "deepseek-v4-pro",
			Usage: CodexGatewayProviderUsage{
				InputTokens:          1200,
				OutputTokens:         24,
				TotalTokens:          1224,
				CacheReadInputTokens: 0,
			},
		})
		manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	}

	toolsA := json.RawMessage(`[
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
	]`)
	toolsB := json.RawMessage(`[
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}
	]`)

	buildTrace("miss_not_warmed", toolsA, CodexGatewayDeepSeekRequestContext{SessionKey: "session_a", IsolationKey: "iso_a", WorkspaceKey: "workspace_shared"})
	buildTrace("miss_repeat", toolsA, CodexGatewayDeepSeekRequestContext{SessionKey: "session_b", IsolationKey: "iso_a", WorkspaceKey: "workspace_shared"})
	buildTrace("miss_tool_change", toolsB, CodexGatewayDeepSeekRequestContext{SessionKey: "session_c", IsolationKey: "iso_a", WorkspaceKey: "workspace_shared"})
	buildTrace("miss_user_change", toolsB, CodexGatewayDeepSeekRequestContext{SessionKey: "session_d", IsolationKey: "iso_b", WorkspaceKey: "workspace_shared"})
	buildTrace("miss_other_workspace", toolsA, CodexGatewayDeepSeekRequestContext{SessionKey: "session_e", IsolationKey: "iso_a", WorkspaceKey: "workspace_other"})
	require.NoError(t, manager.Close())

	dateDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"))
	report1 := readCaptureJSONFile(t, filepath.Join(dateDir, "miss_not_warmed", "trace_report.json"))
	require.Contains(t, fmtAny(report1["request_diagnostics"]), "stable_serialization")
	require.Contains(t, fmtAny(report1["cache_efficiency"]), "request_not_warmed")

	report2 := readCaptureJSONFile(t, filepath.Join(dateDir, "miss_repeat", "trace_report.json"))
	require.Contains(t, fmtAny(report2["cache_efficiency"]), "upstream_best_effort_or_unknown")

	report3 := readCaptureJSONFile(t, filepath.Join(dateDir, "miss_tool_change", "trace_report.json"))
	require.Contains(t, fmtAny(report3["cache_efficiency"]), "tool_schema_changed")
	require.Contains(t, fmtAny(report3["cache_efficiency"]), "prefix_hash_changed_reason")
	require.Contains(t, fmtAny(report3["cache_usage"]), "tool_schema_hash")

	report4 := readCaptureJSONFile(t, filepath.Join(dateDir, "miss_user_change", "trace_report.json"))
	require.Contains(t, fmtAny(report4["cache_efficiency"]), "user_id_changed")

	report5 := readCaptureJSONFile(t, filepath.Join(dateDir, "miss_other_workspace", "trace_report.json"))
	require.NotContains(t, fmtAny(report5["cache_efficiency"]), "tool_schema_changed")
	require.NotContains(t, fmtAny(report5["cache_efficiency"]), "user_id_changed")
}

func TestCodexGatewayCaptureV2DeepSeekRecordsFullPrefixDiagnostics(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o600))
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              keyPath,
		CorrelationHashKeyFile:   keyPath,
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()

	store := NewCodexGatewayStateStore(CodexGatewayStateStoreConfig{TTL: time.Minute, MaxItems: 4, Now: time.Now})
	replay := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"PRIVATE_REPLAY_PROMPT"}`),
		json.RawMessage(`{"role":"assistant","content":"PRIVATE_REPLAY_ANSWER"}`),
	}
	require.NoError(t, store.Put(CodexGatewayResponseState{
		Key: CodexGatewayStateLookupKey{
			ResponseID:    "resp_full_diag",
			SessionKey:    "session_full_diag",
			IsolationKey:  "iso_full_diag",
			Provider:      "deepseek",
			UpstreamModel: "deepseek-v4-pro",
		},
		AssistantContent:        "PRIVATE_REPLAY_ANSWER",
		AssistantContentPresent: true,
		ReplayMessages:          replay,
	}))

	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID:  "deepseek_full_prefix_diag",
		Method:   "POST",
		Path:     "/codex/v1/responses",
		Model:    "deepseek-v4-pro",
		Provider: "deepseek",
	})
	require.NotNil(t, trace)
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	_, err := BuildCodexGatewayDeepSeekRequest(model, CodexGatewayResponsesCreateRequest{
		Model:              "deepseek-v4-pro",
		PreviousResponseID: stringPtr("resp_full_diag"),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"PRIVATE_CURRENT_PROMPT"}]}
		]`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}
		]`),
	}, store, CodexGatewayDeepSeekRequestContext{
		SessionKey:           "session_full_diag",
		IsolationKey:         "iso_full_diag",
		WorkspaceKey:         "workspace_full_diag",
		ManagedSessionBucket: "bucket_full_diag",
		CaptureTrace:         trace,
	}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "deepseek_full_prefix_diag")
	diagnostics := readCaptureJSONFile(t, filepath.Join(traceDir, "client_request.diagnostics.json"))
	deepseek := diagnostics["deepseek_cache"].(map[string]any)
	for _, key := range []string{
		"raw_body_hash",
		"messages_full_hash",
		"message_suffix_hash",
		"message_last_hash",
		"message_count",
		"tool_schema_hash",
		"user_id_hash",
		"workspace_scope_hash",
		"managed_session_bucket_hash",
		"previous_response_id_present",
		"previous_response_replay_mode",
		"state_lookup_status",
		"request_prefix_hash",
		"static_prefix_hash",
		"message_prefix_hash",
		"request_shape_hash",
	} {
		require.Contains(t, deepseek, key)
	}
	require.Equal(t, true, deepseek["previous_response_id_present"])
	require.Equal(t, "full_replay_messages", deepseek["previous_response_replay_mode"])
	require.Equal(t, "hit", deepseek["state_lookup_status"])
	require.Equal(t, float64(3), deepseek["message_count"])

	report := readCaptureJSONFile(t, filepath.Join(traceDir, "trace_report.json"))
	require.Contains(t, fmtAny(report["cache_usage"]), "messages_full_hash")
	require.Contains(t, fmtAny(report["request_diagnostics"]), "previous_response_replay_mode")
	assertCaptureDirDoesNotContain(t, traceDir,
		"PRIVATE_REPLAY_PROMPT",
		"PRIVATE_REPLAY_ANSWER",
		"PRIVATE_CURRENT_PROMPT",
		"resp_full_diag",
		"workspace_full_diag",
		"bucket_full_diag",
		"Authorization",
		"sk-",
	)
}

func TestCodexGatewayCaptureV2DeepSeekCacheUsagePrefersProviderExtra(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o600))
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              keyPath,
		CorrelationHashKeyFile:   keyPath,
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()

	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID:  "deepseek_cache_extra",
		Method:   "POST",
		Path:     "/codex/v1/responses",
		Model:    "deepseek-v4-pro",
		Provider: "deepseek",
	})
	require.NotNil(t, trace)
	manager.RecordProviderResult(trace, CodexGatewayProviderResult{
		UpstreamModel: "deepseek-v4-pro",
		Usage: CodexGatewayProviderUsage{
			InputTokens:          1200,
			OutputTokens:         50,
			TotalTokens:          1250,
			CacheReadInputTokens: 900,
			ProviderUsageExtra: map[string]any{
				"prompt_cache_hit_tokens":  float64(300),
				"prompt_cache_miss_tokens": float64(900),
			},
		},
	})
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "deepseek_cache_extra")
	cacheUsage := readCaptureJSONFile(t, filepath.Join(traceDir, "cache_usage.json"))
	require.Equal(t, float64(300), cacheUsage["prompt_cache_hit_tokens"])
	require.Equal(t, float64(900), cacheUsage["prompt_cache_miss_tokens"])
	require.Equal(t, 0.25, cacheUsage["cache_hit_ratio"])

	report := readCaptureJSONFile(t, filepath.Join(traceDir, "trace_report.json"))
	efficiency := report["cache_efficiency"].(map[string]any)
	require.Equal(t, float64(300), efficiency["prompt_cache_hit_tokens"])
	require.Equal(t, float64(900), efficiency["prompt_cache_miss_tokens"])
	require.Equal(t, float64(900), efficiency["cache_miss_input_tokens"])
	require.Equal(t, 0.25, efficiency["cache_hit_rate"])
	require.Equal(t, 0.25, efficiency["cache_hit_ratio"])
	require.Contains(t, fmtAny(efficiency), "low_cache_hit_rate")

	sessionReport, err := os.ReadFile(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "session_report.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(sessionReport), `"prompt_cache_hit_tokens":300`)
	require.Contains(t, string(sessionReport), `"prompt_cache_miss_tokens":900`)
	require.Contains(t, string(sessionReport), `"cache_hit_ratio":0.25`)
}

func TestCodexGatewayCaptureV2CacheEfficiencyDiagnostics(t *testing.T) {
	baseDir := t.TempDir()
	keyPath := filepath.Join(baseDir, ".key")
	require.NoError(t, os.WriteFile(keyPath, []byte("01234567890123456789012345678901"), 0o600))
	manager := NewCodexGatewayCaptureManager(config.GatewayCodexCaptureConfig{
		Enabled:                  true,
		BaseDir:                  baseDir,
		HashKeyFile:              keyPath,
		CorrelationHashKeyFile:   keyPath,
		CaptureSuccessSampleRate: 1,
	})
	defer manager.Close()

	trace := manager.StartTrace(context.Background(), CodexGatewayCaptureTraceMeta{
		TraceID:  "cache_efficiency",
		Method:   "POST",
		Path:     "/codex/v1/responses",
		Model:    "deepseek-v4-pro",
		Provider: "deepseek",
	})
	require.NotNil(t, trace)
	manager.RecordProviderResult(trace, CodexGatewayProviderResult{
		UpstreamRequestID: "upstream_secret_request_id",
		UpstreamModel:     "deepseek-v4-pro",
		Usage: CodexGatewayProviderUsage{
			InputTokens:          1200,
			OutputTokens:         20,
			TotalTokens:          1220,
			CacheReadInputTokens: 240,
		},
	})
	manager.FinishTrace(trace, CodexGatewayCaptureFinishSummary{Status: "ok"})
	require.NoError(t, manager.Close())

	traceDir := filepath.Join(baseDir, time.Now().Format("2006-01-02"), "cache_efficiency")
	report := readCaptureJSONFile(t, filepath.Join(traceDir, "trace_report.json"))
	efficiency := report["cache_efficiency"].(map[string]any)
	require.Equal(t, float64(1200), efficiency["input_tokens"])
	require.Equal(t, float64(240), efficiency["cache_read_input_tokens"])
	require.Equal(t, float64(960), efficiency["cache_miss_input_tokens"])
	require.Equal(t, 0.2, efficiency["cache_hit_rate"])
	require.Contains(t, fmtAny(efficiency), "low_cache_hit_rate")
	require.Contains(t, fmtAny(efficiency), "deepseek_account_or_prefix_changed")

	sessionReport, err := os.ReadFile(filepath.Join(baseDir, time.Now().Format("2006-01-02"), "session_report.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(sessionReport), `"cache_hit_rate":0.2`)
	require.Contains(t, string(sessionReport), `"cache_miss_input_tokens":960`)
	require.Contains(t, string(sessionReport), "low_cache_hit_rate")
	assertCaptureDirDoesNotContain(t, traceDir,
		"PRIVATE_PROMPT_SENTINEL",
		"PRIVATE_TOOL_OUTPUT_SENTINEL",
		"sk-test-secret",
		"upstream_secret_request_id",
	)
}

func readCaptureJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func assertCaptureDirDoesNotContain(t *testing.T, dir string, needles ...string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		text := string(raw)
		for _, needle := range needles {
			require.NotContains(t, text, needle, "file %s leaked %q", path, needle)
		}
		return nil
	}))
}
