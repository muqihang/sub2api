package service

import (
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

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

func TestAugmentGatewayRouter_OpenAIModelWithoutProviderGroupReturnsTypedProviderUnavailable(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry())

	route, err := router.Resolve("gpt-5.4")

	require.Error(t, err)
	require.Empty(t, route)

	var unavailable *AugmentGatewayProviderUnavailableError
	require.True(t, errors.As(err, &unavailable))
	require.Equal(t, "gpt-5.4", unavailable.ModelID)
	require.Equal(t, AugmentGatewayProviderOpenAI, unavailable.Provider)
	require.Equal(t, AugmentGatewayProviderUnavailableNoProviderGroup, unavailable.Kind)
}

func TestAugmentGatewayRouter_DeepSeekModelWithoutProviderGroupReturnsTypedProviderUnavailable(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry())

	route, err := router.Resolve("deepseek-v4-pro")

	require.Error(t, err)
	require.Empty(t, route)

	var unavailable *AugmentGatewayProviderUnavailableError
	require.True(t, errors.As(err, &unavailable))
	require.Equal(t, "deepseek-v4-pro", unavailable.ModelID)
	require.Equal(t, AugmentGatewayProviderDeepSeek, unavailable.Provider)
	require.Equal(t, AugmentGatewayProviderUnavailableNoProviderGroup, unavailable.Kind)
}

func TestAugmentGatewayRouter_ConfiguredOpenAIAndDeepSeekModelsResolveToProvider(t *testing.T) {
	router := NewAugmentGatewayRouter(newAugmentGatewayRouterRegistryWithProviderGroups(1001, 1002))

	for _, tc := range []struct {
		modelID         string
		provider        AugmentGatewayProvider
		providerGroupID int64
	}{
		{
			modelID:         "gpt-5.4",
			provider:        AugmentGatewayProviderOpenAI,
			providerGroupID: 1001,
		},
		{
			modelID:         "deepseek-v4-pro",
			provider:        AugmentGatewayProviderDeepSeek,
			providerGroupID: 1002,
		},
	} {
		t.Run(tc.modelID, func(t *testing.T) {
			route, err := router.Resolve(tc.modelID)

			require.NoError(t, err)
			require.Equal(t, tc.modelID, route.Model.ID)
			require.Equal(t, tc.provider, route.Provider)
			require.Equal(t, tc.modelID, route.UpstreamModel)
			require.Equal(t, tc.providerGroupID, route.Model.ProviderGroupID)
		})
	}
}

func TestAugmentGatewayRouter_EmptyModelDefaultConfigFailsWithoutProviderGroup(t *testing.T) {
	router := NewAugmentGatewayRouter(NewDefaultAugmentGatewayModelRegistry())

	route, err := router.Resolve("")

	require.Error(t, err)
	require.Empty(t, route)

	var unavailable *AugmentGatewayProviderUnavailableError
	require.True(t, errors.As(err, &unavailable))
	require.Equal(t, "gpt-5.4", unavailable.ModelID)
	require.Equal(t, AugmentGatewayProviderOpenAI, unavailable.Provider)
	require.Equal(t, AugmentGatewayProviderUnavailableNoProviderGroup, unavailable.Kind)
}

func TestAugmentGatewayRouter_EmptyModelWithConfiguredOpenAIGroupResolvesToDefaultGPT54(t *testing.T) {
	router := NewAugmentGatewayRouter(newAugmentGatewayRouterRegistryWithProviderGroups(1001, 0))

	route, err := router.Resolve("")

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", route.Model.ID)
	require.Equal(t, AugmentGatewayProviderOpenAI, route.Provider)
	require.Equal(t, "gpt-5.4", route.UpstreamModel)
	require.Equal(t, int64(1001), route.Model.ProviderGroupID)
}

func TestAugmentGatewayRouter_CustomDefaultModel(t *testing.T) {
	router := NewAugmentGatewayRouter(newAugmentGatewayRouterRegistryWithProviderGroups(1001, 0), "gpt-5.4-mini")

	route, err := router.Resolve("   ")

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4-mini", route.Model.ID)
	require.Equal(t, AugmentGatewayProviderOpenAI, route.Provider)
}

func newAugmentGatewayRouterRegistryWithProviderGroups(openAIGroupID, deepSeekGroupID int64) *AugmentGatewayModelRegistry {
	return NewAugmentGatewayModelRegistry(config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: defaultAugmentGatewayEnabledModelIDs(),
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI:   openAIGroupID,
			DeepSeek: deepSeekGroupID,
		},
	})
}
