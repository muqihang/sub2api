package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayHandlerExplicitCanaryRejectsForwardedLoopbackHeadersFromRemoteSocket(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages?beta=true", nil)
	req.RemoteAddr = "198.51.100.10:45678"
	req.Header.Set("X-Real-IP", "127.0.0.1")
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("CF-Connecting-IP", "127.0.0.1")

	require.False(t, gatewayHandlerExplicitCanaryLoopbackRequest(req))
}

func TestGatewayHandlerExplicitCanaryAcceptsLoopbackSocket(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages?beta=true", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	req.Header.Set("X-Real-IP", "198.51.100.10")

	require.True(t, gatewayHandlerExplicitCanaryLoopbackRequest(req))
}
