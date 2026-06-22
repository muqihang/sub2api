package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAISelectionErrorResponseWritesStructuredJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	h := &OpenAIGatewayHandler{}
	wrote := h.handleOpenAISelectionError(c, &service.OpenAIRuntimeGuardSelectionError{
		Code:     service.OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability,
		Category: "capability.unsupported_oauth_model_profile",
		Message:  "no available OpenAI accounts supporting model: gpt-5.4-nano",
	}, false, "Service temporarily unavailable")

	require.True(t, wrote)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	errorObj, ok := parsed["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "unsupported_oauth_capability", errorObj["code"])
	require.Equal(t, "capability.unsupported_oauth_model_profile", errorObj["category"])
}

func TestOpenAISelectionErrorResponseWritesStructuredResponsesSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, _ = c.Writer.WriteString(":\n\n")

	h := &OpenAIGatewayHandler{}
	wrote := h.handleOpenAISelectionError(c, &service.OpenAIRuntimeGuardSelectionError{
		Code:     service.OpenAIRuntimeGuardErrorCodeNoCompatibleAccount,
		Category: "capability.no_compatible_account",
		Message:  "no available OpenAI accounts",
	}, true, "Service temporarily unavailable")

	require.True(t, wrote)
	body := w.Body.String()
	require.Contains(t, body, "event: response.failed\n")
	payload := gjson.Get(body, `data: #`).String()
	if payload == "" {
		// gjson cannot parse SSE text directly; assert on substrings for the protocol frame.
		require.Contains(t, body, `"code":"no_compatible_account"`)
		require.Contains(t, body, `"category":"capability.no_compatible_account"`)
		return
	}
	require.Equal(t, "no_compatible_account", gjson.Get(payload, "response.error.code").String())
	require.Equal(t, "capability.no_compatible_account", gjson.Get(payload, "response.error.category").String())
}
