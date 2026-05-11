package service

import (
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

func codexGatewayModelSlugs(models []CodexGatewayModel) []string {
	slugs := make([]string, 0, len(models))
	for _, model := range models {
		slugs = append(slugs, model.Slug)
	}
	return slugs
}
