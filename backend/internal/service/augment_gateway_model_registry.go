package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type AugmentGatewayModelRegistry struct {
	modelsByID map[string]AugmentGatewayModel
	orderedIDs []string
	visibleIDs map[string]struct{}
}

func NewDefaultAugmentGatewayModelRegistry() *AugmentGatewayModelRegistry {
	return NewAugmentGatewayModelRegistry(config.GatewayAugmentConfig{
		Enabled:       true,
		EnabledModels: defaultAugmentGatewayEnabledModelIDs(),
	})
}

func NewAugmentGatewayModelRegistry(cfg config.GatewayAugmentConfig) *AugmentGatewayModelRegistry {
	modelsByID := make(map[string]AugmentGatewayModel, len(defaultAugmentGatewayModels))
	orderedIDs := make([]string, 0, len(defaultAugmentGatewayModels))
	visibleIDs := make(map[string]struct{}, len(cfg.EnabledModels))
	enabledIDs := make(map[string]struct{}, len(cfg.EnabledModels))

	for _, id := range cfg.EnabledModels {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		enabledIDs[id] = struct{}{}
	}

	for _, model := range defaultAugmentGatewayModels {
		model.ProviderGroupID = augmentGatewayProviderGroupID(cfg.ProviderGroups, model.Provider)
		modelsByID[model.ID] = model
		orderedIDs = append(orderedIDs, model.ID)

		if !cfg.Enabled {
			continue
		}
		if _, enabled := enabledIDs[model.ID]; !enabled {
			continue
		}
		if !augmentGatewayModelProviderConfiguredForVisibility(model) {
			continue
		}
		visibleIDs[model.ID] = struct{}{}
	}

	return &AugmentGatewayModelRegistry{
		modelsByID: modelsByID,
		orderedIDs: orderedIDs,
		visibleIDs: visibleIDs,
	}
}

func (r *AugmentGatewayModelRegistry) VisibleModels() []AugmentGatewayModel {
	if r == nil {
		return nil
	}
	models := make([]AugmentGatewayModel, 0, len(r.visibleIDs))
	for _, id := range r.orderedIDs {
		if _, ok := r.visibleIDs[id]; !ok {
			continue
		}
		models = append(models, r.modelsByID[id])
	}
	return models
}

func (r *AugmentGatewayModelRegistry) IsVisible(modelID string) bool {
	if r == nil {
		return false
	}
	_, ok := r.visibleIDs[strings.TrimSpace(modelID)]
	return ok
}

func (r *AugmentGatewayModelRegistry) Resolve(modelID string) (AugmentGatewayModel, bool) {
	if r == nil {
		return AugmentGatewayModel{}, false
	}
	model, ok := r.modelsByID[strings.TrimSpace(modelID)]
	return model, ok
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

func augmentGatewayModelProviderConfiguredForVisibility(model AugmentGatewayModel) bool {
	switch model.Provider {
	case AugmentGatewayProviderAnthropic, AugmentGatewayProviderGemini:
		return model.ProviderGroupID != 0
	default:
		return true
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
