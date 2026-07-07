package service

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func parseResponsesStickyRequest(t *testing.T, body string, ctx *SessionContext) *ParsedRequest {
	t.Helper()
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(body)), "responses")
	require.NoError(t, err)
	parsed.SessionContext = ctx
	return parsed
}

func parseAnthropicStickyRequest(t *testing.T, body string, ctx *SessionContext) *ParsedRequest {
	t.Helper()
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(body)), domain.PlatformAnthropic)
	require.NoError(t, err)
	parsed.SessionContext = ctx
	return parsed
}

func responsesStickyContext() *SessionContext {
	return &SessionContext{ClientIP: "1.2.3.4", UserAgent: "codex_cli_rs/0.1.0", APIKeyID: 101}
}

func TestParseGatewayRequest_ResponsesInputRawAndBodyRefresh(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"old input marker"}]}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), "responses")
	require.NoError(t, err)
	require.NotEmpty(t, parsed.InputRaw())
	require.Nil(t, parsed.MessagesRaw())
	require.Equal(t, "old input marker", gjson.ParseBytes(parsed.InputRaw()).Get("0.content.0.text").String())

	newBody := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"new input marker"}]}]}`)
	require.NoError(t, parsed.ReplaceBody(newBody))
	require.Equal(t, "new input marker", gjson.ParseBytes(parsed.InputRaw()).Get("0.content.0.text").String())

	clone, err := parsed.CloneForBody([]byte(`{"model":"gpt-5.4","input":"clone input marker"}`))
	require.NoError(t, err)
	require.Equal(t, "clone input marker", gjson.ParseBytes(clone.InputRaw()).String())
	require.Equal(t, "new input marker", gjson.ParseBytes(parsed.InputRaw()).Get("0.content.0.text").String(), "clone must not mutate original raw range")
}

func TestGenerateSessionHash_ResponsesInputDifferentAndGrowingPrefixStable(t *testing.T) {
	svc := &GatewayService{}
	ctx := responsesStickyContext()
	goInput := parseResponsesStickyRequest(t, `{"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"Be concise."}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"help me with Go"}]}]}`, ctx)
	pythonInput := parseResponsesStickyRequest(t, `{"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"Be concise."}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"help me with Python"}]}]}`, ctx)
	growingInput := parseResponsesStickyRequest(t, `{"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"Be concise."}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"help me with Go"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Sure."}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"add tests"}]}]}`, ctx)

	goHash := svc.GenerateSessionHash(goInput)
	pythonHash := svc.GenerateSessionHash(pythonInput)
	growingHash := svc.GenerateSessionHash(growingInput)
	require.NotEmpty(t, goHash)
	require.NotEmpty(t, pythonHash)
	require.NotEqual(t, goHash, pythonHash, "same client context with different Responses input should not collide")
	require.Equal(t, goHash, growingHash, "stable Responses input prefix should preserve the sticky hash")
}

func TestGenerateSessionHash_MessagesProtocolIgnoresResponsesInput(t *testing.T) {
	svc := &GatewayService{}
	ctx := responsesStickyContext()
	first := parseAnthropicStickyRequest(t, `{"messages":[{"role":"user","content":"hello"}],"input":"first responses input"}`, ctx)
	second := parseAnthropicStickyRequest(t, `{"messages":[{"role":"user","content":"hello"}],"input":"second responses input"}`, ctx)
	require.Nil(t, first.InputRaw())
	require.Nil(t, second.InputRaw())
	require.Equal(t, svc.GenerateSessionHash(first), svc.GenerateSessionHash(second))
}

func TestGenerateSessionHash_ResponsesInputDoesNotOverrideHigherPrioritySources(t *testing.T) {
	svc := &GatewayService{}
	ctx := responsesStickyContext()
	metadata := `{"device_id":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef","account_uuid":"acct","session_id":"99999999-8888-4777-8666-555555555555"}`
	withMetadata := parseResponsesStickyRequest(t, `{"metadata":{"user_id":`+strconvQuoteForTest(metadata)+`},"input":"raw input marker one"}`, ctx)
	withMetadataDifferentInput := parseResponsesStickyRequest(t, `{"metadata":{"user_id":`+strconvQuoteForTest(metadata)+`},"input":"raw input marker two"}`, ctx)
	require.Equal(t, svc.GenerateSessionHash(withMetadata), svc.GenerateSessionHash(withMetadataDifferentInput), "metadata.user_id should remain highest priority")

	withCacheable := parseResponsesStickyRequest(t, `{"messages":[{"role":"user","content":[{"type":"text","text":"cache anchor","cache_control":{"type":"ephemeral"}}]}],"input":"raw input marker one"}`, ctx)
	withCacheableDifferentInput := parseResponsesStickyRequest(t, `{"messages":[{"role":"user","content":[{"type":"text","text":"cache anchor","cache_control":{"type":"ephemeral"}}]}],"input":"raw input marker two"}`, ctx)
	require.Equal(t, svc.GenerateSessionHash(withCacheable), svc.GenerateSessionHash(withCacheableDifferentInput), "cacheable content should remain higher priority than Responses input")
}

func TestGenerateSessionHash_ResponsesInputSafeLogsDoNotExposeRawAnchors(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	svc := &GatewayService{}
	parsed := parseResponsesStickyRequest(t, `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"raw responses input marker"}]}]}`, responsesStickyContext())
	hash := svc.GenerateSessionHash(parsed)
	require.NotEmpty(t, hash)

	logs := buf.String()
	require.NotContains(t, logs, "raw responses input marker")
	require.NotContains(t, logs, hash)
	require.NotContains(t, logs, "1.2.3.4")
	require.Contains(t, logs, `"source":"message_content_fallback"`)
	require.Contains(t, logs, `"content_length_bucket"`)
}

func strconvQuoteForTest(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
