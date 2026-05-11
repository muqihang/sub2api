package service

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type CodexGatewayModelRegistry struct {
	models []CodexGatewayModel
	index  map[string]CodexGatewayModel
}

func NewDefaultCodexGatewayModelRegistry() *CodexGatewayModelRegistry {
	return NewCodexGatewayModelRegistry(config.GatewayCodexConfig{})
}

func NewCodexGatewayModelRegistry(cfg config.GatewayCodexConfig) *CodexGatewayModelRegistry {
	base := defaultCodexGatewayModels()
	enabled := make(map[string]struct{}, len(base))
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

	models := make([]CodexGatewayModel, 0, len(enabled))
	index := make(map[string]CodexGatewayModel, len(enabled))
	for _, model := range base {
		if _, ok := enabled[model.Slug]; !ok {
			continue
		}
		models = append(models, model)
		index[model.Slug] = model
	}

	return &CodexGatewayModelRegistry{
		models: models,
		index:  index,
	}
}

func (r *CodexGatewayModelRegistry) AllModels() []CodexGatewayModel {
	if r == nil {
		return nil
	}
	models := make([]CodexGatewayModel, len(r.models))
	copy(models, r.models)
	return models
}

func (r *CodexGatewayModelRegistry) Models() []CodexGatewayModel {
	if r == nil {
		return nil
	}
	models := make([]CodexGatewayModel, 0, len(r.models))
	for _, model := range r.models {
		if !model.SupportedInAPI || model.Visibility != "visible" {
			continue
		}
		models = append(models, model)
	}
	return models
}

func (r *CodexGatewayModelRegistry) Resolve(slug string) (CodexGatewayModel, bool) {
	if r == nil {
		return CodexGatewayModel{}, false
	}
	model, ok := r.index[strings.TrimSpace(slug)]
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
