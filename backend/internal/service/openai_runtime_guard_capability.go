package service

import (
	"errors"
	"fmt"
	"strings"
)

type OpenAIRuntimeGuardErrorCode string

const (
	OpenAIRuntimeGuardErrorCodeLocalPolicyBlock           OpenAIRuntimeGuardErrorCode = "local_policy_block"
	OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability OpenAIRuntimeGuardErrorCode = "unsupported_oauth_capability"
	OpenAIRuntimeGuardErrorCodeNoCompatibleAccount        OpenAIRuntimeGuardErrorCode = "no_compatible_account"

	openAIRuntimeGuardCapabilityCategoryLocalPolicyBlock      = "capability.local_policy_block"
	openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel = "capability.unsupported_oauth_model_profile"
	openAIRuntimeGuardCapabilityCategoryNoCompatibleAccount   = "capability.no_compatible_account"
)

type OpenAIRuntimeGuardSelectionError struct {
	Code     OpenAIRuntimeGuardErrorCode
	Category string
	Metadata map[string]string
	Message  string
	cause    error
}

func (e *OpenAIRuntimeGuardSelectionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return string(e.Code)
}

func (e *OpenAIRuntimeGuardSelectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *OpenAIRuntimeGuardSelectionError) Is(target error) bool {
	if e == nil {
		return false
	}
	if e.cause != nil && errors.Is(e.cause, target) {
		return true
	}
	other, ok := target.(*OpenAIRuntimeGuardSelectionError)
	return ok && other != nil && e.Code == other.Code && e.Category == other.Category
}

func newOpenAIRuntimeGuardSelectionError(code OpenAIRuntimeGuardErrorCode, category, message string, cause error, metadata map[string]string) *OpenAIRuntimeGuardSelectionError {
	md := map[string]string{}
	for key, value := range metadata {
		if strings.TrimSpace(value) != "" {
			md[key] = strings.TrimSpace(value)
		}
	}
	if len(md) == 0 {
		md = nil
	}
	return &OpenAIRuntimeGuardSelectionError{Code: code, Category: category, Message: message, cause: cause, Metadata: md}
}

func noAvailableOpenAISelectionErrorForRequest(requestedModel string, imageCapability OpenAIImagesCapability, compactBlocked bool, oauthCapabilityFiltered bool) error {
	if compactBlocked {
		return ErrNoAvailableCompactAccounts
	}
	message := "no available OpenAI accounts"
	if strings.TrimSpace(requestedModel) != "" {
		message = fmt.Sprintf("no available OpenAI accounts supporting model: %s", strings.TrimSpace(requestedModel))
	}
	code := OpenAIRuntimeGuardErrorCodeNoCompatibleAccount
	category := openAIRuntimeGuardCapabilityCategoryNoCompatibleAccount
	if oauthCapabilityFiltered {
		code = OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability
		category = openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel
	}
	return newOpenAIRuntimeGuardSelectionError(code, category, message, ErrNoAvailableAccounts, map[string]string{
		"model":            requestedModel,
		"image_capability": string(imageCapability),
	})
}

func openAIAccountRuntimeGuardSelectionError(account *Account, requestedModel string, imageCapability OpenAIImagesCapability) *OpenAIRuntimeGuardSelectionError {
	if openAIAccountSupportsRuntimeGuardCapability(account, requestedModel, imageCapability) {
		return nil
	}
	return newOpenAIUnsupportedOAuthCapabilitySelectionError(requestedModel, imageCapability)
}

func newOpenAIUnsupportedOAuthCapabilitySelectionError(requestedModel string, imageCapability OpenAIImagesCapability) *OpenAIRuntimeGuardSelectionError {
	err := noAvailableOpenAISelectionErrorForRequest(requestedModel, imageCapability, false, true)
	var selectionErr *OpenAIRuntimeGuardSelectionError
	if errors.As(err, &selectionErr) && selectionErr != nil {
		return selectionErr
	}
	return newOpenAIRuntimeGuardSelectionError(
		OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability,
		openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel,
		fmt.Sprintf("no available OpenAI accounts supporting model: %s", strings.TrimSpace(requestedModel)),
		ErrNoAvailableAccounts,
		map[string]string{"model": requestedModel, "image_capability": string(imageCapability)},
	)
}

func openAIAccountRuntimeGuardRejectsOAuthCandidate(account *Account, requestedModel string, imageCapability OpenAIImagesCapability) bool {
	if account == nil || !account.IsOpenAI() || !account.IsOpenAIOAuth() {
		return false
	}
	if requestedModel != "" && !account.IsModelSupported(requestedModel) {
		return false
	}
	return !openAIAccountSupportsRuntimeGuardCapability(account, requestedModel, imageCapability)
}

func isUnsupportedOpenAIOAuthRuntimeGuardModel(requestedModel string, imageCapability OpenAIImagesCapability) bool {
	model := strings.TrimSpace(requestedModel)
	if model == "" || imageCapability != "" || isOpenAIImageGenerationModel(model) {
		return false
	}
	return !openAIOAuthBuiltInModelSeedSupports(model)
}

func (e *OpenAIFastBlockedError) RuntimeGuardCode() OpenAIRuntimeGuardErrorCode {
	return OpenAIRuntimeGuardErrorCodeLocalPolicyBlock
}

func (e *OpenAIFastBlockedError) RuntimeGuardCategory() string {
	return openAIRuntimeGuardCapabilityCategoryLocalPolicyBlock
}

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
