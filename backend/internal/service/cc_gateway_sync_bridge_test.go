package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newCCGatewayFormalPoolSyncBridgeSSE() *http.Response {
	const stream = "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_sync_bridge\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-fable-5\",\"content\":[],\"usage\":{\"input_tokens\":7,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":1}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_sync_bridge"},
		},
		Body: io.NopCloser(strings.NewReader(stream)),
	}
}

func TestCCGatewayFormalPoolNonStreamingClientUsesStreamingUpstreamAndAggregatesJSON(t *testing.T) {
	useClaudeCodeSessionBoundaryLedgerFileForTest(t)
	upstream := &ccGatewayBoundaryUpstreamRecorder{resp: newCCGatewayFormalPoolSyncBridgeSSE()}
	svc := newCCGatewayBoundaryService(upstream)
	account := plan76FormalPoolCanonicalTupleAccountForTest("2.1.197")

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := context.Background()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.198 (external, sdk-cli)")
	c.Request.Header.Set("Anthropic-Beta", "client-beta")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "99999999-8888-4777-8666-555555555555")

	body := []byte(`{"model":"claude-fable-5","stream":false,"metadata":{"user_id":"{\"device_id\":\"client-device\",\"session_id\":\"99999999-8888-4777-8666-555555555555\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"safe local fixture"}]}]}`)

	result, err := svc.Forward(ctx, c, account, parseAnthropicRequestForTest(t, body))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Stream, "client-facing result must remain non-streaming")
	require.Equal(t, "req_sync_bridge", result.RequestID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	responseBody := rec.Body.String()
	require.Equal(t, "message", gjson.Get(responseBody, "type").String())
	require.Equal(t, "claude-fable-5", gjson.Get(responseBody, "model").String())
	require.Equal(t, "hello world", gjson.Get(responseBody, "content.0.text").String())
	require.Equal(t, "end_turn", gjson.Get(responseBody, "stop_reason").String())
	require.Equal(t, int64(7), gjson.Get(responseBody, "usage.input_tokens").Int())
	require.Equal(t, int64(2), gjson.Get(responseBody, "usage.output_tokens").Int())

	require.Equal(t, true, gjson.GetBytes(upstream.lastBody, "stream").Bool(), "formal-pool upstream body must be streaming")
	attested := decodeCCGatewayFormalPoolContextForTest(t, upstream.lastReq)
	observed, ok := attested["observed_client_profile"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, observed["stream"], "CC Gateway attestation must describe the upstream streaming shape")
	require.NotContains(t, string(upstream.lastBody), "raw prompt")
	require.True(t, bytes.Contains(upstream.lastBody, []byte(`"stream":true`)))
}
