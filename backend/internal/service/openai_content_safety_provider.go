package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// OpenAIContentSafetyProvider is an injectable moderation signal source. The
// input deliberately excludes Account, token, cookie, credential, and raw body
// data so implementations cannot consume the shared GPT OAuth account pool.
type OpenAIContentSafetyProvider interface {
	Moderate(ctx context.Context, input OpenAIContentSafetyProviderInput) (OpenAIContentSafetyProviderResult, error)
}

// OpenAIContentSafetyProviderOAuthAccountBacked marks providers that are backed
// by OpenAI OAuth accounts. Runtime guard refuses to call these providers.
type OpenAIContentSafetyProviderOAuthAccountBacked interface {
	UsesOpenAIOAuthAccount() bool
}

type OpenAIContentSafetyProviderInput struct {
	Protocol         string            `json:"protocol"`
	TextHash         string            `json:"text_hash"`
	SanitizedSummary string            `json:"sanitized_summary"`
	RedactedText     string            `json:"redacted_text,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`

	// These fields remain empty by construction and are present to make tests and
	// reviews catch accidental account/credential plumbing into provider inputs.
	AccountID     string `json:"account_id,omitempty"`
	AccountType   string `json:"account_type,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
}

type OpenAIContentSafetyProviderResult struct {
	Available  bool              `json:"available"`
	Flagged    bool              `json:"flagged"`
	Category   string            `json:"category,omitempty"`
	Confidence string            `json:"confidence,omitempty"`
	Action     string            `json:"action,omitempty"`
	TextHash   string            `json:"text_hash,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type OpenAIContentSafetyAuditRecord struct {
	Category         string            `json:"category,omitempty"`
	Confidence       string            `json:"confidence,omitempty"`
	TextHash         string            `json:"text_hash,omitempty"`
	SanitizedSummary string            `json:"sanitized_summary,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

func evaluateOpenAIRuntimeGuardContentSafetyWithProvider(
	ctx context.Context,
	account *Account,
	protocol string,
	body []byte,
	provider OpenAIContentSafetyProvider,
) openAIRuntimeGuardContentSafetyDecision {
	if !shouldApplyOpenAIRuntimeGuardContentSafety(account) || len(body) == 0 || openAIRuntimeGuardContentSafetyDisabled(account) {
		return openAIRuntimeGuardContentSafetyDecision{}
	}
	text := openAIRuntimeGuardContentSafetyExtractText(protocol, body)
	textHash := openAIContentSafetyTextHash(text)
	summary := openAIContentSafetySanitizedSummary(text, textHash)
	if category := classifyOpenAIRuntimeGuardContentSafety(text); category != "" {
		decision := openAIRuntimeGuardContentSafetyDecisionForAction(account, openAIRuntimeGuardContentSafetyActionBlock, category, "high")
		decision.TextHash = textHash
		decision.SanitizedSummary = summary
		decision.Audit = OpenAIContentSafetyAuditRecord{
			Category:         decision.Category,
			Confidence:       decision.Confidence,
			TextHash:         textHash,
			SanitizedSummary: summary,
		}
		return decision
	}
	if provider == nil || textHash == "" {
		return openAIRuntimeGuardContentSafetyDecision{}
	}
	if openAIContentSafetyProviderUsesOAuthAccount(provider) {
		return openAIRuntimeGuardContentSafetyDecision{
			TextHash:         textHash,
			SanitizedSummary: summary,
			Audit: OpenAIContentSafetyAuditRecord{
				TextHash:         textHash,
				SanitizedSummary: summary,
				Metadata:         map[string]string{"provider_status": "provider_unavailable.oauth_account_backed"},
			},
		}
	}
	redactedText := openAIContentSafetyRedactedText(text)
	if redactedText == "" {
		return openAIRuntimeGuardContentSafetyDecision{}
	}
	inputMetadata := map[string]string{"input_kind": "redacted_text"}
	if model := safeOpenAIRuntimeGuardMetadataValue(gjson.GetBytes(body, "model").String()); model != "" {
		inputMetadata["model"] = model
	}
	input := OpenAIContentSafetyProviderInput{
		Protocol:         strings.TrimSpace(protocol),
		TextHash:         textHash,
		SanitizedSummary: summary,
		RedactedText:     redactedText,
		Metadata:         inputMetadata,
	}
	result, err := provider.Moderate(ctx, input)
	if err != nil || !result.Available {
		status := "provider_unavailable"
		if err != nil {
			status = "provider_unavailable.error"
		}
		return openAIRuntimeGuardContentSafetyDecision{
			TextHash:         textHash,
			SanitizedSummary: summary,
			Audit: OpenAIContentSafetyAuditRecord{
				TextHash:         textHash,
				SanitizedSummary: summary,
				Metadata:         map[string]string{"provider_status": status},
			},
		}
	}
	if !result.Flagged {
		return openAIRuntimeGuardContentSafetyDecision{}
	}
	category := strings.TrimSpace(result.Category)
	if category == "" {
		category = "content_safety.provider.flagged"
	}
	confidence := strings.TrimSpace(result.Confidence)
	if confidence == "" {
		confidence = "medium"
	}
	action := strings.TrimSpace(result.Action)
	if action == "" {
		action = openAIRuntimeGuardContentSafetyActionBlock
	}
	decision := openAIRuntimeGuardContentSafetyDecisionForAction(account, action, category, confidence)
	decision.TextHash = textHash
	decision.SanitizedSummary = summary
	decision.Audit = OpenAIContentSafetyAuditRecord{
		Category:         decision.Category,
		Confidence:       decision.Confidence,
		TextHash:         textHash,
		SanitizedSummary: decision.SanitizedSummary,
		Metadata:         openAIContentSafetySanitizedMetadata(result.Metadata),
	}
	return decision
}

func openAIRuntimeGuardContentSafetyDecisionForAction(account *Account, action, category, confidence string) openAIRuntimeGuardContentSafetyDecision {
	decision := openAIRuntimeGuardContentSafetyDecision{
		Action:     openAIRuntimeGuardContentSafetyActionBlock,
		Category:   strings.TrimSpace(category),
		Metric:     openAIRuntimeGuardContentSafetyBlockedMetric,
		Confidence: strings.TrimSpace(confidence),
		Blocked:    true,
	}
	if decision.Confidence == "" {
		decision.Confidence = "medium"
	}
	if strings.TrimSpace(action) == openAIRuntimeGuardContentSafetyActionShadow || openAIRuntimeGuardContentSafetyShadowOnly(account) {
		decision.Action = openAIRuntimeGuardContentSafetyActionShadow
		decision.Metric = openAIRuntimeGuardContentSafetyShadowMetric
		decision.Blocked = false
		decision.Shadow = true
	}
	return decision
}

func openAIContentSafetyProviderUsesOAuthAccount(provider OpenAIContentSafetyProvider) bool {
	if provider == nil {
		return false
	}
	marker, ok := provider.(OpenAIContentSafetyProviderOAuthAccountBacked)
	return ok && marker.UsesOpenAIOAuthAccount()
}

func openAIContentSafetyTextHash(text string) string {
	normalized := normalizeContentModerationText(text)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func openAIContentSafetySanitizedSummary(text, textHash string) string {
	normalized := normalizeContentModerationText(text)
	if normalized == "" {
		return ""
	}
	return fmt.Sprintf("text_len=%d hash_prefix=%s", len([]rune(normalized)), openAIContentSafetyHashPrefix(textHash))
}

func openAIContentSafetyMergeSanitizedSummary(localSummary, providerSummary, textHash string) string {
	return localSummary
}

func openAIContentSafetySanitizedMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = safeOpenAIRuntimeGuardMetadataValue(key)
		value = safeOpenAIRuntimeGuardMetadataValue(value)
		if !openAIContentSafetyAuditMetadataKeyAllowed(key) || value == "" || openAIContentSafetyContainsSensitive(value) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func openAIContentSafetyContainsSensitive(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "secret-raw-body-marker") ||
		strings.Contains(lower, "bearer") ||
		strings.Contains(lower, "cookie") ||
		strings.Contains(lower, "oauth-token") ||
		strings.Contains(lower, "chatgpt-acc") ||
		strings.Contains(lower, "sk-")
}

func openAIContentSafetyHashPrefix(textHash string) string {
	if len(textHash) < 12 {
		return textHash
	}
	return textHash[:12]
}

var openAIContentSafetyExtraSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bBearer\s+\S+`),
	regexp.MustCompile(`(?i)\bCookie:\s*\S+`),
	regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9._~+/=-]{4,}\b`),
	regexp.MustCompile(`(?i)\b(?:oauth-token|chatgpt-acc|secret-raw-body-marker)\b`),
}

func openAIContentSafetyRedactedText(text string) string {
	redacted := redactContentModerationSecrets(normalizeContentModerationText(text))
	for _, pattern := range openAIContentSafetyExtraSecretPatterns {
		redacted = pattern.ReplaceAllString(redacted, "[redacted]")
	}
	return trimRunes(normalizeContentModerationText(redacted), maxModerationInputRunes)
}

func openAIContentSafetyAuditMetadataKeyAllowed(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "policy", "provider", "provider_status", "rule_id", "severity", "source":
		return true
	default:
		return false
	}
}

type contentModerationOpenAIContentSafetyProvider struct {
	svc *ContentModerationService
}

func NewContentModerationOpenAIContentSafetyProvider(svc *ContentModerationService) OpenAIContentSafetyProvider {
	if svc == nil {
		return nil
	}
	return &contentModerationOpenAIContentSafetyProvider{svc: svc}
}

func (p *contentModerationOpenAIContentSafetyProvider) Moderate(ctx context.Context, input OpenAIContentSafetyProviderInput) (OpenAIContentSafetyProviderResult, error) {
	if p == nil || p.svc == nil {
		return OpenAIContentSafetyProviderResult{}, nil
	}
	text := openAIContentSafetyRedactedText(input.RedactedText)
	if text == "" {
		return OpenAIContentSafetyProviderResult{}, nil
	}
	protocol := strings.TrimSpace(input.Protocol)
	if protocol == "" {
		protocol = ContentModerationProtocolOpenAIResponses
	}
	body := []byte(strconv.Quote(text))
	if protocol == ContentModerationProtocolOpenAIImages {
		body = []byte(`{"prompt":` + strconv.Quote(text) + `}`)
	} else {
		body = []byte(`{"input":` + strconv.Quote(text) + `}`)
		protocol = ContentModerationProtocolOpenAIResponses
	}
	decision, err := p.svc.Check(ctx, ContentModerationCheckInput{
		Endpoint: "openai_runtime_guard",
		Provider: "openai_runtime_guard",
		Model:    strings.TrimSpace(input.Metadata["model"]),
		Protocol: protocol,
		Body:     body,
	})
	if err != nil {
		return OpenAIContentSafetyProviderResult{}, err
	}
	if decision == nil {
		return OpenAIContentSafetyProviderResult{Available: true}, nil
	}
	result := OpenAIContentSafetyProviderResult{
		Available:  true,
		Flagged:    decision.Flagged || decision.Blocked,
		Category:   decision.HighestCategory,
		Confidence: openAIContentSafetyConfidenceFromScore(decision.HighestScore),
		Metadata: map[string]string{
			"provider":        "content_moderation_service",
			"provider_status": "ok",
		},
	}
	if decision.Blocked {
		result.Action = openAIRuntimeGuardContentSafetyActionBlock
	} else if decision.Flagged {
		result.Action = openAIRuntimeGuardContentSafetyActionShadow
	}
	return result, nil
}

func openAIContentSafetyConfidenceFromScore(score float64) string {
	switch {
	case score >= 0.90:
		return "high"
	case score >= 0.50:
		return "medium"
	case score > 0:
		return "low"
	default:
		return ""
	}
}
