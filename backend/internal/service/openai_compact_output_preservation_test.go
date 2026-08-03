package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestReconstructResponseOutputFromSSE_PrefersRawDoneItems(t *testing.T) {
	bodyText := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hel"}`,
		`data: {"type":"response.output_text.delta","delta":"lo"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}],"opaque":{"kept":true}}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","output":[]}}`,
	}, "\n")

	outputJSON, ok := reconstructResponseOutputFromSSE(bodyText)
	require.True(t, ok)
	items := gjson.ParseBytes(outputJSON).Array()
	require.Len(t, items, 1, "raw done item and delta reconstruction must not duplicate output")
	require.Equal(t, "msg_1", items[0].Get("id").String())
	require.Equal(t, "hello", items[0].Get("content.0.text").String())
	require.True(t, items[0].Get("opaque.kept").Bool(), "unknown raw fields must be preserved")
}

func TestReconstructResponseOutputFromSSE_CompactionAddedFallback(t *testing.T) {
	bodyText := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"cmp_add","type":"compaction","encrypted_content":"added-only"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","output":[]}}`,
	}, "\n")

	outputJSON, ok := reconstructResponseOutputFromSSE(bodyText)
	require.True(t, ok)
	items := gjson.ParseBytes(outputJSON).Array()
	require.Len(t, items, 1)
	require.Equal(t, "compaction", items[0].Get("type").String())
	require.Equal(t, "added-only", items[0].Get("encrypted_content").String())
}

func TestHandleSSEToJSON_CompactSupplementsMissingCompactionIntoNonEmptyOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &OpenAIGatewayService{
		cfg:           &config.Config{},
		toolCorrector: NewCodexToolCorrector(),
	}
	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"cmp_sup","type":"compaction","encrypted_content":"supplement"}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_sup","object":"response","status":"completed","output":[{"id":"msg_sup","type":"message","role":"assistant","content":[{"type":"output_text","text":"note"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		``,
	}, "\n")

	t.Run("standard", func(t *testing.T) {
		ctx, recorder := newCompactOutputTestContext()
		resp := newCompactOutputTestResponse(upstreamSSE)

		result, err := service.handleSSEToJSON(resp, ctx, []byte(upstreamSSE), "gpt-5.5", "gpt-5.5")
		require.NoError(t, err)
		require.NotNil(t, result)
		requireCompactAndMessageOutput(t, recorder.Body.Bytes())
	})

	t.Run("passthrough", func(t *testing.T) {
		ctx, recorder := newCompactOutputTestContext()
		resp := newCompactOutputTestResponse(upstreamSSE)

		result, err := service.handlePassthroughSSEToJSON(resp, ctx, []byte(upstreamSSE), "gpt-5.5", "gpt-5.5")
		require.NoError(t, err)
		require.NotNil(t, result)
		requireCompactAndMessageOutput(t, recorder.Body.Bytes())
	})
}

func TestHandleSSEToJSON_NonCompactDoesNotSupplementCompaction(t *testing.T) {
	bodyText := `data: {"type":"response.output_item.done","item":{"id":"cmp_g","type":"compaction_summary","encrypted_content":"g"}}` + "\n"
	bodyText += `data: {"type":"response.completed","response":{"id":"r1","output":[{"type":"message"}]}}` + "\n"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	service := &OpenAIGatewayService{cfg: &config.Config{}, toolCorrector: NewCodexToolCorrector()}
	resp := newCompactOutputTestResponse(bodyText)

	result, err := service.handleSSEToJSON(resp, ctx, []byte(bodyText), "gpt-5.5", "gpt-5.5")
	require.NoError(t, err)
	require.NotNil(t, result)
	items := gjson.GetBytes(recorder.Body.Bytes(), "output").Array()
	require.Len(t, items, 1)
	require.Equal(t, "message", items[0].Get("type").String())
}

func newCompactOutputTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	return ctx, recorder
}

func newCompactOutputTestResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func requireCompactAndMessageOutput(t *testing.T, body []byte) {
	t.Helper()
	items := gjson.GetBytes(body, "output").Array()
	require.Len(t, items, 2)
	types := []string{items[0].Get("type").String(), items[1].Get("type").String()}
	require.Contains(t, types, "message")
	require.Contains(t, types, "compaction")
}
