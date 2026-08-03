package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type thinkingIsolationCache struct {
	service.GatewayCache
	deletedGroupID int64
	deletedSession string
	deleteCalls    int
}

func (c *thinkingIsolationCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	c.deleteCalls++
	c.deletedGroupID = groupID
	c.deletedSession = sessionHash
	return nil
}

func TestGatewayHandler_IsolatesBadThinkingSessionByClearingStickyBinding(t *testing.T) {
	cache := &thinkingIsolationCache{}
	gatewaySvc := service.NewGatewayService(nil, nil, nil, nil, nil, nil, nil, cache, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &GatewayHandler{gatewayService: gatewaySvc}
	groupID := int64(7)

	isolated := h.isolateBadThinkingSessionOnForwardError(context.Background(), &groupID, "session-hash", &service.Account{ID: 42, Platform: service.PlatformAnthropic}, &service.SessionCorruptThinkingSignatureError{StatusCode: 400, Message: "Invalid signature in thinking block"}, nil)

	require.True(t, isolated)
	require.Equal(t, 1, cache.deleteCalls)
	require.Equal(t, int64(7), cache.deletedGroupID)
	require.Equal(t, "session-hash", cache.deletedSession)
}
