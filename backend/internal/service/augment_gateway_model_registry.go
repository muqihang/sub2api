package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type AugmentGatewayRegistryStateSource interface {
	LoadAugmentGatewayRegistryState(ctx context.Context) (*AugmentGatewayRegistryState, error)
}

type AugmentGatewayModelRegistryOption func(*AugmentGatewayModelRegistry)

type AugmentGatewayModelRegistry struct {
	fallback                   config.GatewayAugmentConfig
	orderedIDs                 []string
	allowMissingProviderGroups bool
	stateSource                AugmentGatewayRegistryStateSource
	pricingChecker             AugmentGatewayExplicitPricingChecker
}

type augmentGatewayRegistryComputedState struct {
	modelsByID         map[string]AugmentGatewayModel
	explicitPricingIDs map[string]struct{}
	enabledIDs         map[string]struct{}
	visibleIDs         map[string]struct{}
}

func WithAugmentGatewayRegistryStateSource(source AugmentGatewayRegistryStateSource) AugmentGatewayModelRegistryOption {
	return func(registry *AugmentGatewayModelRegistry) {
		registry.stateSource = source
	}
}

func WithAugmentGatewayExplicitPricingChecker(checker AugmentGatewayExplicitPricingChecker) AugmentGatewayModelRegistryOption {
	return func(registry *AugmentGatewayModelRegistry) {
		registry.pricingChecker = checker
	}
}

func NewDefaultAugmentGatewayModelRegistry() *AugmentGatewayModelRegistry {
	return NewAugmentGatewayModelRegistry(
		config.GatewayAugmentConfig{
			Enabled:       true,
			EnabledModels: defaultAugmentGatewayEnabledModelIDs(),
		},
		func(registry *AugmentGatewayModelRegistry) {
			registry.allowMissingProviderGroups = true
		},
	)
}

func NewAugmentGatewayModelRegistry(cfg config.GatewayAugmentConfig, options ...AugmentGatewayModelRegistryOption) *AugmentGatewayModelRegistry {
	orderedIDs := make([]string, 0, len(defaultAugmentGatewayModels))
	for _, model := range defaultAugmentGatewayModels {
		orderedIDs = append(orderedIDs, model.ID)
	}
	registry := &AugmentGatewayModelRegistry{
		fallback:                   cfg,
		orderedIDs:                 orderedIDs,
		allowMissingProviderGroups: false,
		pricingChecker:             defaultAugmentGatewayExplicitPricingChecker,
	}
	for _, option := range options {
		if option != nil {
			option(registry)
		}
	}
	return registry
}

func (r *AugmentGatewayModelRegistry) VisibleModels() []AugmentGatewayModel {
	state := r.computeState()
	models := make([]AugmentGatewayModel, 0, len(state.visibleIDs))
	for _, id := range r.orderedIDs {
		if _, ok := state.visibleIDs[id]; !ok {
			continue
		}
		models = append(models, state.modelsByID[id])
	}
	return models
}

func (r *AugmentGatewayModelRegistry) IsVisible(modelID string) bool {
	state := r.computeState()
	_, ok := state.visibleIDs[strings.TrimSpace(modelID)]
	return ok
}

func (r *AugmentGatewayModelRegistry) IsEnabled(modelID string) bool {
	state := r.computeState()
	_, ok := state.enabledIDs[strings.TrimSpace(modelID)]
	return ok
}

func (r *AugmentGatewayModelRegistry) HasExplicitPricing(modelID string) bool {
	state := r.computeState()
	_, ok := state.explicitPricingIDs[strings.TrimSpace(modelID)]
	return ok
}

func (r *AugmentGatewayModelRegistry) Resolve(modelID string) (AugmentGatewayModel, bool) {
	state := r.computeState()
	model, ok := state.modelsByID[strings.TrimSpace(modelID)]
	return model, ok
}

func (r *AugmentGatewayModelRegistry) computeState() augmentGatewayRegistryComputedState {
	state := augmentGatewayRegistryComputedState{
		modelsByID:         make(map[string]AugmentGatewayModel, len(defaultAugmentGatewayModels)),
		explicitPricingIDs: make(map[string]struct{}, len(defaultAugmentGatewayModels)),
		enabledIDs:         make(map[string]struct{}, len(defaultAugmentGatewayModels)),
		visibleIDs:         make(map[string]struct{}, len(defaultAugmentGatewayModels)),
	}
	if r == nil {
		return state
	}

	registryState := &AugmentGatewayRegistryState{
		GatewayEnabled:     r.fallback.Enabled,
		ProviderGroups:     buildFallbackAugmentGatewayProviderGroups(r.fallback),
		Models:             buildFallbackAugmentGatewayModelSettings(r.fallback),
		RoutePolicyVersion: AugmentGatewayDefaultRoutePolicyVersion,
	}
	if r.stateSource != nil {
		if loaded, err := r.stateSource.LoadAugmentGatewayRegistryState(context.Background()); err == nil && loaded != nil {
			registryState = loaded
		}
	}

	for _, model := range defaultAugmentGatewayModels {
		providerState := registryState.ProviderGroups[model.Provider]
		model.ProviderGroupID = providerState.GroupID
		state.modelsByID[model.ID] = model
		if r.pricingChecker != nil && r.pricingChecker.HasExplicitPricing(model.ID) {
			state.explicitPricingIDs[model.ID] = struct{}{}
		}

		modelSetting := normalizeAugmentGatewayModelSetting(registryState.Models[model.ID])
		if !registryState.GatewayEnabled || !modelSetting.Enabled {
			continue
		}
		if _, ok := state.explicitPricingIDs[model.ID]; !ok {
			continue
		}
		state.enabledIDs[model.ID] = struct{}{}

		if modelSetting.SmokeStatus != AugmentGatewaySmokeStatusPassed {
			continue
		}
		if !r.allowMissingProviderGroups {
			if providerState.GroupID == 0 || !providerState.Healthy {
				continue
			}
		}
		state.visibleIDs[model.ID] = struct{}{}
	}
	return state
}

func defaultAugmentGatewayEnabledModelIDs() []string {
	return []string{
		"gpt-5.4",
		"gpt-5.5",
		"gpt-5.4-mini",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
	}
}

func augmentGatewayProviderGroupID(groups config.GatewayAugmentProviderGroupsConfig, provider AugmentGatewayProvider) int64 {
	switch provider {
	case AugmentGatewayProviderOpenAI:
		return groups.OpenAI
	case AugmentGatewayProviderDeepSeek:
		return groups.DeepSeek
	case AugmentGatewayProviderAnthropic:
		return groups.Anthropic
	case AugmentGatewayProviderGemini:
		return groups.Gemini
	default:
		return 0
	}
}

var defaultAugmentGatewayModels = []AugmentGatewayModel{
	{
		ID:            "gpt-5.4",
		Provider:      AugmentGatewayProviderOpenAI,
		UpstreamModel: "gpt-5.4",
	},
	{
		ID:            "gpt-5.5",
		Provider:      AugmentGatewayProviderOpenAI,
		UpstreamModel: "gpt-5.5",
	},
	{
		ID:            "gpt-5.4-mini",
		Provider:      AugmentGatewayProviderOpenAI,
		UpstreamModel: "gpt-5.4-mini",
	},
	{
		ID:              "deepseek-v4-pro",
		Provider:        AugmentGatewayProviderDeepSeek,
		UpstreamModel:   "deepseek-v4-pro",
		ReasoningEffort: "max",
	},
	{
		ID:              "deepseek-v4-flash",
		Provider:        AugmentGatewayProviderDeepSeek,
		UpstreamModel:   "deepseek-v4-flash",
		ReasoningEffort: "max",
	},
	{
		ID:            "claude-sonnet-4-5",
		Provider:      AugmentGatewayProviderAnthropic,
		UpstreamModel: "claude-sonnet-4-5",
	},
	{
		ID:            "gemini-2.5-pro",
		Provider:      AugmentGatewayProviderGemini,
		UpstreamModel: "gemini-2.5-pro",
	},
}
