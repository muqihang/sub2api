package service

import "strings"

const (
	openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel = "capability.unsupported_oauth_model_profile"
	openAIRuntimeGuardCapabilityCategoryNoCompatibleAccount   = "capability.no_compatible_account"
)

// openAIAccountSupportsRuntimeGuardCapability applies server-side OpenAI OAuth
// capability seeds that are intentionally independent of model_mapping. API-key
// accounts keep their existing mapping/passthrough behavior.
func openAIAccountSupportsRuntimeGuardCapability(account *Account, requestedModel string, imageCapability OpenAIImagesCapability) bool {
	if account == nil || !account.IsOpenAI() {
		return false
	}
	if !account.IsOpenAIOAuth() {
		return true
	}
	if !openAIOAuthAccountSupportsImageCapability(account, imageCapability) {
		return false
	}
	return openAIOAuthAccountSupportsRequestedModel(account, requestedModel, imageCapability)
}

func openAIOAuthAccountSupportsRequestedModel(account *Account, requestedModel string, imageCapability OpenAIImagesCapability) bool {
	model := strings.TrimSpace(requestedModel)
	if model == "" {
		return true
	}
	if isOpenAIImageGenerationModel(model) {
		return imageCapability == OpenAIImagesCapabilityBasic && openAIOAuthAccountSupportsImageBridge(account)
	}
	if account != nil {
		if mapped := strings.TrimSpace(account.GetMappedModel(model)); mapped != "" && mapped != model {
			model = mapped
		}
	}
	return openAIOAuthBuiltInModelSeedSupports(model)
}

func openAIOAuthBuiltInModelSeedSupports(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return true
	}
	m = strings.TrimPrefix(m, "openai/")
	m = strings.TrimPrefix(m, "models/")
	switch {
	case m == "gpt-5.5" || strings.HasPrefix(m, "gpt-5.5-"):
		return true
	case m == "gpt-5.4" || strings.HasPrefix(m, "gpt-5.4-"):
		return !strings.HasPrefix(m, "gpt-5.4-nano")
	case m == "gpt-5.3-codex" || strings.HasPrefix(m, "gpt-5.3-codex-"):
		return true
	case m == "gpt-5-codex" || strings.HasPrefix(m, "gpt-5-codex-"):
		return true
	case m == "gpt-5.1-codex" || strings.HasPrefix(m, "gpt-5.1-codex-"):
		return true
	case m == "gpt-5.1" || strings.HasPrefix(m, "gpt-5.1-"):
		return true
	case m == "gpt-5" || strings.HasPrefix(m, "gpt-5-"):
		return true
	default:
		return false
	}
}

func openAIOAuthAccountSupportsImageCapability(account *Account, capability OpenAIImagesCapability) bool {
	switch capability {
	case "":
		return true
	case OpenAIImagesCapabilityBasic:
		return openAIOAuthAccountSupportsImageBridge(account)
	case OpenAIImagesCapabilityNative:
		return false
	default:
		return false
	}
}

func openAIOAuthAccountSupportsImageBridge(account *Account) bool {
	if account == nil || !account.IsOpenAIOAuth() {
		return false
	}
	if override := account.CodexImageGenerationBridgeOverride(); override != nil {
		return *override
	}
	return true
}
