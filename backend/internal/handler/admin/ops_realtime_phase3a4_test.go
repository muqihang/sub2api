package admin

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsOpsRealtimeRequestCanceled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("GET", "/", nil)
	c, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(c)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	require.True(t, isOpsRealtimeRequestCanceled(ctx, context.Canceled))
	require.True(t, isOpsRealtimeRequestCanceled(ctx, errors.New("pq: canceling statement due to user request")))

	freshCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	freshCtx.Request = httptest.NewRequest("GET", "/", nil)
	require.False(t, isOpsRealtimeRequestCanceled(freshCtx, errors.New("ordinary database error")))
}
