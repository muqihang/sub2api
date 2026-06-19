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

	openAIRuntimeGuardCapabilityCategoryLocalPolicyBlock        = "capability.local_policy_block"
	openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel   = "capability.unsupported_oauth_model_profile"
	openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthPersona = "capability.unsupported_oauth_persona_version"
	openAIRuntimeGuardCapabilityCategoryNoCompatibleAccount     = "capability.no_compatible_account"
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
		if openAIOAuthRequestMayRequireNewerCodexPersona(requestedModel, imageCapability) &&
			!isUnsupportedOpenAIOAuthRuntimeGuardModel(requestedModel, imageCapability) {
			category = openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthPersona
		}
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
	return newOpenAIUnsupportedOAuthCapabilitySelectionErrorForUpstream(
		account,
		requestedModel,
		openAIAccountRuntimeGuardResolvedUpstreamModel(account, requestedModel),
		imageCapability,
	)
}

func openAIAccountRuntimeGuardSelectionErrorForUpstream(account *Account, requestedModel string, upstreamModel string, imageCapability OpenAIImagesCapability) *OpenAIRuntimeGuardSelectionError {
	if openAIAccountSupportsRuntimeGuardUpstreamCapability(account, requestedModel, upstreamModel, imageCapability) {
		return nil
	}
	return newOpenAIUnsupportedOAuthCapabilitySelectionErrorForUpstream(account, requestedModel, upstreamModel, imageCapability)
}

func newOpenAIUnsupportedOAuthCapabilitySelectionError(requestedModel string, imageCapability OpenAIImagesCapability) *OpenAIRuntimeGuardSelectionError {
	return newOpenAIUnsupportedOAuthCapabilitySelectionErrorForUpstream(nil, requestedModel, "", imageCapability)
}

func newOpenAIUnsupportedOAuthCapabilitySelectionErrorForUpstream(account *Account, requestedModel string, upstreamModel string, imageCapability OpenAIImagesCapability) *OpenAIRuntimeGuardSelectionError {
	err := noAvailableOpenAISelectionErrorForRequest(requestedModel, imageCapability, false, true)
	var selectionErr *OpenAIRuntimeGuardSelectionError
	if errors.As(err, &selectionErr) && selectionErr != nil {
		if account != nil {
			category := openAIAccountRuntimeGuardUnsupportedCategory(account, requestedModel, upstreamModel, imageCapability)
			selectionErr.Category = category
		}
		if strings.TrimSpace(upstreamModel) != "" && strings.TrimSpace(upstreamModel) != strings.TrimSpace(requestedModel) {
			if selectionErr.Metadata == nil {
				selectionErr.Metadata = map[string]string{}
			}
			selectionErr.Metadata["upstream_model"] = strings.TrimSpace(upstreamModel)
		}
		return selectionErr
	}
	return newOpenAIRuntimeGuardSelectionError(
		OpenAIRuntimeGuardErrorCodeUnsupportedOAuthCapability,
		openAIAccountRuntimeGuardUnsupportedCategory(account, requestedModel, upstreamModel, imageCapability),
		fmt.Sprintf("no available OpenAI accounts supporting model: %s", strings.TrimSpace(requestedModel)),
		ErrNoAvailableAccounts,
		map[string]string{"model": requestedModel, "upstream_model": upstreamModel, "image_capability": string(imageCapability)},
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
	return openAIAccountSupportsRuntimeGuardUpstreamCapability(
		account,
		requestedModel,
		openAIAccountRuntimeGuardResolvedUpstreamModel(account, requestedModel),
		imageCapability,
	)
}

func openAIAccountSupportsRuntimeGuardUpstreamCapability(account *Account, requestedModel string, upstreamModel string, imageCapability OpenAIImagesCapability) bool {
	if account == nil || !account.IsOpenAI() {
		return false
	}
	if !account.IsOpenAIOAuth() {
		return true
	}
	if !openAIOAuthAccountSupportsImageCapability(account, imageCapability) {
		return false
	}
	if !openAIOAuthAccountSupportsRequestedModel(account, requestedModel, imageCapability) {
		return false
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	requestedModel = strings.TrimSpace(requestedModel)
	if upstreamModel != "" && !strings.EqualFold(upstreamModel, requestedModel) &&
		!openAIOAuthAccountSupportsRequestedModel(account, upstreamModel, imageCapability) {
		return false
	}
	return !openAIOAuthCodexPersonaVersionTooOldForRequest(account, requestedModel, imageCapability) &&
		!openAIOAuthCodexPersonaVersionTooOldForRequest(account, upstreamModel, imageCapability)
}

func openAIAccountRuntimeGuardResolvedUpstreamModel(account *Account, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ""
	}
	if account == nil {
		return requestedModel
	}
	mappedModel := strings.TrimSpace(account.GetMappedModel(requestedModel))
	if mappedModel == "" {
		mappedModel = requestedModel
	}
	return normalizeOpenAIModelForUpstream(account, mappedModel)
}

func openAIAccountRuntimeGuardUnsupportedCategory(account *Account, requestedModel string, upstreamModel string, imageCapability OpenAIImagesCapability) string {
	if openAIOAuthRuntimeGuardPersonaRejects(account, requestedModel, upstreamModel, imageCapability) {
		return openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthPersona
	}
	return openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthModel
}

func openAIOAuthRuntimeGuardPersonaRejects(account *Account, requestedModel string, upstreamModel string, imageCapability OpenAIImagesCapability) bool {
	if account == nil || !account.IsOpenAIOAuth() {
		return false
	}
	if !openAIOAuthAccountSupportsImageCapability(account, imageCapability) {
		return false
	}
	requestedModel = strings.TrimSpace(requestedModel)
	upstreamModel = strings.TrimSpace(upstreamModel)
	if !openAIOAuthAccountSupportsRequestedModel(account, requestedModel, imageCapability) {
		return false
	}
	if upstreamModel != "" && !strings.EqualFold(upstreamModel, requestedModel) &&
		!openAIOAuthAccountSupportsRequestedModel(account, upstreamModel, imageCapability) {
		return false
	}
	return openAIOAuthCodexPersonaVersionTooOldForRequest(account, requestedModel, imageCapability) ||
		openAIOAuthCodexPersonaVersionTooOldForRequest(account, upstreamModel, imageCapability)
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

func openAIOAuthMinCodexPersonaVersionForModel(requestedModel string, imageCapability OpenAIImagesCapability) string {
	if imageCapability != "" {
		return ""
	}
	m := strings.ToLower(strings.TrimSpace(requestedModel))
	if m == "" {
		return ""
	}
	m = strings.TrimPrefix(m, "openai/")
	m = strings.TrimPrefix(m, "models/")
	switch {
	case m == "gpt-5.5" || strings.HasPrefix(m, "gpt-5.5-"):
		return "2.1.175"
	case m == "gpt-5.4" || strings.HasPrefix(m, "gpt-5.4-"):
		if strings.HasPrefix(m, "gpt-5.4-nano") {
			return ""
		}
		return "2.1.175"
	default:
		return ""
	}
}

func openAIOAuthRequestMayRequireNewerCodexPersona(requestedModel string, imageCapability OpenAIImagesCapability) bool {
	if openAIOAuthMinCodexPersonaVersionForModel(requestedModel, imageCapability) != "" {
		return true
	}
	model := strings.TrimSpace(requestedModel)
	if model == "" {
		return false
	}
	upstreamModel := normalizeCodexModel(model)
	return !strings.EqualFold(upstreamModel, model) &&
		openAIOAuthMinCodexPersonaVersionForModel(upstreamModel, imageCapability) != ""
}

func openAIOAuthCodexPersonaVersionTooOldForRequest(account *Account, requestedModel string, imageCapability OpenAIImagesCapability) bool {
	minVersion := openAIOAuthMinCodexPersonaVersionForModel(requestedModel, imageCapability)
	if minVersion == "" {
		return false
	}
	version := openAIOAuthCodexPersonaVersion(account)
	if version == "" {
		return false
	}
	return openAICodexPersonaVersionLessThan(version, minVersion)
}

func openAIOAuthCodexPersonaVersion(account *Account) string {
	if account == nil {
		return ""
	}
	for _, key := range []string{
		"openai_gateway_canonical_version",
		"openai_codex_persona_version",
		"codex_persona_version",
		"codex_version",
	} {
		if value := strings.TrimSpace(account.GetExtraString(key)); value != "" {
			return value
		}
	}
	for _, ua := range []string{
		account.GetExtraString("openai_gateway_canonical_user_agent"),
		account.GetOpenAIUserAgent(),
	} {
		if version := deriveOpenAIGatewayProfileVersion(ua); version != "" {
			return version
		}
	}
	return ""
}

func openAICodexPersonaVersionLessThan(version string, minVersion string) bool {
	version = strings.TrimSpace(version)
	minVersion = strings.TrimSpace(minVersion)
	if version == "" || minVersion == "" {
		return false
	}
	// Only enforce the known 2.x persona line. Codex CLI 0.x profiles use a
	// different version scheme and are not treated as a forged 2.x persona.
	if !strings.HasPrefix(version, "2.") || !strings.HasPrefix(minVersion, "2.") {
		return false
	}
	return compareVersions(version, minVersion) < 0
}

func evaluateOpenAIOAuthCodexPersonaGuard(account *Account, requestedModel string, imageCapability OpenAIImagesCapability) openAIReasoningEffortGuardDecision {
	if account == nil || !account.IsOpenAIOAuth() {
		return openAIReasoningEffortGuardDecision{}
	}
	minVersion := openAIOAuthMinCodexPersonaVersionForModel(requestedModel, imageCapability)
	if minVersion == "" {
		return openAIReasoningEffortGuardDecision{}
	}
	version := openAIOAuthCodexPersonaVersion(account)
	if !openAICodexPersonaVersionLessThan(version, minVersion) {
		return openAIReasoningEffortGuardDecision{}
	}
	return openAIReasoningEffortGuardDecision{
		Action:   "block",
		Blocked:  true,
		Present:  true,
		Status:   400,
		Path:     "model",
		From:     version,
		To:       minVersion,
		Category: openAIRuntimeGuardCapabilityCategoryUnsupportedOAuthPersona,
		Metric:   "openai_runtime_guard.blocked.oauth_persona_version",
	}
}

func classifyOpenAIUpstreamAuth401ErrorCode(responseBody []byte) string {
	if code := strings.TrimSpace(extractUpstreamErrorCode(responseBody)); code != "" {
		return code
	}
	msg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(responseBody)))
	if msg == "" && len(responseBody) > 0 {
		msg = strings.ToLower(strings.TrimSpace(sanitizeUpstreamErrorMessage(string(responseBody))))
	}
	switch {
	case strings.Contains(msg, "authentication token has been invalidated"),
		strings.Contains(msg, "token has been invalidated"),
		strings.Contains(msg, "token invalidated"):
		return openAIAuthErrorCodeTokenInvalidated
	case strings.Contains(msg, "token revoked"):
		return openAIAuthErrorCodeTokenRevoked
	default:
		return ""
	}
}
