package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func newCompactBodySignalTestContext(t *testing.T, path string, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

// body-signal 提升后必须与 path-based compact 走同一条链路：
// path 改写、requireCompact 判定、stream/store/prompt_cache_key 归一化删除。
// 回归防护：若 stream 字段存活，Forward 会用流式 handler 解析 compact 的
// JSON 响应，导致 "stream ended before a terminal event" 的换号 failover 风暴。
func TestNormalizeOpenAIResponsesCompactRequest_BodySignalPromoted(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{
		"model":"gpt-5.5",
		"stream":true,
		"store":true,
		"prompt_cache_key":"pck-signal-1",
		"input":[
			{"type":"message","role":"user","content":"hello"},
			{"type":"compaction_trigger"}
		]
	}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses", body)

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)

	require.Equal(t, "/v1/responses/compact", c.Request.URL.Path)
	require.True(t, isOpenAIRemoteCompactPath(c))

	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
	require.False(t, gjson.GetBytes(normalized, "prompt_cache_key").Exists())
	require.Equal(t, "gpt-5.5", gjson.GetBytes(normalized, "model").String())
	require.True(t, gjson.GetBytes(normalized, "input").IsArray())

	reqStream, streamOK := parseOpenAICompatibleStream(normalized)
	require.True(t, streamOK)
	require.False(t, reqStream)

	seed, exists := c.Get(service.OpenAICompactSessionSeedKeyForTest())
	require.True(t, exists)
	require.Equal(t, "pck-signal-1", seed)
	clientStream, exists := c.Get("openai_compact_client_stream")
	require.True(t, exists)
	require.Equal(t, true, clientStream)
}

func TestNormalizeOpenAIResponsesCompactRequest_BodySignalTrailingSlash(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/", body)

	_, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses/compact", c.Request.URL.Path)
}

func TestNormalizeOpenAIResponsesCompactRequest_CodexDirectAliasPromoted(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	c := newCompactBodySignalTestContext(t, "/backend-api/codex/responses", body)

	_, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/backend-api/codex/responses/compact", c.Request.URL.Path)
}

func TestNormalizeOpenAIResponsesCompactRequest_NoTriggerUntouched(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":"hello"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses", body)

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses", c.Request.URL.Path)
	require.False(t, isOpenAIRemoteCompactPath(c))
	require.Equal(t, body, normalized)
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
}

func TestNormalizeOpenAIResponsesCompactRequest_PathBasedNoDoubleSuffix(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","stream":true,"store":true,"input":[{"type":"message","role":"user","content":"hello"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/compact", body)

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses/compact", c.Request.URL.Path)
	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
}

func TestNormalizeOpenAIResponsesCompactRequest_SubpathNotPromoted(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/resp_123/cancel", body)

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses/resp_123/cancel", c.Request.URL.Path)
	require.Equal(t, body, normalized)
}

func TestMarkOpenAICompactClientStreamFromHeaders_RemoteCompactionV2(t *testing.T) {
	c := newCompactBodySignalTestContext(t, "/responses", []byte(`{}`))
	c.Request.Header.Set("Accept", "text/event-stream")
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{
		"request_kind":"compaction",
		"compaction":{"implementation":"responses_compaction_v2"}
	}`)

	require.True(t, markOpenAICompactClientStreamFromHeaders(c))
	clientStream, exists := c.Get(service.OpenAICompactClientStreamKeyForTest())
	require.True(t, exists)
	require.Equal(t, true, clientStream)
}

func TestMarkOpenAICompactClientStreamFromHeaders_RejectsAmbiguousRequests(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		accept   string
		metadata string
	}{
		{
			name:     "normal responses request",
			path:     "/responses",
			accept:   "text/event-stream",
			metadata: `{"request_kind":"response","compaction":{"implementation":"responses_compaction_v2"}}`,
		},
		{
			name:     "compact metadata without event stream",
			path:     "/responses",
			accept:   "application/json",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`,
		},
		{
			name:     "compact metadata on subresource",
			path:     "/responses/resp_123/cancel",
			accept:   "text/event-stream",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`,
		},
		{
			name:     "missing implementation",
			path:     "/responses",
			accept:   "text/event-stream",
			metadata: `{"request_kind":"compaction"}`,
		},
		{
			name:     "invalid metadata",
			path:     "/responses",
			accept:   "text/event-stream",
			metadata: `{`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCompactBodySignalTestContext(t, tt.path, []byte(`{}`))
			c.Request.Header.Set("Accept", tt.accept)
			c.Request.Header.Set("X-Codex-Turn-Metadata", tt.metadata)

			require.False(t, markOpenAICompactClientStreamFromHeaders(c))
			_, exists := c.Get(service.OpenAICompactClientStreamKeyForTest())
			require.False(t, exists)
		})
	}
}

func TestOpenAIResponses_RemoteCompactHeartbeatStartsBeforeBodyReadCompletes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newOpenAIHandlerForPreviousResponseIDValidation(t, nil)
	h.cfg = &config.Config{}
	h.cfg.Gateway.StreamKeepaliveInterval = 1

	groupID := int64(2)
	apiKey := &service.APIKey{ID: 101, GroupID: &groupID, User: &service.User{ID: 1}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
		c.Next()
	})
	router.POST("/responses", h.Responses)
	server := httptest.NewServer(router)
	defer server.Close()

	bodyReader, bodyWriter := io.Pipe()
	defer func() { _ = bodyWriter.Close() }()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/responses", bodyReader)
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`)

	type responseResult struct {
		response *http.Response
		err      error
	}
	responseCh := make(chan responseResult, 1)
	go func() {
		response, requestErr := server.Client().Do(request)
		responseCh <- responseResult{response: response, err: requestErr}
	}()

	select {
	case result := <-responseCh:
		require.NoError(t, result.err)
		defer func() { _ = result.response.Body.Close() }()
		require.Equal(t, http.StatusOK, result.response.StatusCode)
		ping := make([]byte, len("event: ping\ndata: {\"type\":\"ping\"}\n\n"))
		_, err = io.ReadFull(result.response.Body, ping)
		require.NoError(t, err)
		require.Equal(t, "event: ping\ndata: {\"type\":\"ping\"}\n\n", string(ping))
	case <-time.After(4 * time.Second):
		t.Fatal("remote compact heartbeat did not arrive while request body remained open")
	}
	require.NoError(t, bodyWriter.Close())
}
