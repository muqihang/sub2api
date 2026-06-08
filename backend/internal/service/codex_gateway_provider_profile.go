package service

import "strings"

type CodexGatewayProviderProfile struct {
	Provider                     string
	ReasoningProtocol            string
	ThinkingParamPolicy          string
	SupportedEfforts             []string
	DefaultEffort                string
	SupportsToolReasoningReplay  bool
	SupportsStructuredToolOutput bool
	SupportsImageToolOutput      bool
	CacheUsageShape              string
	SupportsOfficialUserID       bool
	SupportsPromptCacheKey       bool
	SupportsStrictTools          bool
	SupportsResponsesWS          bool
}

func CodexGatewayProviderProfileFor(model CodexGatewayModel) CodexGatewayProviderProfile {
	provider := normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider))
	switch provider {
	case CodexGatewayProviderDeepSeek:
		return CodexGatewayProviderProfile{
			Provider:                     string(CodexGatewayProviderDeepSeek),
			ReasoningProtocol:            "deepseek",
			ThinkingParamPolicy:          "deepseek_official",
			SupportedEfforts:             []string{"high", "max"},
			DefaultEffort:                "max",
			SupportsToolReasoningReplay:  true,
			SupportsStructuredToolOutput: false,
			SupportsImageToolOutput:      false,
			CacheUsageShape:              "deepseek_top_level",
			SupportsOfficialUserID:       true,
			SupportsPromptCacheKey:       false,
			SupportsStrictTools:          true,
			SupportsResponsesWS:          false,
		}
	case CodexGatewayProviderAnthropic:
		return CodexGatewayProviderProfile{
			Provider:                     string(CodexGatewayProviderAnthropic),
			ReasoningProtocol:            "anthropic",
			ThinkingParamPolicy:          "anthropic_thinking",
			SupportedEfforts:             []string{"low", "medium", "high", "max"},
			DefaultEffort:                "high",
			SupportsToolReasoningReplay:  true,
			SupportsStructuredToolOutput: true,
			SupportsImageToolOutput:      true,
			CacheUsageShape:              "anthropic_usage",
			SupportsOfficialUserID:       false,
			SupportsPromptCacheKey:       false,
			SupportsStrictTools:          false,
			SupportsResponsesWS:          false,
		}
	case CodexGatewayProviderAgnes:
		return CodexGatewayProviderProfile{
			Provider:                     string(CodexGatewayProviderAgnes),
			ReasoningProtocol:            "agnes",
			ThinkingParamPolicy:          "agnes_openai_reasoning",
			SupportedEfforts:             []string{"low", "medium", "high", "max"},
			DefaultEffort:                "high",
			SupportsToolReasoningReplay:  false,
			SupportsStructuredToolOutput: false,
			SupportsImageToolOutput:      codexGatewayStringSliceContains(model.InputModalities, "image") || model.SupportsImageDetailOriginal,
			CacheUsageShape:              "none",
			SupportsOfficialUserID:       false,
			SupportsPromptCacheKey:       true,
			SupportsStrictTools:          false,
			SupportsResponsesWS:          false,
		}
	case CodexGatewayProviderOpenAI:
		return CodexGatewayProviderProfile{
			Provider:                     string(CodexGatewayProviderOpenAI),
			ReasoningProtocol:            "openai_responses",
			ThinkingParamPolicy:          "openai_responses",
			SupportedEfforts:             []string{"low", "medium", "high", "xhigh"},
			DefaultEffort:                "medium",
			SupportsToolReasoningReplay:  true,
			SupportsStructuredToolOutput: true,
			SupportsImageToolOutput:      true,
			CacheUsageShape:              "openai_nested",
			SupportsOfficialUserID:       false,
			SupportsPromptCacheKey:       true,
			SupportsStrictTools:          true,
			SupportsResponsesWS:          false,
		}
	default:
		providerName := strings.TrimSpace(model.Provider)
		if providerName == "" {
			providerName = "generic"
		}
		return CodexGatewayProviderProfile{Provider: providerName, ReasoningProtocol: "none", CacheUsageShape: "none"}
	}
}
