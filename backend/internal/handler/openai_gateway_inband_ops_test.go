package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIStreamingErrorHandlersMarkOpsStreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OpenAIGatewayHandler{}

	testCases := []struct {
		name        string
		path        string
		wantType    string
		wantMessage string
		wantStatus  int
		call        func(*gin.Context)
	}{
		{
			name:        "responses",
			path:        "/v1/responses",
			wantType:    "upstream_error",
			wantMessage: "safe upstream error",
			wantStatus:  http.StatusBadGateway,
			call: func(context *gin.Context) {
				handler.handleStreamingAwareError(context, http.StatusBadGateway, "upstream_error", "safe upstream error", true)
			},
		},
		{
			name:        "responses with code category",
			path:        "/v1/responses",
			wantType:    "api_error",
			wantMessage: "safe selection error",
			wantStatus:  http.StatusServiceUnavailable,
			call: func(context *gin.Context) {
				handler.handleStreamingAwareErrorWithCodeCategory(context, http.StatusServiceUnavailable, "api_error", "no_accounts", "capacity", "safe selection error", true)
			},
		},
		{
			name:        "anthropic messages",
			path:        "/v1/messages",
			wantType:    "rate_limit_error",
			wantMessage: "safe rate limit",
			wantStatus:  http.StatusTooManyRequests,
			call: func(context *gin.Context) {
				handler.anthropicStreamingAwareError(context, http.StatusTooManyRequests, "rate_limit_error", "safe rate limit", true)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, testCase.path, nil)

			testCase.call(context)

			streamError, ok := service.GetOpsStreamError(context)
			require.True(t, ok)
			require.Equal(t, testCase.wantType, streamError.ErrType)
			require.Equal(t, testCase.wantMessage, streamError.Message)
			require.Equal(t, testCase.wantStatus, streamError.IntendedStatus)
		})
	}
}
