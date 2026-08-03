package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIChatFallbackFailureEventUsesSafeMetadataOnly(t *testing.T) {
	account := &Account{ID: 42, Name: "sensitive-account", Platform: PlatformOpenAI}
	resp := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"X-Request-Id": {"req_safe"}}}

	event := newOpenAIChatFallbackFailureEvent(account, resp, "upstream failed")

	require.Equal(t, PlatformOpenAI, event.Platform)
	require.Equal(t, http.StatusBadGateway, event.UpstreamStatusCode)
	require.Equal(t, "req_safe", event.UpstreamRequestID)
	require.Equal(t, "failover", event.Kind)
	require.Equal(t, "upstream failed", event.Message)
	require.Zero(t, event.AccountID)
	require.Empty(t, event.AccountName)
	require.Empty(t, event.Detail)
}
