package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAIStreamFailoverErrorPreservesConfiguredDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	service := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		LogUpstreamErrorBody:         true,
		LogUpstreamErrorBodyMaxBytes: 8,
	}}}
	account := &Account{ID: 1, Name: "existing-account-context", Platform: PlatformOpenAI}
	payload := []byte("existing-upstream-detail")

	_ = service.newOpenAIStreamFailoverError(context, account, true, "req_existing", payload, "upstream failed")

	rawEvents, ok := context.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, truncateString(string(payload), 8), events[0].Detail)
	require.Equal(t, account.ID, events[0].AccountID)
	require.Equal(t, account.Name, events[0].AccountName)
}

func TestRecordOpenAIStreamUpstreamErrorNewPathMarksSafeStreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	service := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		LogUpstreamErrorBody: true,
	}}}
	account := &Account{ID: 2, Name: "new-path-account", Platform: PlatformOpenAI}
	payload := []byte("raw-payload-should-not-be-recorded")

	service.recordOpenAIStreamUpstreamError(context, account, false, "req_new", "http_error", payload, "upstream failed", false)

	_, hasUpstreamEvents := context.Get(OpsUpstreamErrorsKey)
	require.False(t, hasUpstreamEvents)
	streamError, ok := GetOpsStreamError(context)
	require.True(t, ok)
	require.Equal(t, "upstream_error", streamError.ErrType)
	require.Equal(t, "upstream failed", streamError.Message)
	require.Equal(t, http.StatusBadGateway, streamError.IntendedStatus)
}
