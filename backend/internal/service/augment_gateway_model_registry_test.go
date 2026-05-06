package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayModelRegistry_FirstBatchVisibleModels(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()

	models := reg.VisibleModels()

	require.Equal(t, []string{
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.4-mini",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
	}, augmentGatewayModelIDs(models))
}

func TestAugmentGatewayModelRegistry_ClaudeGeminiHiddenByDefault(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()

	require.False(t, reg.IsVisible("claude-sonnet-4-5"))
	require.False(t, reg.IsVisible("gemini-2.5-pro"))

	claude, ok := reg.Resolve("claude-sonnet-4-5")
	require.True(t, ok)
	require.Equal(t, AugmentGatewayProviderAnthropic, claude.Provider)

	gemini, ok := reg.Resolve("gemini-2.5-pro")
	require.True(t, ok)
	require.Equal(t, AugmentGatewayProviderGemini, gemini.Provider)
}

func TestAugmentGatewayModelRegistry_DeepSeekDefaultsReasoningEffortMax(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()

	pro, ok := reg.Resolve("deepseek-v4-pro")
	require.True(t, ok)
	require.Equal(t, "max", pro.ReasoningEffort)

	flash, ok := reg.Resolve("deepseek-v4-flash")
	require.True(t, ok)
	require.Equal(t, "max", flash.ReasoningEffort)
}

func TestAugmentGatewayModelRegistry_ClaudeGeminiRequireEnabledModelAndProviderGroup(t *testing.T) {
	base := config.GatewayAugmentConfig{
		Enabled: true,
		EnabledModels: []string{
			"gpt-5.4",
			"claude-sonnet-4-5",
			"gemini-2.5-pro",
		},
	}

	reg := NewAugmentGatewayModelRegistry(base)
	require.True(t, reg.IsVisible("gpt-5.4"))
	require.False(t, reg.IsVisible("claude-sonnet-4-5"))
	require.False(t, reg.IsVisible("gemini-2.5-pro"))

	base.ProviderGroups.Anthropic = 2001
	reg = NewAugmentGatewayModelRegistry(base)
	require.True(t, reg.IsVisible("claude-sonnet-4-5"))
	require.False(t, reg.IsVisible("gemini-2.5-pro"))

	base.ProviderGroups.Gemini = 2002
	reg = NewAugmentGatewayModelRegistry(base)
	require.True(t, reg.IsVisible("claude-sonnet-4-5"))
	require.True(t, reg.IsVisible("gemini-2.5-pro"))
	require.Equal(t, []string{
		"gpt-5.4",
		"claude-sonnet-4-5",
		"gemini-2.5-pro",
	}, augmentGatewayModelIDs(reg.VisibleModels()))
}

func augmentGatewayModelIDs(models []AugmentGatewayModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
