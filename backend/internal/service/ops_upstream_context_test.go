package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSafeUpstreamURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips query", "https://api.anthropic.com/v1/messages?beta=true", "https://api.anthropic.com/v1/messages"},
		{"strips fragment", "https://api.openai.com/v1/responses#frag", "https://api.openai.com/v1/responses"},
		{"strips both", "https://host/path?token=secret#x", "https://host/path"},
		{"no query or fragment", "https://host/path", "https://host/path"},
		{"empty string", "", ""},
		{"whitespace only", "  ", ""},
		{"query before fragment", "https://h/p?a=1#f", "https://h/p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, safeUpstreamURL(tt.input))
		})
	}
}

func TestAppendOpsUpstreamError_UsesRequestBodyBytesFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	setOpsUpstreamRequestBody(c, []byte(`{"model":"gpt-5"}`))
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Kind:    "http_error",
		Message: "upstream failed",
	})

	v, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := v.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, `{"model":"gpt-5"}`, events[0].UpstreamRequestBody)
}

func TestAppendOpsUpstreamError_UsesRequestBodyStringFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	c.Set(OpsUpstreamRequestBodyKey, `{"model":"gpt-4"}`)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Kind:    "request_error",
		Message: "dial timeout",
	})

	v, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := v.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, `{"model":"gpt-4"}`, events[0].UpstreamRequestBody)
}

func TestSetOpsUpstreamRequestBody_AnthropicCompatStoresSafeSummaryOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	decision := AnthropicCompatIngressDecision{
		InboundRoute:   AnthropicCompatInboundMessages,
		CCGatewayRoute: AnthropicCompatCCGatewayMessages,
		ClientType:     AnthropicCompatClientType,
	}
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Request = c.Request.WithContext(WithAnthropicCompatAuditSummary(c.Request.Context(), NewAnthropicCompatAuditSummary(decision)))

	raw := []byte(`{"model":"claude-sonnet-4-6","max_tokens":32000,"stream":true,"system":"DO-NOT-PERSIST-SYSTEM","metadata":{"user_id":"{\"account_uuid\":\"DO-NOT-PERSIST-ACCOUNT-REF\",\"email\":\"DO-NOT-PERSIST-EMAIL-REF\"}"},"messages":[{"role":"user","content":"DO-NOT-PERSIST-PROMPT"}],"tools":[{"name":"search","input_schema":{"type":"object"}}],"thinking":{"type":"enabled","budget_tokens":1024}}`)
	setOpsUpstreamRequestBody(c, raw)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: "http_error", Message: "upstream failed"})

	v, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := v.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	stored := events[0].UpstreamRequestBody
	require.Contains(t, stored, `"client_type":"claude_code_compat"`)
	require.Contains(t, stored, `"messages_count":1`)
	require.Contains(t, stored, `"tools_count":1`)
	require.Contains(t, stored, `"max_tokens":32000`)
	require.NotContains(t, stored, "DO-NOT-PERSIST-PROMPT")
	require.NotContains(t, stored, "DO-NOT-PERSIST-SYSTEM")
	require.NotContains(t, stored, "DO-NOT-PERSIST-EMAIL-REF")
	require.NotContains(t, stored, "DO-NOT-PERSIST-ACCOUNT-REF")
	require.NotContains(t, stored, `"messages":[`)
	require.NotContains(t, stored, `"content"`)
	require.NotContains(t, stored, `"metadata"`)
	require.NotContains(t, stored, `"system"`)
}
