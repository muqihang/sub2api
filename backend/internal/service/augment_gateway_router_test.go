package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayRouter_KnownFirstBatchModelResolvesToProvider(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry())

	route, err := router.Resolve("deepseek-v4-pro")

	require.NoError(t, err)
	require.Equal(t, "deepseek-v4-pro", route.Model.ID)
	require.Equal(t, AugmentGatewayProviderDeepSeek, route.Provider)
	require.Equal(t, "deepseek-v4-pro", route.UpstreamModel)
}

func TestAugmentGatewayRouter_UnknownModelReturnsTypedUnavailableError(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry())

	route, err := router.Resolve("not-a-real-augment-model")

	require.Error(t, err)
	require.Empty(t, route)

	var unavailable *AugmentGatewayModelUnavailableError
	require.True(t, errors.As(err, &unavailable))
	require.Equal(t, "not-a-real-augment-model", unavailable.ModelID)
	require.Equal(t, AugmentGatewayModelUnavailableUnknown, unavailable.Kind)
}

func TestAugmentGatewayRouter_DisabledClaudeGeminiReturnUnavailableWithoutFallback(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry())

	for _, modelID := range []string{"claude-sonnet-4-5", "gemini-2.5-pro"} {
		t.Run(modelID, func(t *testing.T) {
			route, err := router.Resolve(modelID)

			require.Error(t, err)
			require.Empty(t, route)

			var unavailable *AugmentGatewayModelUnavailableError
			require.True(t, errors.As(err, &unavailable))
			require.Equal(t, modelID, unavailable.ModelID)
			require.Equal(t, AugmentGatewayModelUnavailableDisabled, unavailable.Kind)
		})
	}
}

func TestAugmentGatewayRouter_EmptyModelResolvesToDefaultGPT54(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry())

	route, err := router.Resolve("")

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", route.Model.ID)
	require.Equal(t, AugmentGatewayProviderOpenAI, route.Provider)
	require.Equal(t, "gpt-5.4", route.UpstreamModel)
}

func TestAugmentGatewayRouter_CustomDefaultModel(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry(), "gpt-5.4-mini")

	route, err := router.Resolve("   ")

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4-mini", route.Model.ID)
	require.Equal(t, AugmentGatewayProviderOpenAI, route.Provider)
}
