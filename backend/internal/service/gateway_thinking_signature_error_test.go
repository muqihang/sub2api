package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsClaudeThinkingSignatureSessionError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{name: "invalid signature", msg: "messages.1.content.0: Invalid signature in thinking block", want: true},
		{name: "missing signature", msg: "messages.39.content.0.thinking.signature: Field required", want: true},
		{name: "nested missing signature", msg: `{"error":{"message":"messages.17.content.0.thinking.signature: Field required"}}`, want: true},
		{name: "ordinary invalid request", msg: "messages.0.content.0.text: Field required", want: false},
		{name: "non thinking signature", msg: "metadata.signature: Field required", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsClaudeThinkingSignatureSessionError(tc.msg))
		})
	}
}

func TestGatewayHandleErrorResponse_ThinkingSignatureSetsRestartSignal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	svc := &GatewayService{}
	respBody := []byte(`{"error":{"message":"messages.1.content.0: Invalid signature in thinking block","type":"invalid_request_error"}}`)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Header:     http.Header{},
	}
	account := &Account{ID: 11, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	_, err := svc.handleErrorResponse(context.Background(), resp, c, account)
	require.Error(t, err)
	var thinkingErr *SessionCorruptThinkingSignatureError
	require.ErrorAs(t, err, &thinkingErr)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "restart_required", rec.Header().Get("X-Sub2API-Session-Action"))
	require.Equal(t, "session_corrupt_thinking_signature", rec.Header().Get("X-Sub2API-Error-Class"))
	require.JSONEq(t, string(respBody), rec.Body.String())
}
