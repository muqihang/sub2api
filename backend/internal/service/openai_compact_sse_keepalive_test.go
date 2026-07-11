package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const keepaliveTestInterval = 10 * time.Millisecond

func newCompactKeepaliveTestContext(markClientStream bool) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	if markClientStream {
		ctx.Set("openai_compact_client_stream", true)
	}
	return ctx, recorder
}

func waitForCompactKeepaliveBeat() {
	time.Sleep(20 * keepaliveTestInterval)
}

func stripCompactKeepaliveComments(body string) string {
	var blocks []string
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		if strings.HasPrefix(strings.TrimSpace(block), ":") {
			continue
		}
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n")
}

func TestStartOpenAICompactSSEKeepalive_NoopWhenUnmarkedOrDisabled(t *testing.T) {
	ctx, recorder := newCompactKeepaliveTestContext(false)
	stop := StartOpenAICompactSSEKeepalive(ctx, keepaliveTestInterval)
	waitForCompactKeepaliveBeat()
	stop()
	require.Zero(t, recorder.Body.Len())
	require.False(t, StopOpenAICompactSSEKeepaliveCommitted(ctx))

	ctx, recorder = newCompactKeepaliveTestContext(true)
	stop = StartOpenAICompactSSEKeepalive(ctx, 0)
	waitForCompactKeepaliveBeat()
	stop()
	require.Zero(t, recorder.Body.Len())
	require.False(t, StopOpenAICompactSSEKeepaliveCommitted(ctx))
}

func TestOpenAICompactSSEKeepalive_CommitsHeadersAndComments(t *testing.T) {
	ctx, recorder := newCompactKeepaliveTestContext(true)
	stop := StartOpenAICompactSSEKeepalive(ctx, keepaliveTestInterval)
	defer stop()
	waitForCompactKeepaliveBeat()

	require.True(t, StopOpenAICompactSSEKeepaliveCommitted(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
	require.Contains(t, recorder.Body.String(), ": keepalive\n\n")
}

func TestOpenAICompactSSEKeepalive_FirstBeatArrivesBeforeFullInterval(t *testing.T) {
	ctx, recorder := newCompactKeepaliveTestContext(true)
	interval := 400 * time.Millisecond
	stop := StartOpenAICompactSSEKeepalive(ctx, interval)
	time.Sleep(300 * time.Millisecond)
	stop()

	require.Contains(t, recorder.Body.String(), ": keepalive\n\n")
}

func TestOpenAICompactSSEKeepalive_StopRestoresInstalledWriter(t *testing.T) {
	ctx, _ := newCompactKeepaliveTestContext(true)
	originalWriter := ctx.Writer
	stop := StartOpenAICompactSSEKeepalive(ctx, time.Hour)
	require.NotSame(t, originalWriter, ctx.Writer)

	stop()
	require.Same(t, originalWriter, ctx.Writer)
	require.NotPanics(t, stop)
}

func TestOpenAICompactSSEKeepalive_StopBeforeFirstBeatWritesNothing(t *testing.T) {
	ctx, recorder := newCompactKeepaliveTestContext(true)
	stop := StartOpenAICompactSSEKeepalive(ctx, 100*time.Millisecond)
	stop()
	time.Sleep(150 * time.Millisecond)

	require.Empty(t, recorder.Body.String())
	require.False(t, StopOpenAICompactSSEKeepaliveCommitted(ctx))
}

func TestOpenAICompactSSEKeepalive_StopDoesNotClobberLaterWriter(t *testing.T) {
	ctx, _ := newCompactKeepaliveTestContext(true)
	stop := StartOpenAICompactSSEKeepalive(ctx, time.Hour)
	replacement := &openAICompactKeepaliveWriter{}
	ctx.Writer = replacement

	stop()
	require.Same(t, replacement, ctx.Writer)
}

func TestWriteOpenAICompactSSEBridge_AfterKeepaliveCommit(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctx, recorder := newCompactKeepaliveTestContext(true)
		stop := StartOpenAICompactSSEKeepalive(ctx, keepaliveTestInterval)
		defer stop()
		waitForCompactKeepaliveBeat()

		finalResponse := []byte(`{"id":"resp_ka_1","output":[{"id":"cmp_ka","type":"compaction","encrypted_content":"x"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
		require.True(t, writeOpenAICompactSSEBridge(ctx, http.StatusOK, finalResponse))
		events := parseCompactBridgeSSE(t, stripCompactKeepaliveComments(recorder.Body.String()))
		require.Len(t, events, 2)
		require.Equal(t, "compaction", gjson.Get(events[0][1], "item.type").String())
		require.Equal(t, "response.completed", events[1][0])
	})

	t.Run("failure", func(t *testing.T) {
		ctx, recorder := newCompactKeepaliveTestContext(true)
		stop := StartOpenAICompactSSEKeepalive(ctx, keepaliveTestInterval)
		defer stop()
		waitForCompactKeepaliveBeat()

		require.True(t, writeOpenAICompactSSEBridge(ctx, http.StatusBadGateway, []byte(`{"error":{"message":"upstream exploded"}}`)))
		events := parseCompactBridgeSSE(t, stripCompactKeepaliveComments(recorder.Body.String()))
		require.Len(t, events, 1)
		require.Equal(t, "response.failed", events[0][0])
		require.Contains(t, gjson.Get(events[0][1], "response.error.message").String(), "upstream exploded")
		streamErr, ok := GetOpsStreamError(ctx)
		require.True(t, ok)
		require.Equal(t, http.StatusBadGateway, streamErr.IntendedStatus)
	})
}

func TestOpenAICompactKeepaliveWriter_RequestSideWriteSuspendsBeats(t *testing.T) {
	ctx, recorder := newCompactKeepaliveTestContext(true)
	stop := StartOpenAICompactSSEKeepalive(ctx, keepaliveTestInterval)
	defer stop()
	waitForCompactKeepaliveBeat()

	_, err := ctx.Writer.Write([]byte(`{"error":"local reject"}`))
	require.NoError(t, err)
	lengthAfterWrite := recorder.Body.Len()
	waitForCompactKeepaliveBeat()
	require.Equal(t, lengthAfterWrite, recorder.Body.Len())
	require.Contains(t, recorder.Body.String(), `{"error":"local reject"}`)
}

func TestOpenAICompactKeepaliveWriter_StateReadersAreNilSafe(t *testing.T) {
	var writer *openAICompactKeepaliveWriter
	require.NotPanics(t, func() {
		require.Equal(t, http.StatusOK, writer.Status())
		require.Equal(t, -1, writer.Size())
		require.False(t, writer.Written())
	})

	writer = &openAICompactKeepaliveWriter{}
	require.NotPanics(t, func() {
		require.Equal(t, http.StatusOK, writer.Status())
		require.Equal(t, -1, writer.Size())
		require.False(t, writer.Written())
	})
}

func TestOpenAICompactSSEKeepalive_RequestCancellationStillCleansUpWriter(t *testing.T) {
	ctx, _ := newCompactKeepaliveTestContext(true)
	requestContext, cancel := context.WithCancel(ctx.Request.Context())
	ctx.Request = ctx.Request.WithContext(requestContext)
	originalWriter := ctx.Writer
	stop := StartOpenAICompactSSEKeepalive(ctx, 10*time.Millisecond)

	cancel()
	require.NotPanics(t, stop)
	require.Same(t, originalWriter, ctx.Writer)
}

func TestOpenAICompactKeepaliveAdjustedWrittenSize_ExcludesHeartbeatBytes(t *testing.T) {
	ctx, recorder := newCompactKeepaliveTestContext(true)
	require.Equal(t, ctx.Writer.Size(), OpenAICompactKeepaliveAdjustedWrittenSize(ctx))

	stop := StartOpenAICompactSSEKeepalive(ctx, keepaliveTestInterval)
	defer stop()
	before := OpenAICompactKeepaliveAdjustedWrittenSize(ctx)
	waitForCompactKeepaliveBeat()
	require.Equal(t, before, OpenAICompactKeepaliveAdjustedWrittenSize(ctx))

	_, err := ctx.Writer.Write([]byte("real-bytes"))
	require.NoError(t, err)
	require.Equal(t, len("real-bytes"), OpenAICompactKeepaliveAdjustedWrittenSize(ctx))
	require.Contains(t, recorder.Body.String(), ": keepalive\n\n")
}

func TestWriteOpenAIFastPolicyBlockedResponse_CompactKeepalive(t *testing.T) {
	t.Run("after commit emits failed SSE", func(t *testing.T) {
		ctx, recorder := newCompactKeepaliveTestContext(true)
		stop := StartOpenAICompactSSEKeepalive(ctx, keepaliveTestInterval)
		defer stop()
		waitForCompactKeepaliveBeat()

		writeOpenAIFastPolicyBlockedResponse(ctx, &OpenAIFastBlockedError{Message: "tier blocked"})
		events := parseCompactBridgeSSE(t, stripCompactKeepaliveComments(recorder.Body.String()))
		require.Len(t, events, 1)
		require.Equal(t, "permission_error", gjson.Get(events[0][1], "response.error.code").String())
	})

	t.Run("before commit keeps JSON status", func(t *testing.T) {
		ctx, recorder := newCompactKeepaliveTestContext(true)
		stop := StartOpenAICompactSSEKeepalive(ctx, time.Hour)
		defer stop()

		writeOpenAIFastPolicyBlockedResponse(ctx, &OpenAIFastBlockedError{Message: "tier blocked"})
		require.Equal(t, http.StatusForbidden, recorder.Code)
		require.Equal(t, "permission_error", gjson.Get(recorder.Body.String(), "error.type").String())
	})
}
