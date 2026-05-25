package service

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateSessionHashRedactsMetadataSessionLogs(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	svc := &GatewayService{}
	hash := svc.GenerateSessionHash(&ParsedRequest{
		MetadataUserID: `{"device_id":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef","account_uuid":"acct-uuid","session_id":"99999999-8888-4777-8666-555555555555"}`,
	})
	require.NotEmpty(t, hash)
	require.Contains(t, hash, "hmac-sha256:")

	logs := buf.String()
	require.NotContains(t, logs, "99999999-8888-4777-8666-555555555555")
	require.NotContains(t, logs, "deadbeefdeadbeef")
	require.NotContains(t, logs, `{"device_id"`)
	require.Contains(t, logs, `"session_present":true`)
	require.Contains(t, logs, `"device_present":true`)
}

func TestClaudeMimicDebugLineOmitsRawBodyPromptAndSession(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1/v1/messages", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer raw-token-marker")
	req.Header.Set("X-Claude-Code-Session-Id", "99999999-8888-4777-8666-555555555555")
	body := []byte(`{"system":"raw prompt marker","metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":"raw body marker"}]}`)

	line := buildClaudeMimicDebugLine(req, body, &Account{ID: 424242, Name: "raw-account-name-marker"}, "oauth", true)
	require.NotContains(t, line, "raw-token-marker")
	require.NotContains(t, line, "99999999-8888-4777-8666-555555555555")
	require.NotContains(t, line, "client-device")
	require.NotContains(t, line, "424242")
	require.NotContains(t, line, "raw-account-name-marker")
	require.NotContains(t, line, "raw prompt marker")
	require.NotContains(t, line, "raw body marker")
	require.Contains(t, line, "raw_prompt_forbidden")
	require.Contains(t, line, "account_ref=hmac-sha256:")
	require.Contains(t, line, "body.length_bucket=")
}

func TestDebugLogGatewaySnapshotOmitsRawBodyAndSensitiveHeaders(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "gateway-debug-*.log")
	require.NoError(t, err)
	defer f.Close()

	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(f)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer raw-token-marker")
	headers.Set("X-Claude-Code-Session-Id", "99999999-8888-4777-8666-555555555555")
	svc.debugLogGatewaySnapshot("CLIENT_ORIGINAL", headers, []byte(`{"messages":[{"content":"raw body marker"}],"system":"raw prompt marker"}`), map[string]string{
		"account": "424242(raw-account-name-marker)",
	})

	data, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	log := string(data)
	require.NotContains(t, log, "raw-token-marker")
	require.NotContains(t, log, "99999999-8888-4777-8666-555555555555")
	require.NotContains(t, log, "424242")
	require.NotContains(t, log, "raw-account-name-marker")
	require.NotContains(t, log, "raw body marker")
	require.NotContains(t, log, "raw prompt marker")
	require.Contains(t, log, "body_omitted_reason: raw_body_forbidden")
	require.Contains(t, log, "ref=hmac-sha256:")
}

func TestGenerateSessionHashRedactsMetadataParseFailureLogs(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	svc := &GatewayService{}
	raw := `{"device_id":}`
	_ = svc.GenerateSessionHash(&ParsedRequest{MetadataUserID: raw})

	logs := buf.String()
	require.NotContains(t, logs, raw)
	require.Contains(t, logs, `"metadata_present":true`)
	require.Contains(t, logs, `"metadata_length":14`)
	require.True(t, strings.Contains(logs, `"parsed_nil":true`) || strings.Contains(logs, `"parsed_nil":false`))
}
