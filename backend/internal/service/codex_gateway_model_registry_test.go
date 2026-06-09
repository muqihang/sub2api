package service

import (
	"context"
	"encoding/json"
	"sort"
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
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
		"agnes-2.0-flash",
		"agnes-1.5-flash",
	}, codexGatewayModelSlugs(models))

	gpt55, ok := reg.Resolve("gpt-5.5")
	require.True(t, ok)
	require.Equal(t, "visible", gpt55.Visibility)
	require.Equal(t, 272_000, gpt55.ContextWindow)
	require.Equal(t, 244_800, gpt55.AutoCompactTokenLimit)
	require.Equal(t, 95, gpt55.EffectiveContextWindowPercent)

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
	require.False(t, pro.Capabilities.ImageInput)

	flash, ok := reg.Resolve("deepseek-v4-flash")
	require.True(t, ok)
	require.Equal(t, "hidden", flash.Visibility)
	require.Equal(t, "xhigh", flash.DefaultReasoningLevel)
	require.False(t, flash.SupportsParallelToolCalls)

	claude, ok := reg.Resolve("claude-opus-4-8")
	require.True(t, ok)
	require.Equal(t, "anthropic", claude.Provider)
	require.Equal(t, "anthropic_direct", claude.ProviderVariant)
	require.Equal(t, "Claude Opus 4.8", claude.DisplayName)
	require.Equal(t, "hidden", claude.Visibility)
	require.False(t, claude.SupportedInAPI)
	require.Equal(t, 1_000_000, claude.ContextWindow)
	require.Equal(t, 850_000, claude.AutoCompactTokenLimit)
	require.Equal(t, 64_000, claude.MaxOutputTokens)
	require.Equal(t, "high", claude.DefaultReasoningLevel)
	require.Equal(t, []string{"low", "high", "xhigh"}, claude.SupportedReasoningLevels)
	require.Equal(t, "claude-opus-4-8", claude.UpstreamBaseModel)
	require.Equal(t, "claude-opus-4-8-thinking", claude.UpstreamThinkingModel)

	_, ok = reg.Resolve("claude-opus-4-6")
	require.False(t, ok)
	_, ok = reg.Resolve("claude-opus-4-7-thinking")
	require.False(t, ok)
	_, ok = reg.Resolve("claude-opus-4-7-max")
	require.False(t, ok)

	agnes, ok := reg.Resolve("agnes-2.0-flash")
	require.True(t, ok)
	require.Equal(t, "agnes", agnes.Provider)
	require.Equal(t, "Agnes 2.0 Flash", agnes.DisplayName)
	require.Equal(t, "agnes-2.0-flash", agnes.UpstreamModel)
	require.Equal(t, "hidden", agnes.Visibility)
	require.False(t, agnes.SupportedInAPI)
	require.Equal(t, "medium", agnes.DefaultReasoningLevel)
	require.Equal(t, []string{"none", "low", "medium", "high"}, agnes.SupportedReasoningLevels)
	require.True(t, agnes.SupportsParallelToolCalls)
	require.Equal(t, []string{"text", "image"}, agnes.InputModalities)
	require.True(t, agnes.SupportsImageDetailOriginal)
	require.True(t, agnes.SupportsSearchTool)
	require.Equal(t, "openai", agnes.WebSearchToolType)
	require.Equal(t, "none", agnes.ImageGenerationToolType)
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

func TestCodexGatewayModelRegistry_VisibleCatalogExcludesHiddenDeepSeekAndAgnesModels(t *testing.T) {
	reg := NewDefaultCodexGatewayModelRegistry()

	require.Equal(t, []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
	}, codexGatewayModelSlugs(reg.Models()))
}

func TestCodexGatewayModelRegistry_DefaultCatalogOnlyExposesUnsuffixedClaudeModels(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			ProviderGroups: config.GatewayCodexProviderGroupsConfig{
				OpenAI:    1001,
				DeepSeek:  2002,
				Anthropic: 3003,
			},
		},
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{
			"deepseek-v4-pro":   true,
			"deepseek-v4-flash": true,
		}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{
			"deepseek-v4-pro":   true,
			"deepseek-v4-flash": true,
		}}),
	)

	var claudeSlugs []string
	for _, model := range reg.Models() {
		if model.Provider != "anthropic" {
			continue
		}
		claudeSlugs = append(claudeSlugs, model.Slug)
		require.NotContains(t, model.DisplayName, "Kiro")
		require.NotContains(t, model.DisplayName, "Thinking")
		require.NotContains(t, model.DisplayName, "AG")
		require.NotContains(t, model.DisplayName, "Max")
	}

	require.Equal(t, []string{
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	}, claudeSlugs)
	_, ok := reg.Resolve("claude-opus-4-7-thinking")
	require.False(t, ok)
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
		AutoCompactTokenLimit         int    `json:"auto_compact_token_limit,omitempty"`
		MaxContextWindow              int    `json:"max_context_window,omitempty"`
		EffectiveContextWindowPercent int    `json:"effective_context_window_percent,omitempty"`
		SupportsWebsockets            *bool  `json:"supports_websockets,omitempty"`
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
		require.Equal(t, "local", model.ShellType)
		require.NotEmpty(t, model.BaseInstructions)
		require.Contains(t, model.BaseInstructions, "You are Codex, based on GPT-5.")
		require.Contains(t, model.BaseInstructions, "`rg`")
		require.Contains(t, model.BaseInstructions, "Try to use `edit`")
		require.Contains(t, model.BaseInstructions, "For multi-line file creation or rewrites")
		require.Nil(t, model.SupportsWebsockets, "Codex Gateway catalog must not advertise WS until full WS v2 support is implemented")
		require.NotEmpty(t, model.SupportedReasoningLevels)
		require.NotEmpty(t, model.SupportedReasoningLevels[0].Description)
	}
	require.Contains(t, bySlug, "gpt-5.5")
	require.Contains(t, bySlug, "deepseek-v4-pro")

	gpt55 := bySlug["gpt-5.5"]
	require.Equal(t, 272_000, gpt55.ContextWindow)
	require.Equal(t, 244_800, gpt55.AutoCompactTokenLimit)
	require.Equal(t, 95, gpt55.EffectiveContextWindowPercent)
	require.NotContains(t, gpt55.BaseInstructions, "skills, plugins, MCP servers, or tool routing guidance")

	deepseek := envelope.Models[1]
	require.Equal(t, "deepseek-v4-pro", deepseek.Slug)
	require.True(t, deepseek.SupportsSearchTool)
	require.Equal(t, "text_and_image", deepseek.WebSearchToolType)
	require.Equal(t, []string{"text", "image"}, deepseek.InputModalities)
	require.False(t, deepseek.SupportsParallelToolCalls)
	require.Contains(t, deepseek.BaseInstructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.Contains(t, deepseek.BaseInstructions, "clearly matches")
	require.Contains(t, deepseek.BaseInstructions, "MUST read the matching SKILL.md")
	require.Contains(t, deepseek.BaseInstructions, "Do not load unrelated skills")
}

func TestCodexGatewayModelRegistry_ExportCodexCLICatalogJSONPromotesDeepSeekForSpawnAgentModelOverrides(t *testing.T) {
	type codexCLICatalogModelForTest struct {
		Slug     string `json:"slug"`
		Priority int    `json:"priority"`
	}

	readyModels := []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
		"claude-opus-4-6",
		"claude-opus-4-6-thinking",
		"claude-sonnet-4-6",
		"claude-sonnet-4-6-thinking",
		"claude-haiku-4-5-20251001",
		"claude-haiku-4-5-20251001-thinking",
	}
	mutations := make(map[string]CodexGatewayModelMutation, len(readyModels))
	pricingReady := make(map[string]bool, len(readyModels))
	protocolReady := make(map[string]bool, len(readyModels))
	for _, slug := range readyModels {
		mutations[slug] = CodexGatewayModelMutation{Enabled: true}
		pricingReady[slug] = true
		protocolReady[slug] = true
	}

	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{EnabledModels: readyModels},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderOpenAI:    {Provider: CodexGatewayProviderOpenAI, GroupID: 1001, Healthy: true},
					CodexGatewayProviderDeepSeek:  {Provider: CodexGatewayProviderDeepSeek, GroupID: 2002, Healthy: true},
					CodexGatewayProviderAnthropic: {Provider: CodexGatewayProviderAnthropic, GroupID: 3003, Healthy: true},
				},
				Models: mutations,
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: pricingReady}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: protocolReady}),
	)

	raw, err := reg.ExportCodexCLICatalogJSON()
	require.NoError(t, err)

	var envelope struct {
		Models []codexCLICatalogModelForTest `json:"models"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	require.GreaterOrEqual(t, len(envelope.Models), 6)

	// Codex core sorts ModelPreset by ascending priority before `spawn_agent` keeps only
	// the first 5 picker-visible model overrides. DeepSeek must survive that native cap.
	sorted := append([]codexCLICatalogModelForTest(nil), envelope.Models...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })
	firstFive := []string{}
	for _, model := range sorted[:5] {
		firstFive = append(firstFive, model.Slug)
	}
	require.Contains(t, firstFive, "deepseek-v4-pro")
	require.Contains(t, firstFive, "deepseek-v4-flash")
}

func TestCodexGatewayModelRegistry_ExportCodexCLICatalogJSONAddsRoutingBridgeForAnthropic(t *testing.T) {
	type codexCLICatalogModelForTest struct {
		Slug             string `json:"slug"`
		BaseInstructions string `json:"base_instructions"`
	}

	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"gpt-5.5", "claude-opus-4-7"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderOpenAI:    {Provider: CodexGatewayProviderOpenAI, GroupID: 1001, Healthy: true},
					CodexGatewayProviderAnthropic: {Provider: CodexGatewayProviderAnthropic, GroupID: 3003, Healthy: true},
				},
				Models: map[string]CodexGatewayModelMutation{
					"gpt-5.5":         {Enabled: true},
					"claude-opus-4-7": {Enabled: true},
				},
			},
		}),
		WithCodexGatewayPricingReadyChecker(codexGatewayPricingReadyCheckerStub{ready: map[string]bool{
			"gpt-5.5":         true,
			"claude-opus-4-7": true,
		}}),
		WithCodexGatewayProtocolReadyChecker(codexGatewayProtocolReadyCheckerStub{ready: map[string]bool{
			"gpt-5.5":         true,
			"claude-opus-4-7": true,
		}}),
	)

	raw, err := reg.ExportCodexCLICatalogJSON()
	require.NoError(t, err)

	var envelope struct {
		Models []codexCLICatalogModelForTest `json:"models"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	bySlug := make(map[string]codexCLICatalogModelForTest, len(envelope.Models))
	for _, model := range envelope.Models {
		bySlug[model.Slug] = model
	}
	require.NotContains(t, bySlug["gpt-5.5"].BaseInstructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.Contains(t, bySlug["claude-opus-4-7"].BaseInstructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.Contains(t, bySlug["claude-opus-4-7"].BaseInstructions, "MUST read the matching SKILL.md")
	require.Contains(t, bySlug["claude-opus-4-7"].BaseInstructions, "Do not load unrelated skills")
}

func TestCodexGatewayModelRegistry_ExportCodexCLICatalogJSONAddsRoutingBridgeForAgnes(t *testing.T) {
	type codexCLICatalogModelForTest struct {
		Slug             string `json:"slug"`
		BaseInstructions string `json:"base_instructions"`
	}

	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"agnes-2.0-flash"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderAgnes: {Provider: CodexGatewayProviderAgnes, GroupID: 4004, Healthy: true},
				},
				Models: map[string]CodexGatewayModelMutation{
					"agnes-2.0-flash": {Enabled: true},
				},
			},
		}),
	)

	raw, err := reg.ExportCodexCLICatalogJSON()
	require.NoError(t, err)

	var envelope struct {
		Models []codexCLICatalogModelForTest `json:"models"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	require.Len(t, envelope.Models, 1)
	require.Equal(t, "agnes-2.0-flash", envelope.Models[0].Slug)
	require.Contains(t, envelope.Models[0].BaseInstructions, "skills, plugins, MCP servers, or tool routing guidance")
	require.Contains(t, envelope.Models[0].BaseInstructions, "MUST read the matching SKILL.md")
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
	require.Equal(t, []string{"text", "image"}, model.InputModalities)
	require.False(t, model.Capabilities.ImageInput)
	require.True(t, model.SupportsSearchTool)
	require.Equal(t, "openai", model.WebSearchToolType)
	require.Contains(t, codexGatewayModelSlugs(reg.Models()), "deepseek-v4-pro")
}

func TestCodexGatewayModelRegistry_AgnesVisibleWhenProviderGroupIsHealthy(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{
			EnabledModels: []string{"agnes-2.0-flash", "agnes-1.5-flash"},
		},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderAgnes: {Provider: CodexGatewayProviderAgnes, GroupID: 4004, Healthy: true},
				},
				Models: map[string]CodexGatewayModelMutation{
					"agnes-2.0-flash": {Enabled: true},
					"agnes-1.5-flash": {Enabled: true},
				},
			},
		}),
	)

	require.Equal(t, []string{"agnes-2.0-flash", "agnes-1.5-flash"}, codexGatewayModelSlugs(reg.Models()))

	flash20, ok := reg.Resolve("agnes-2.0-flash")
	require.True(t, ok)
	require.True(t, flash20.SupportedInAPI)
	require.Equal(t, "visible", flash20.Visibility)
	require.Equal(t, "agnes", flash20.Provider)
	require.Equal(t, "Agnes 2.0 Flash", flash20.DisplayName)
	require.Equal(t, "agnes-2.0-flash", flash20.UpstreamModel)
	require.Equal(t, []string{"none", "low", "medium", "high"}, flash20.SupportedReasoningLevels)
	require.Equal(t, []string{"text", "image"}, flash20.InputModalities)
	require.True(t, flash20.SupportsImageDetailOriginal)
	require.NotEmpty(t, flash20.ExperimentalSupportedTools)
	require.True(t, flash20.SupportsParallelToolCalls)

	flash15, ok := reg.Resolve("agnes-1.5-flash")
	require.True(t, ok)
	require.True(t, flash15.SupportedInAPI)
	require.Equal(t, []string{"text", "image"}, flash15.InputModalities)
	require.True(t, flash15.SupportsImageDetailOriginal)
	require.NotContains(t, codexGatewayModelSlugs(reg.Models()), "agnes-image-2.1-flash")
	require.NotContains(t, codexGatewayModelSlugs(reg.Models()), "agnes-video-v2.0")

	payload := reg.ModelsResponse(nil)
	require.Len(t, payload.Models, 2)
	require.True(t, payload.Models[0].Capabilities.ImageInput)
	require.True(t, payload.Models[0].Capabilities.ToolCalls)
	require.True(t, payload.Models[1].Capabilities.ImageInput)
}

func TestCodexGatewayModelRegistry_AnthropicDirectModelsExposeNativeNamesAndThinking(t *testing.T) {
	directModels := []string{
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	}
	mutations := make(map[string]CodexGatewayModelMutation, len(directModels))
	for _, slug := range directModels {
		mutations[slug] = CodexGatewayModelMutation{Enabled: true}
	}
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{EnabledModels: directModels},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderAnthropic: {Provider: CodexGatewayProviderAnthropic, GroupID: 3003, Healthy: true},
				},
				Models: mutations,
			},
		}),
	)

	require.Equal(t, directModels, codexGatewayModelSlugs(reg.Models()))
	for _, slug := range directModels {
		model, ok := reg.Resolve(slug)
		require.True(t, ok, slug)
		require.Equal(t, "anthropic", model.Provider)
		require.Equal(t, "anthropic_direct", model.ProviderVariant)
		require.NotContains(t, model.DisplayName, "Kiro")
		require.NotContains(t, model.DisplayName, "Thinking")
		require.NotContains(t, model.DisplayName, "AG")
		require.NotContains(t, model.DisplayName, "Max")
		require.Equal(t, slug, model.UpstreamModel)
		require.Equal(t, slug, model.UpstreamBaseModel)
		wantThinkingModel := slug
		switch slug {
		case "claude-opus-4-8", "claude-opus-4-7", "claude-sonnet-4-6":
			wantThinkingModel = slug + "-thinking"
		}
		require.Equal(t, wantThinkingModel, model.UpstreamThinkingModel)
		require.Equal(t, "high", model.DefaultReasoningLevel)
		require.Equal(t, []string{"low", "high", "xhigh"}, model.SupportedReasoningLevels)
		require.Equal(t, []string{"text", "image"}, model.InputModalities)
		require.True(t, model.SupportsSearchTool)
		require.Equal(t, "openai", model.WebSearchToolType)
	}
	require.Equal(t, "Claude Opus 4.8", mustCodexGatewayModel(t, reg, "claude-opus-4-8").DisplayName)
	require.Equal(t, "Claude Opus 4.7", mustCodexGatewayModel(t, reg, "claude-opus-4-7").DisplayName)
	require.Equal(t, "Claude Sonnet 4.6", mustCodexGatewayModel(t, reg, "claude-sonnet-4-6").DisplayName)
	require.Equal(t, "Claude Haiku 4.5", mustCodexGatewayModel(t, reg, "claude-haiku-4-5-20251001").DisplayName)
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

func mustCodexGatewayModel(t *testing.T, reg *CodexGatewayModelRegistry, slug string) CodexGatewayModel {
	t.Helper()
	model, ok := reg.Resolve(slug)
	require.True(t, ok, slug)
	return model
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

type codexGatewayModelPricingResolverStub struct {
	pricing map[string]*CodexGatewayModelPricing
}

func (s codexGatewayModelPricingResolverStub) ResolveCodexGatewayModelPricing(ctx context.Context, model CodexGatewayModel, groupID *int64) *CodexGatewayModelPricing {
	if s.pricing == nil {
		return nil
	}
	return s.pricing[model.Slug]
}

func TestCodexGatewayModelRegistry_ModelsResponseIncludesCapabilitiesAndPricing(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{EnabledModels: []string{"gpt-5.5"}},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderOpenAI: {Provider: CodexGatewayProviderOpenAI, GroupID: 1001, Healthy: true},
				},
			},
		}),
		WithCodexGatewayModelPricingResolver(codexGatewayModelPricingResolverStub{pricing: map[string]*CodexGatewayModelPricing{
			"gpt-5.5": {
				InputPrice:       stringPtr("2.50"),
				OutputPrice:      stringPtr("15.00"),
				CachedInputPrice: stringPtr("0.25"),
				CacheWritePrice:  stringPtr("2.50"),
				Currency:         "USD",
				Unit:             "per_1m_tokens",
				UpdatedAt:        stringPtr("2026-05-21T00:00:00Z"),
				Source:           "database_model_pricing",
			},
		}}),
	)

	payload := reg.ModelsResponse(nil)
	require.Len(t, payload.Models, 1)
	model := payload.Models[0]
	require.Equal(t, "zhumeng", model.Origin)
	require.Equal(t, "zhumeng", model.ProviderID)
	require.True(t, model.Capabilities.Responses)
	require.True(t, model.Capabilities.Streaming)
	require.True(t, model.Capabilities.ToolCalls)
	require.True(t, model.Capabilities.ImageInput)
	require.True(t, model.Capabilities.ContextContinuation)
	require.NotNil(t, model.Pricing)
	require.Equal(t, "2.50", *model.Pricing.InputPrice)
	require.Equal(t, "0.25", *model.Pricing.CachedInputPrice)
	require.Equal(t, "database_model_pricing", model.Pricing.Source)
}

func TestCodexGatewayResolvedPricingToCatalogIncludesLiteLLMBasePricing(t *testing.T) {
	pricing := codexGatewayResolvedPricingToCatalog(&ResolvedPricing{
		Mode:   BillingModeToken,
		Source: PricingSourceLiteLLM,
		BasePricing: &ModelPricing{
			InputPricePerToken:         5e-6,
			OutputPricePerToken:        30e-6,
			CacheReadPricePerToken:     0.5e-6,
			CacheCreationPricePerToken: 5e-6,
		},
	})

	require.NotNil(t, pricing)
	require.Equal(t, "5", *pricing.InputPrice)
	require.Equal(t, "30", *pricing.OutputPrice)
	require.Equal(t, "0.5", *pricing.CachedInputPrice)
	require.Equal(t, "database_model_pricing", pricing.Source)
}

func TestCodexGatewayModelRegistry_CodexCLICatalogPreservesDesktopModelContract(t *testing.T) {
	reg := NewCodexGatewayModelRegistry(
		config.GatewayCodexConfig{EnabledModels: []string{"gpt-5.5"}},
		WithCodexGatewayRegistryStateSource(&codexGatewayRegistryStateSourceStub{
			state: &CodexGatewayRegistryState{
				ProviderGroups: map[CodexGatewayProvider]CodexGatewayProviderRuntime{
					CodexGatewayProviderOpenAI: {Provider: CodexGatewayProviderOpenAI, GroupID: 1001, Healthy: true},
				},
			},
		}),
		WithCodexGatewayModelPricingResolver(codexGatewayModelPricingResolverStub{pricing: map[string]*CodexGatewayModelPricing{
			"gpt-5.5": {InputPrice: stringPtr("2.50"), Currency: "USD", Unit: "per_1m_tokens", Source: "database_model_pricing"},
		}}),
	)

	raw, err := reg.ExportCodexCLICatalogJSON(nil)
	require.NoError(t, err)
	var payload struct {
		Models []struct {
			Slug          string                        `json:"slug"`
			Origin        string                        `json:"origin"`
			ProviderID    string                        `json:"provider_id"`
			Capabilities  CodexGatewayModelCapabilities `json:"capabilities"`
			Pricing       *CodexGatewayModelPricing     `json:"pricing"`
			ContextWindow int                           `json:"context_window"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Len(t, payload.Models, 1)
	require.Equal(t, "gpt-5.5", payload.Models[0].Slug)
	require.Equal(t, "zhumeng", payload.Models[0].Origin)
	require.Equal(t, "zhumeng", payload.Models[0].ProviderID)
	require.True(t, payload.Models[0].Capabilities.ToolCalls)
	require.NotNil(t, payload.Models[0].Pricing)
	require.Equal(t, "2.50", *payload.Models[0].Pricing.InputPrice)
	require.Equal(t, 272_000, payload.Models[0].ContextWindow)
}

func TestCodexGatewayModelRegistryProviderWiresDatabasePricingResolver(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Codex.EnabledModels = []string{"gpt-5.5"}
	cfg.Gateway.Codex.ProviderGroups.OpenAI = 100
	resolver := NewModelPricingResolver(nil, NewBillingService(nil, nil))
	registry := ProvideCodexGatewayModelRegistryWithVariantChecker(
		cfg,
		NewCodexGatewayAdminService(cfg.Gateway.Codex, nil),
		nil,
		resolver,
	)

	require.NotNil(t, registry.modelPricingResolver)
}
