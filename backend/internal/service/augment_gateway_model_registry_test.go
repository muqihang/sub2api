package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAugmentGatewayModelRegistry_FirstBatchVisibleModels(t *testing.T) {
	reg := NewAugmentGatewayModelRegistry(config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: defaultAugmentGatewayEnabledModelIDs(),
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI:   1001,
			DeepSeek: 1002,
		},
	})

	models := reg.VisibleModels()

	require.Equal(t, []string{
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.4-mini",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
	}, augmentGatewayModelIDs(models))
}

func TestAugmentGatewayModelRegistry_FirstBatchHiddenWithoutProviderGroups(t *testing.T) {
	reg := NewAugmentGatewayModelRegistry(config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: defaultAugmentGatewayEnabledModelIDs(),
	})

	require.True(t, reg.IsEnabled("gpt-5.4"))
	require.False(t, reg.IsVisible("gpt-5.4"))
	require.True(t, reg.IsEnabled("deepseek-v4-pro"))
	require.False(t, reg.IsVisible("deepseek-v4-pro"))
	require.Empty(t, reg.VisibleModels())
}

func TestAugmentGatewayModelRegistry_ClaudeGeminiHiddenByDefault(t *testing.T) {
	reg := NewDefaultAugmentGatewayModelRegistry()

	require.False(t, reg.IsEnabled("claude-sonnet-4-5"))
	require.False(t, reg.IsVisible("claude-sonnet-4-5"))
	require.False(t, reg.IsEnabled("gemini-2.5-pro"))
	require.False(t, reg.IsVisible("gemini-2.5-pro"))

	claude, ok := reg.Resolve("claude-sonnet-4-5")
	require.True(t, ok)
	require.Equal(t, AugmentGatewayProviderAnthropic, claude.Provider)

	gemini, ok := reg.Resolve("gemini-2.5-pro")
	require.True(t, ok)
	require.Equal(t, AugmentGatewayProviderGemini, gemini.Provider)
}

func TestAugmentGatewayModelRegistry_ClaudeGeminiEnabledWithoutProviderGroupRemainHidden(t *testing.T) {
	reg := NewAugmentGatewayModelRegistry(config.GatewayAugmentConfig{
		Enabled: true,
		EnabledModels: []string{
			"gpt-5.4",
			"claude-sonnet-4-5",
			"gemini-2.5-pro",
		},
	})

	require.True(t, reg.IsEnabled("gpt-5.4"))
	require.False(t, reg.IsVisible("gpt-5.4"))
	require.True(t, reg.IsEnabled("claude-sonnet-4-5"))
	require.False(t, reg.IsVisible("claude-sonnet-4-5"))
	require.True(t, reg.IsEnabled("gemini-2.5-pro"))
	require.False(t, reg.IsVisible("gemini-2.5-pro"))
	require.Empty(t, reg.VisibleModels())
}

func TestAugmentGatewayModelRegistry_ClaudeGeminiEnabledWithProviderGroupsBecomeVisible(t *testing.T) {
	reg := NewAugmentGatewayModelRegistry(config.GatewayAugmentConfig{
		Enabled: true,
		EnabledModels: []string{
			"gpt-5.4",
			"claude-sonnet-4-5",
			"gemini-2.5-pro",
		},
		ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
			OpenAI:    1001,
			Anthropic: 2001,
			Gemini:    2002,
		},
	})

	require.True(t, reg.IsEnabled("claude-sonnet-4-5"))
	require.True(t, reg.IsVisible("claude-sonnet-4-5"))
	require.True(t, reg.IsEnabled("gemini-2.5-pro"))
	require.True(t, reg.IsVisible("gemini-2.5-pro"))
	require.Equal(t, []string{
		"gpt-5.4",
		"claude-sonnet-4-5",
		"gemini-2.5-pro",
	}, augmentGatewayModelIDs(reg.VisibleModels()))
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
	require.False(t, reg.IsVisible("gpt-5.4"))
	require.False(t, reg.IsVisible("claude-sonnet-4-5"))
	require.False(t, reg.IsVisible("gemini-2.5-pro"))

	base.ProviderGroups.OpenAI = 1001
	reg = NewAugmentGatewayModelRegistry(base)
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

func TestAugmentGatewayModelRegistryHidesProviderMissingModels(t *testing.T) {
	reg := NewAugmentGatewayModelRegistry(
		config.GatewayAugmentConfig{
			Enabled: true,
			EnabledModels: []string{
				"gpt-5.4",
			},
			ProviderGroups: config.GatewayAugmentProviderGroupsConfig{
				OpenAI: 1001,
			},
		},
		WithAugmentGatewayRegistryStateSource(&augmentGatewayRegistryStateSourceStub{
			state: &AugmentGatewayRegistryState{
				GatewayEnabled: true,
				ProviderGroups: map[AugmentGatewayProvider]AugmentGatewayProviderRuntime{
					AugmentGatewayProviderOpenAI: {
						GroupID: 1001,
						Healthy: false,
					},
				},
				Models: map[string]AugmentGatewayModelSetting{
					"gpt-5.4": {
						Enabled:     true,
						SmokeStatus: AugmentGatewaySmokeStatusPassed,
					},
				},
			},
		}),
	)

	require.True(t, reg.IsEnabled("gpt-5.4"))
	require.False(t, reg.IsVisible("gpt-5.4"))
	require.Empty(t, reg.VisibleModels())
}

func augmentGatewayModelIDs(models []AugmentGatewayModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

type augmentGatewayRegistryStateSourceStub struct {
	state *AugmentGatewayRegistryState
	err   error
}

func (s *augmentGatewayRegistryStateSourceStub) LoadAugmentGatewayRegistryState(_ context.Context) (*AugmentGatewayRegistryState, error) {
	return s.state, s.err
}
