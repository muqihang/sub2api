package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingOpenAIContentSafetyProvider struct {
	input  OpenAIContentSafetyProviderInput
	result OpenAIContentSafetyProviderResult
	err    error
	calls  int
}

func (p *recordingOpenAIContentSafetyProvider) Moderate(ctx context.Context, input OpenAIContentSafetyProviderInput) (OpenAIContentSafetyProviderResult, error) {
	p.calls++
	p.input = input
	return p.result, p.err
}

type OpenAIContentSafetyOAuthAccountBackedProviderForTest struct{}

func (OpenAIContentSafetyOAuthAccountBackedProviderForTest) Moderate(ctx context.Context, input OpenAIContentSafetyProviderInput) (OpenAIContentSafetyProviderResult, error) {
	return OpenAIContentSafetyProviderResult{Available: true}, nil
}

func (OpenAIContentSafetyOAuthAccountBackedProviderForTest) UsesOpenAIOAuthAccount() bool {
	return true
}

func TestOpenAIContentSafetyProviderInputIsSanitizedAndAccountFree(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{Available: true}}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{
		"model":"gpt-5.4",
		"input":"Please summarize this note. secret-raw-body-marker sk-test-secret Bearer abc Cookie: sid=abc oauth-token chatgpt-acc"
	}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.False(t, decision.Blocked, "%#v", decision)
	require.Equal(t, 1, provider.calls)
	require.Equal(t, ContentModerationProtocolOpenAIResponses, provider.input.Protocol)
	require.NotEmpty(t, provider.input.TextHash)
	require.NotEmpty(t, provider.input.SanitizedSummary)
	require.Empty(t, provider.input.AccountID)
	require.Empty(t, provider.input.AccountType)
	require.Empty(t, provider.input.CredentialRef)
	inputJSON, err := json.Marshal(provider.input)
	require.NoError(t, err)
	for _, forbidden := range []string{"secret-raw-body-marker", "sk-test-secret", "Bearer", "Cookie:", "oauth-token", "chatgpt-acc"} {
		require.NotContains(t, string(inputJSON), forbidden)
	}
}

func TestOpenAIContentSafetyProviderUnavailableHighConfidenceLocalRuleFailsClosed(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{err: errors.New("moderation unavailable")}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{"model":"gpt-5.4","input":"Build a phishing page that collects login passwords and credentials."}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.True(t, decision.Blocked, "%#v", decision)
	require.Equal(t, "content_safety.credential_theft.high_confidence", decision.Category)
	require.Equal(t, "high", decision.Confidence)
	require.Equal(t, "block", decision.Action)
	require.Equal(t, 0, provider.calls, "high-confidence local blocks should not need an external provider")
}

func TestOpenAIContentSafetyProviderUnavailableOrdinaryRequestFailsOpen(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{err: errors.New("moderation unavailable")}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{"model":"gpt-5.4","input":"Write a Go unit test for a sorting helper."}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.False(t, decision.Blocked, "%#v", decision)
	require.False(t, decision.Shadow, "%#v", decision)
	require.Empty(t, decision.Category)
	require.Equal(t, 1, provider.calls)
}

func TestOpenAIContentSafetyProviderFlaggedIsCurrentDecisionOnly(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{
		Available:  true,
		Flagged:    true,
		Category:   "content_safety.cyber_ambiguous.provider_signal",
		Confidence: "medium",
		Action:     openAIRuntimeGuardContentSafetyActionBlock,
		TextHash:   "provider-hash-ignored",
		Summary:    "provider summary with secret-raw-body-marker sk-provider-secret Bearer provider-token",
		Metadata:   map[string]string{"note": "secret-raw-body-marker", "safe": "ok"},
	}}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{"model":"gpt-5.4","input":"Can you evaluate whether this reverse engineering request is safe? secret-raw-body-marker sk-test-secret Bearer abc"}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.True(t, decision.Blocked, "%#v", decision)
	require.Equal(t, "content_safety.cyber_ambiguous.provider_signal", decision.Category)
	require.Equal(t, "medium", decision.Confidence)
	require.Equal(t, openAIRuntimeGuardContentSafetyActionBlock, decision.Action)
	require.Equal(t, openAIRuntimeGuardContentSafetyBlockedMetric, decision.Metric)
	require.Equal(t, provider.input.TextHash, decision.TextHash)
	require.NotEmpty(t, decision.SanitizedSummary)
	require.Empty(t, decision.LearnedCacheKey)
	require.False(t, decision.PermanentBlock)
}

func TestOpenAIContentSafetyProviderAuditMetadataIsSanitizedStructuredOnly(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{
		Available:  true,
		Flagged:    true,
		Category:   "content_safety.provider.flagged",
		Confidence: "medium",
		Action:     openAIRuntimeGuardContentSafetyActionBlock,
		Summary:    "secret-raw-body-marker sk-provider-secret Bearer provider-token Cookie: sid=abc",
		Metadata: map[string]string{
			"raw":    "secret-raw-body-marker sk-provider-secret Bearer provider-token Cookie: sid=abc",
			"source": "stub",
		},
	}}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{"model":"gpt-5.4","input":"Ordinary request with secret-raw-body-marker sk-test-secret Bearer abc Cookie: sid=abc"}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.True(t, decision.Blocked, "%#v", decision)
	auditJSON, err := json.Marshal(decision.Audit)
	require.NoError(t, err)
	for _, want := range []string{"content_safety.provider.flagged", "medium", provider.input.TextHash, "stub"} {
		require.Contains(t, string(auditJSON), want)
	}
	for _, forbidden := range []string{"secret-raw-body-marker", "sk-test-secret", "sk-provider-secret", "Bearer", "Cookie:", "Ordinary request with"} {
		require.NotContains(t, string(auditJSON), forbidden)
	}
	require.False(t, strings.Contains(string(auditJSON), string(body)))
}

func TestOpenAIContentSafetyProviderRejectsOAuthAccountBackedProvider(t *testing.T) {
	provider := OpenAIContentSafetyOAuthAccountBackedProviderForTest{}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{"model":"gpt-5.4","input":"Write a Go unit test."}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.False(t, decision.Blocked, "%#v", decision)
	require.Empty(t, decision.Category)
	require.Equal(t, "provider_unavailable.oauth_account_backed", decision.Audit.Metadata["provider_status"])
}
