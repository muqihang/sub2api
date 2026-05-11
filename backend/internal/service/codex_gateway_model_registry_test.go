package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexGatewayModelRegistry_DefaultCatalogIncludesVisibleAndHiddenModels(t *testing.T) {
	reg := NewDefaultCodexGatewayModelRegistry()

	models := reg.AllModels()
	require.Equal(t, []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
	}, codexGatewayModelSlugs(models))

	gpt55, ok := reg.Resolve("gpt-5.5")
	require.True(t, ok)
	require.Equal(t, "visible", gpt55.Visibility)

	pro, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.Equal(t, "hidden", pro.Visibility)
	require.False(t, pro.SupportedInAPI)
	require.Equal(t, 1_000_000, pro.ContextWindow)
	require.Equal(t, 850_000, pro.AutoCompactTokenLimit)
	require.Equal(t, 384_000, pro.MaxOutputTokens)
	require.Equal(t, "xhigh", pro.DefaultReasoningLevel)
	require.False(t, pro.SupportsParallelToolCalls)
	require.Equal(t, "none", pro.WebSearchToolType)
	require.Equal(t, "none", pro.ImageGenerationToolType)

	flash, ok := reg.Resolve("deepseek-v4-flash")
	require.True(t, ok)
	require.Equal(t, "hidden", flash.Visibility)
	require.Equal(t, "xhigh", flash.DefaultReasoningLevel)
	require.False(t, flash.SupportsParallelToolCalls)
}

func TestCodexGatewayModelRegistry_ConfigFilterAppliesToCatalog(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(config.GatewayCodexConfig{
		EnabledModels: []string{
			"gpt-5.4",
			"deepseek-v4-pro",
		},
	})

	models := reg.AllModels()
	require.Equal(t, []string{
		"gpt-5.4",
		"deepseek-v4-pro",
	}, codexGatewayModelSlugs(models))
}

func TestCodexGatewayModelRegistry_VisibleCatalogExcludesHiddenDeepSeekModels(t *testing.T) {
	reg := NewDefaultCodexGatewayModelRegistry()

	require.Equal(t, []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
	}, codexGatewayModelSlugs(reg.Models()))
}

func TestCodexGatewayModelRegistry_ExportCatalogJSON(t *testing.T) {
	reg := NewDefaultCodexGatewayModelRegistry()

	raw, err := reg.ExportCatalogJSON()
	require.NoError(t, err)

	var envelope CodexGatewayModelsResponse
	require.NoError(t, json.Unmarshal(raw, &envelope))
	require.Equal(t, []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
	}, codexGatewayModelSlugs(envelope.Models))
}

func TestCodexGatewayModelRegistry_HidesOpenAIModelsWhenProviderGroupIsUnavailable(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(config.GatewayCodexConfig{
		EnabledModels: []string{"gpt-5.5"},
	})

	model, ok := reg.Resolve("gpt-5.5")
	require.True(t, ok)
	require.False(t, model.SupportedInAPI)
	require.Equal(t, "hidden", model.Visibility)
	require.Empty(t, reg.Models())
}

func TestCodexGatewayModelRegistry_DeepSeekVisibleWhenAllGatesPass(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"gpt-5.5", "deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderDeepSeek: {
						Provider: CodexGatewayProviderDeepSeek,
						GroupID:  2002,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"deepseek-v4-pro": {
						Enabled: true,
					},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
	)

	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.True(t, model.SupportedInAPI)
	require.Equal(t, "visible", model.Visibility)
	require.Contains(t, codexGatewayModelSlugs(reg.Models()), "deepseek-v4-pro")
}

func TestCodexGatewayModelRegistry_DeepSeekRemainsHiddenWhenProtocolFixtureGateFails(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderDeepSeek: {
						Provider: CodexGatewayProviderDeepSeek,
						GroupID:  2002,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"deepseek-v4-pro": {
						Enabled: true,
					},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": false}}),
	)

	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.False(t, model.SupportedInAPI)
	require.Equal(t, "hidden", model.Visibility)
	require.NotContains(t, codexGatewayModelSlugs(reg.Models()), "deepseek-v4-pro")
}

func TestCodexGatewayModelRegistry_DeepSeekRemainsHiddenWhenAdminExplicitlyHidesModel(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderDeepSeek: {
						Provider: CodexGatewayProviderDeepSeek,
						GroupID:  2002,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"deepseek-v4-pro": {
						Enabled:     true,
						SmokeStatus: CodexGatewaySmokeStatusFailed,
					},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
	)

	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.False(t, model.SupportedInAPI)
	require.Equal(t, "hidden", model.Visibility)
}

func TestCodexGatewayModelRegistry_ReflectsAdminServiceUpdatesLive(t *testing.T) {
	admin := NewCodexGatewayAdminService(config.GatewayCodexConfig{
		EnabledModels: []string{"deepseek-v4-pro"},
	}, nil)
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(admin),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
	)

	model, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.False(t, model.SupportedInAPI)

	_, err := admin.UpdateProviderGroup(context.Background(), CodexGatewayProviderDeepSeek, 2002)
	require.NoError(t, err)

	model, ok = reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.True(t, model.SupportedInAPI)
	require.Equal(t, "visible", model.Visibility)
}

func codexGatewayModelSlugs(models []CodexGatewayModel) []string {
	slugs := make([]string, 0, len(models))
	for _, model := range models {
		slugs = append(slugs, model.Slug)
	}
	return slugs
}

type codexGatewayRegistryStateSourceStub struct {
	state *CodexGatewayRegistryState
	err   error
}

func (s *codexGatewayRegistryStateSourceStub) LoadCodexGatewayRegistryState(_ context.Context) (*CodexGatewayRegistryState, error) {
	return s.state, s.err
}

type codexGatewayPricingReadyCheckerStub struct {
	ready map[string]bool
}

func (s codexGatewayPricingReadyCheckerStub) HasPricing(modelID string) bool {
	return s.ready[modelID]
}

type codexGatewayProtocolReadyCheckerStub struct {
	ready map[string]bool
}

func (s codexGatewayProtocolReadyCheckerStub) IsReady(modelID string) bool {
	return s.ready[modelID]
}
