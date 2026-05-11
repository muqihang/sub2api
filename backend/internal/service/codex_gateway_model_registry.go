package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type CodexGatewayModelRegistry struct {
	fallback        config.GatewayCodexConfig
	orderedSlugs    []string
	stateSource     CodexGatewayRegistryStateSource
	pricingChecker  CodexGatewayPricingReadyChecker
	protocolChecker CodexGatewayProtocolReadyChecker
}

func NewDefaultCodexGatewayModelRegistry() *CodexGatewayModelRegistry {
	return NewCodexGatewayModelRegistry(config.GatewayCodexConfig{
		EnabledModels: defaultCodexGatewayEnabledModelSlugs(),
		ProviderGroups: config.GatewayCodexProviderGroupsConfig{
			OpenAI: 1,
		},
	})
}

type CodexGatewayRegistryStateSource interface {
	LoadCodexGatewayRegistryState(ctx context.Context) (*CodexGatewayRegistryState, error)
}

type CodexGatewayModelRegistryOption func(*CodexGatewayModelRegistry)

type CodexGatewayRegistryState struct {
	ProviderGroups map[CodexGatewayProvider]CodexGatewayProviderRuntime `json:"provider_groups"`
	Models         map[string]CodexGatewayModelMutation                 `json:"models"`
}

type codexGatewayRegistryComputedState struct {
	models []CodexGatewayModel
	index  map[string]CodexGatewayModel
}

func WithCodexGatewayRegistryStateSource(source CodexGatewayRegistryStateSource) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.stateSource = source
	}
}

func WithCodexGatewayPricingReadyChecker(checker CodexGatewayPricingReadyChecker) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.pricingChecker = checker
	}
}

func WithCodexGatewayProtocolReadyChecker(checker CodexGatewayProtocolReadyChecker) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.protocolChecker = checker
	}
}

func NewCodexGatewayModelRegistry(cfg config.GatewayCodexConfig, options ...CodexGatewayModelRegistryOption) *CodexGatewayModelRegistry {
	if len(cfg.EnabledModels) == 0 {
		cfg.EnabledModels = defaultCodexGatewayEnabledModelSlugs()
	}

	base := defaultCodexGatewayModels()
	orderedSlugs := make([]string, 0, len(base))
	for _, model := range base {
		orderedSlugs = append(orderedSlugs, model.Slug)
	}

	registry := &CodexGatewayModelRegistry{
		fallback:        cfg,
		orderedSlugs:    orderedSlugs,
		pricingChecker:  defaultCodexGatewayPricingReadyChecker,
		protocolChecker: defaultCodexGatewayProtocolReadyChecker,
	}
	for _, option := range options {
		if option != nil {
			option(registry)
		}
	}
	return registry
}

func (r *CodexGatewayModelRegistry) enabledSlugs() map[string]struct{} {
	base := defaultCodexGatewayModels()
	enabled := make(map[string]struct{}, len(base))
	cfg := r.fallback
	if len(cfg.EnabledModels) == 0 {
		for _, model := range base {
			enabled[model.Slug] = struct{}{}
		}
	} else {
		for _, modelID := range cfg.EnabledModels {
			modelID = strings.TrimSpace(modelID)
			if modelID == "" {
				continue
			}
			enabled[modelID] = struct{}{}
		}
	}
	return enabled
}

func (r *CodexGatewayModelRegistry) computeState() codexGatewayRegistryComputedState {
	state := codexGatewayRegistryComputedState{
		models: []CodexGatewayModel{},
		index:  make(map[string]CodexGatewayModel),
	}
	if r == nil {
		return state
	}

	registryState := &CodexGatewayRegistryState{
		ProviderGroups: buildFallbackCodexGatewayProviderGroups(r.fallback),
		Models:         buildFallbackCodexGatewayModelMutations(r.fallback),
	}
	if r.stateSource != nil {
		if loaded, err := r.stateSource.LoadCodexGatewayRegistryState(context.Background()); err == nil && loaded != nil {
			registryState = &CodexGatewayRegistryState{
				ProviderGroups: mergeCodexGatewayProviderGroups(buildFallbackCodexGatewayProviderGroups(r.fallback), loaded.ProviderGroups),
				Models:         mergeCodexGatewayModelMutations(buildFallbackCodexGatewayModelMutations(r.fallback), loaded.Models),
			}
		}
	}

	enabled := r.enabledSlugs()
	for _, model := range defaultCodexGatewayModels() {
		if _, ok := enabled[model.Slug]; !ok {
			continue
		}
		provider := normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider))
		mutation := registryState.Models[model.Slug]
		model = codexGatewayApplyVisibilityGates(
			model,
			mutation,
			registryState.ProviderGroups[provider],
			r.pricingChecker,
			r.protocolChecker,
		)
		state.models = append(state.models, model)
		state.index[model.Slug] = model
	}

	return state
}

func (r *CodexGatewayModelRegistry) AllModels() []CodexGatewayModel {
	state := r.computeState()
	models := make([]CodexGatewayModel, len(state.models))
	copy(models, state.models)
	return models
}

func (r *CodexGatewayModelRegistry) Models() []CodexGatewayModel {
	state := r.computeState()
	models := make([]CodexGatewayModel, 0, len(state.models))
	for _, model := range state.models {
		if !model.SupportedInAPI || model.Visibility != "visible" {
			continue
		}
		models = append(models, model)
	}
	return models
}

func (r *CodexGatewayModelRegistry) Resolve(slug string) (CodexGatewayModel, bool) {
	model, ok := r.computeState().index[strings.TrimSpace(slug)]
	return model, ok
}

func (r *CodexGatewayModelRegistry) ModelsResponse() CodexGatewayModelsResponse {
	return CodexGatewayModelsResponse{Models: r.Models()}
}

func (r *CodexGatewayModelRegistry) ExportCatalogJSON() ([]byte, error) {
	return json.MarshalIndent(r.ModelsResponse(), "", "  ")
}

func defaultCodexGatewayModels() []CodexGatewayModel {
	openAIModel := func(slug, displayName string, priority int) CodexGatewayModel {
		return CodexGatewayModel{
			Slug:                        slug,
			DisplayName:                 displayName,
			Provider:                    "openai",
			UpstreamModel:               slug,
			Visibility:                  "visible",
			SupportedInAPI:              true,
			Priority:                    priority,
			DefaultReasoningLevel:       "high",
			SupportedReasoningLevels:    []string{"medium", "high", "xhigh"},
			SupportVerbosity:            true,
			SupportsParallelToolCalls:   true,
			ContextWindow:               400000,
			AutoCompactTokenLimit:       300000,
			MaxOutputTokens:             128000,
			InputModalities:             []string{"text"},
			SupportsImageDetailOriginal: true,
			SupportsSearchTool:          true,
			ExperimentalSupportedTools:  []string{"function", "namespace", "custom"},
			ShellType:                   "local",
			WebSearchToolType:           "openai",
			ImageGenerationToolType:     "openai",
		}
	}

	deepSeekModel := func(slug, displayName string, priority int) CodexGatewayModel {
		return CodexGatewayModel{
			Slug:                        slug,
			DisplayName:                 displayName,
			Provider:                    "deepseek",
			UpstreamModel:               slug,
			Visibility:                  "hidden",
			SupportedInAPI:              false,
			Priority:                    priority,
			DefaultReasoningLevel:       "xhigh",
			SupportedReasoningLevels:    []string{"high", "xhigh"},
			SupportVerbosity:            false,
			SupportsParallelToolCalls:   false,
			ContextWindow:               1_000_000,
			AutoCompactTokenLimit:       850_000,
			MaxOutputTokens:             384_000,
			InputModalities:             []string{"text"},
			SupportsImageDetailOriginal: false,
			SupportsSearchTool:          false,
			ExperimentalSupportedTools:  []string{"function", "namespace", "custom"},
			ShellType:                   "local",
			WebSearchToolType:           "none",
			ImageGenerationToolType:     "none",
		}
	}

	return []CodexGatewayModel{
		openAIModel("gpt-5.5", "GPT-5.5", 100),
		openAIModel("gpt-5.4", "GPT-5.4", 90),
		openAIModel("gpt-5.4-mini", "GPT-5.4 Mini", 80),
		openAIModel("gpt-5.3-codex", "GPT-5.3 Codex", 70),
		deepSeekModel("deepseek-v4-pro", "DeepSeek V4 Pro", 60),
		deepSeekModel("deepseek-v4-flash", "DeepSeek V4 Flash", 50),
	}
}

func codexGatewayApplyVisibilityGates(model CodexGatewayModel, mutation CodexGatewayModelMutation, providerRuntime CodexGatewayProviderRuntime, pricingChecker CodexGatewayPricingReadyChecker, protocolChecker CodexGatewayProtocolReadyChecker) CodexGatewayModel {
	out := model
	if !mutation.Enabled || codexGatewayModelExplicitlyHidden(mutation) {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	if providerRuntime.GroupID <= 0 || !providerRuntime.Healthy {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	if !codexGatewayIsDeepSeekModel(out) {
		return out
	}
	if pricingChecker == nil || !pricingChecker.HasPricing(out.Slug) {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	if protocolChecker == nil || !protocolChecker.IsReady(out.Slug) {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	out.Visibility = "visible"
	out.SupportedInAPI = true
	return out
}

func codexGatewayModelExplicitlyHidden(mutation CodexGatewayModelMutation) bool {
	return normalizeCodexGatewaySmokeStatus(mutation.SmokeStatus) == CodexGatewaySmokeStatusFailed
}

func codexGatewayIsDeepSeekModel(model CodexGatewayModel) bool {
	return normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider)) == CodexGatewayProviderDeepSeek
}

func mergeCodexGatewayProviderGroups(base, override map[CodexGatewayProvider]CodexGatewayProviderRuntime) map[CodexGatewayProvider]CodexGatewayProviderRuntime {
	out := make(map[CodexGatewayProvider]CodexGatewayProviderRuntime, len(base)+len(override))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func mergeCodexGatewayModelMutations(base, override map[string]CodexGatewayModelMutation) map[string]CodexGatewayModelMutation {
	out := make(map[string]CodexGatewayModelMutation, len(base)+len(override))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}
