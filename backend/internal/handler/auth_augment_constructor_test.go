package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerConstructsWithAugmentGatewayService(t *testing.T) {
	augmentGatewayService := service.NewAugmentGatewayService(
		nil,
		service.NewDefaultAugmentGatewayModelRegistry(),
		service.NewAugmentGatewayRouter(service.NewDefaultAugmentGatewayModelRegistry()),
		nil,
		service.NewAugmentGatewayReasoningTurnStore(),
	)

	authHandler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, nil, augmentGatewayService)

	require.Same(t, augmentGatewayService, authHandler.augmentGatewayService)
}

func TestAuthHandlerConstructsWithAugmentOfficialSessionService(t *testing.T) {
	officialSessionService := service.NewAugmentOfficialSessionService(nil, nil, "test-secret")

	authHandler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil, officialSessionService)

	require.Same(t, officialSessionService, authHandler.augmentOfficialSessionService)
}

func TestAuthHandlerConstructorKeepsLegacyDirectCallShape(t *testing.T) {
	authHandler := NewAuthHandler(nil, nil, nil, nil, nil, nil, nil)

	require.NotNil(t, authHandler)
}
