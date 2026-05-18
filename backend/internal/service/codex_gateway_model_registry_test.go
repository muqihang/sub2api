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
		"claude-opus-4-7",
		"claude-opus-4-7-thinking",
		"claude-opus-4-7-ag",
		"claude-opus-4-7-thinking-ag",
		"claude-opus-4-7-max",
		"claude-opus-4-6",
		"claude-opus-4-6-thinking",
		"claude-opus-4-6-ag",
		"claude-opus-4-6-thinking-ag",
		"claude-opus-4-6-max",
		"claude-sonnet-4-6",
		"claude-sonnet-4-6-thinking",
		"claude-sonnet-4-6-ag",
		"claude-sonnet-4-6-thinking-ag",
		"claude-sonnet-4-6-max",
		"claude-haiku-4-5-20251001",
		"claude-haiku-4-5-20251001-thinking",
		"claude-haiku-4-5-20251001-ag",
		"claude-haiku-4-5-20251001-thinking-ag",
		"claude-haiku-4-5-20251001-max",
	}, codexGatewayModelSlugs(models))

	gpt55, ok := reg.Resolve("gpt-5.5")
	require.True(t, ok)
	require.Equal(t, "visible", gpt55.Visibility)
	require.Equal(t, 1_050_000, gpt55.ContextWindow)
	require.Equal(t, 900_000, gpt55.AutoCompactTokenLimit)
	require.Equal(t, 92, gpt55.EffectiveContextWindowPercent)

	gpt54, ok := reg.Resolve("gpt-5.4")
	require.True(t, ok)
	require.Equal(t, 1_050_000, gpt54.ContextWindow)
	require.Equal(t, 900_000, gpt54.AutoCompactTokenLimit)
	require.Equal(t, 92, gpt54.EffectiveContextWindowPercent)

	gpt54Mini, ok := reg.Resolve("gpt-5.4-mini")
	require.True(t, ok)
	require.Equal(t, 400_000, gpt54Mini.ContextWindow)
	require.Equal(t, 300_000, gpt54Mini.AutoCompactTokenLimit)
	require.Equal(t, 95, gpt54Mini.EffectiveContextWindowPercent)

	pro, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.Equal(t, "hidden", pro.Visibility)
	require.False(t, pro.SupportedInAPI)
	require.Equal(t, 1_000_000, pro.ContextWindow)
	require.Equal(t, 850_000, pro.AutoCompactTokenLimit)
	require.Equal(t, 384_000, pro.MaxOutputTokens)
	require.Equal(t, "xhigh", pro.DefaultReasoningLevel)
	require.False(t, pro.SupportsParallelToolCalls)
	require.Equal(t, []string{"text", "image"}, pro.InputModalities)
	require.False(t, pro.SupportsImageDetailOriginal)
	require.True(t, pro.SupportsSearchTool)
	require.Equal(t, "openai", pro.WebSearchToolType)
	require.Equal(t, "none", pro.ImageGenerationToolType)

	flash, ok := reg.Resolve("deepseek-v4-flash")
	require.True(t, ok)
	require.Equal(t, "hidden", flash.Visibility)
	require.Equal(t, "xhigh", flash.DefaultReasoningLevel)
	require.False(t, flash.SupportsParallelToolCalls)

	claude, ok := reg.Resolve("claude-opus-4-6")
	require.True(t, ok)
	require.Equal(t, "anthropic", claude.Provider)
	require.Equal(t, "kiro_claude", claude.ProviderVariant)
	require.Equal(t, "hidden", claude.Visibility)
	require.False(t, claude.SupportedInAPI)
	require.Equal(t, 1_000_000, claude.ContextWindow)
	require.Equal(t, 850_000, claude.AutoCompactTokenLimit)
	require.Equal(t, 64_000, claude.MaxOutputTokens)
	require.Equal(t, "high", claude.DefaultReasoningLevel)
	require.Equal(t, []string{"high"}, claude.SupportedReasoningLevels)
	require.Equal(t, "claude-opus-4-6", claude.UpstreamBaseModel)
	require.Equal(t, "claude-opus-4-6", claude.UpstreamThinkingModel)

	claudeThinking, ok := reg.Resolve("claude-opus-4-6-thinking")
	require.True(t, ok)
	require.Equal(t, "Claude Opus 4.6 Thinking Kiro", claudeThinking.DisplayName)
	require.Equal(t, "kiro_claude_thinking", claudeThinking.ProviderVariant)
	require.Equal(t, "high", claudeThinking.DefaultReasoningLevel)
	require.Equal(t, []string{"low", "high", "xhigh"}, claudeThinking.SupportedReasoningLevels)

	claudeMax, ok := reg.Resolve("claude-opus-4-6-max")
	require.True(t, ok)
	require.Equal(t, "Claude Opus 4.6 Max", claudeMax.DisplayName)
	require.Equal(t, "claude_code_max", claudeMax.ProviderVariant)
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

func TestCodexGatewayModelRegistry_ExportCodexCLICatalogJSON(t *testing.T) {
	type codexCLICatalogModelForTest struct {
		Slug                      string   `json:"slug"`
		Visibility                string   `json:"visibility"`
		ShellType                 string   `json:"shell_type"`
		WebSearchToolType         string   `json:"web_search_tool_type,omitempty"`
		InputModalities           []string `json:"input_modalities"`
		SupportsSearchTool        bool     `json:"supports_search_tool"`
		SupportsParallelToolCalls bool     `json:"supports_parallel_tool_calls"`
		SupportedReasoningLevels  []struct {
			Effort      string `json:"effort"`
			Description string `json:"description"`
		} `json:"supported_reasoning_levels"`
		BaseInstructions              string `json:"base_instructions"`
		ContextWindow                 int    `json:"context_window,omitempty"`
		MaxContextWindow              int    `json:"max_context_window,omitempty"`
		EffectiveContextWindowPercent int    `json:"effective_context_window_percent,omitempty"`
	}

	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"gpt-5.5", "deepseek-v4-pro"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderOpenAI: {
						Provider: CodexGatewayProviderOpenAI,
						GroupID:  1001,
						Healthy:  true,
					},
					CodexGatewayProviderDeepSeek: {
						Provider: CodexGatewayProviderDeepSeek,
						GroupID:  2002,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"deepseek-v4-pro": {Enabled: true},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{"deepseek-v4-pro": true}}),
	)

	raw, err := reg.ExportCodexCLICatalogJSON()
	require.NoError(t, err)

	var envelope struct {
		Models []codexCLICatalogModelForTest `json:"models"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	require.Len(t, envelope.Models, 2)

	bySlug := make(map[string]codexCLICatalogModelForTest, len(envelope.Models))
	for _, model := range envelope.Models {
		bySlug[model.Slug] = model
		require.Equal(t, "list", model.Visibility)
		require.Equal(t, "shell_command", model.ShellType)
		require.NotEmpty(t, model.BaseInstructions)
		require.NotEmpty(t, model.SupportedReasoningLevels)
		require.NotEmpty(t, model.SupportedReasoningLevels[0].Description)
	}
	require.Contains(t, bySlug, "gpt-5.5")
	require.Contains(t, bySlug, "deepseek-v4-pro")

	gpt55 := bySlug["gpt-5.5"]
	require.Equal(t, 1_050_000, gpt55.ContextWindow)
	require.Equal(t, 92, gpt55.EffectiveContextWindowPercent)

	deepseek := envelope.Models[1]
	require.Equal(t, "deepseek-v4-pro", deepseek.Slug)
	require.True(t, deepseek.SupportsSearchTool)
	require.NotEmpty(t, deepseek.WebSearchToolType)
	require.Equal(t, []string{"text", "image"}, deepseek.InputModalities)
	require.False(t, deepseek.SupportsParallelToolCalls)
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

func TestCodexGatewayModelRegistry_AnthropicVisibleWhenProviderGroupIsHealthy(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"claude-opus-4-7", "claude-opus-4-7-thinking", "claude-opus-4-7-ag", "claude-opus-4-7-thinking-ag", "claude-opus-4-7-max", "claude-opus-4-6", "claude-opus-4-6-thinking", "claude-opus-4-6-ag", "claude-opus-4-6-thinking-ag", "claude-opus-4-6-max", "claude-sonnet-4-6", "claude-sonnet-4-6-thinking", "claude-sonnet-4-6-ag", "claude-sonnet-4-6-thinking-ag", "claude-sonnet-4-6-max", "claude-haiku-4-5-20251001", "claude-haiku-4-5-20251001-thinking", "claude-haiku-4-5-20251001-ag", "claude-haiku-4-5-20251001-thinking-ag", "claude-haiku-4-5-20251001-max"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderAnthropic: {
						Provider: CodexGatewayProviderAnthropic,
						GroupID:  3003,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"claude-opus-4-7":                       {Enabled: true},
					"claude-opus-4-7-thinking":              {Enabled: true},
					"claude-opus-4-7-ag":                    {Enabled: true},
					"claude-opus-4-7-thinking-ag":           {Enabled: true},
					"claude-opus-4-7-max":                   {Enabled: true},
					"claude-opus-4-6":                       {Enabled: true},
					"claude-opus-4-6-thinking":              {Enabled: true},
					"claude-opus-4-6-ag":                    {Enabled: true},
					"claude-opus-4-6-thinking-ag":           {Enabled: true},
					"claude-opus-4-6-max":                   {Enabled: true},
					"claude-sonnet-4-6":                     {Enabled: true},
					"claude-sonnet-4-6-thinking":            {Enabled: true},
					"claude-sonnet-4-6-ag":                  {Enabled: true},
					"claude-sonnet-4-6-thinking-ag":         {Enabled: true},
					"claude-sonnet-4-6-max":                 {Enabled: true},
					"claude-haiku-4-5-20251001":             {Enabled: true},
					"claude-haiku-4-5-20251001-thinking":    {Enabled: true},
					"claude-haiku-4-5-20251001-ag":          {Enabled: true},
					"claude-haiku-4-5-20251001-thinking-ag": {Enabled: true},
					"claude-haiku-4-5-20251001-max":         {Enabled: true},
				},
			},
		}),
		WithCodexGatewayVariantReadyChecker(codexGatewayVariantReadyCheckerStub{
			ready: map[string]bool{
				"claude-opus-4-7-ag":                    true,
				"claude-opus-4-7-thinking":              true,
				"claude-opus-4-7-thinking-ag":           true,
				"claude-opus-4-7-max":                   false,
				"claude-opus-4-6-ag":                    true,
				"claude-opus-4-6-thinking":              true,
				"claude-opus-4-6-thinking-ag":           true,
				"claude-opus-4-6-max":                   false,
				"claude-sonnet-4-6-ag":                  true,
				"claude-sonnet-4-6-thinking":            true,
				"claude-sonnet-4-6-thinking-ag":         true,
				"claude-sonnet-4-6-max":                 false,
				"claude-haiku-4-5-20251001-ag":          true,
				"claude-haiku-4-5-20251001-thinking":    true,
				"claude-haiku-4-5-20251001-thinking-ag": true,
				"claude-haiku-4-5-20251001-max":         false,
			},
		}),
	)

	require.Equal(t, []string{"claude-opus-4-7", "claude-opus-4-7-thinking", "claude-opus-4-7-ag", "claude-opus-4-7-thinking-ag", "claude-opus-4-6", "claude-opus-4-6-thinking", "claude-opus-4-6-ag", "claude-opus-4-6-thinking-ag", "claude-sonnet-4-6", "claude-sonnet-4-6-thinking", "claude-sonnet-4-6-ag", "claude-sonnet-4-6-thinking-ag", "claude-haiku-4-5-20251001", "claude-haiku-4-5-20251001-thinking", "claude-haiku-4-5-20251001-ag", "claude-haiku-4-5-20251001-thinking-ag"}, codexGatewayModelSlugs(reg.Models()))
	thinking, ok := reg.Resolve("claude-opus-4-7-thinking")
	require.True(t, ok)
	require.Equal(t, "high", thinking.DefaultReasoningLevel)
	require.True(t, thinking.SupportedInAPI)
	require.Equal(t, "Claude Opus 4.7 Thinking Kiro", thinking.DisplayName)

	maxModel, ok := reg.Resolve("claude-opus-4-7-max")
	require.True(t, ok)
	require.False(t, maxModel.SupportedInAPI)
	require.Equal(t, "hidden", maxModel.Visibility)
}

func TestCodexGatewayModelRegistry_HidesAnthropicThinkingVariantWhenVariantCheckerRejectsIt(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"claude-opus-4-6", "claude-opus-4-6-thinking", "claude-opus-4-6-thinking-ag"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderAnthropic: {
						Provider: CodexGatewayProviderAnthropic,
						GroupID:  3003,
						Healthy:  true,
					},
				},
				Models: map[string]CodexGatewayModelMutation{
					"claude-opus-4-6":             {Enabled: true},
					"claude-opus-4-6-thinking":    {Enabled: true},
					"claude-opus-4-6-thinking-ag": {Enabled: true},
				},
			},
		}),
		WithCodexGatewayVariantReadyChecker(codexGatewayVariantReadyCheckerStub{
			ready: map[string]bool{
				"claude-opus-4-6-thinking-ag": false,
			},
		}),
	)

	require.Equal(t, []string{"claude-opus-4-6", "claude-opus-4-6-thinking"}, codexGatewayModelSlugs(reg.Models()))
	model, ok := reg.Resolve("claude-opus-4-6-thinking-ag")
	require.True(t, ok)
	require.False(t, model.SupportedInAPI)
	require.Equal(t, "hidden", model.Visibility)
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

type codexGatewayVariantReadyCheckerStub struct {
	ready map[string]bool
}

func (s codexGatewayVariantReadyCheckerStub) IsReady(_ context.Context, model CodexGatewayModel, _ CodexGatewayProviderRuntime) bool {
	if len(s.ready) == 0 {
		return true
	}
	ready, ok := s.ready[model.Slug]
	if !ok {
		return true
	}
	return ready
}
