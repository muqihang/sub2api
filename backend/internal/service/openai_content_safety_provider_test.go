package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestOpenAIContentSafetyProviderForwardBlocksBeforeUpstream(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{
		Available:  true,
		Flagged:    true,
		Category:   "content_safety.provider.flagged",
		Confidence: "medium",
		Action:     openAIRuntimeGuardContentSafetyActionBlock,
	}}
	svc.openAIContentSafetyProvider = provider
	body := []byte(`{"model":"gpt-5.4","stream":false,"instructions":"keep","input":"Review whether this ambiguous security request is safe."}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Nil(t, result)
	var blocked *OpenAIRuntimeGuardBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, 1, provider.calls)
	require.Len(t, upstream.bodies, 0)
	rawMeta, ok := c.Get(OpenAIRuntimeGuardMetadataKey)
	require.True(t, ok)
	metadata, ok := rawMeta.(OpenAIRuntimeGuardMetadata)
	require.True(t, ok)
	require.Equal(t, "block", metadata.Action)
	require.Equal(t, "content_safety.provider.flagged", metadata.Category)
	require.Equal(t, openAIRuntimeGuardContentSafetyBlockedMetric, metadata.Metric)
	require.Equal(t, "medium", metadata.Confidence)
}

func TestProvideOpenAIGatewayServiceWiresOpenAIContentSafetyProvider(t *testing.T) {
	contentModerationService := &ContentModerationService{}

	svc := ProvideOpenAIGatewayService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		contentModerationService,
	)

	require.NotNil(t, svc)
	require.NotNil(t, svc.openAIContentSafetyProvider)
	require.IsType(t, &contentModerationOpenAIContentSafetyProvider{}, svc.openAIContentSafetyProvider)
}

func TestOpenAIContentSafetyProviderReceivesRedactedModerationContentNotOnlyHash(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{Available: true}}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{"model":"gpt-5.4","input":"Review this ambiguous reverse-engineering request with secret-raw-body-marker sk-test-secret Bearer abc."}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.False(t, decision.Blocked, "%#v", decision)
	require.Equal(t, 1, provider.calls)
	require.NotEmpty(t, provider.input.RedactedText)
	require.Contains(t, provider.input.RedactedText, "Review this ambiguous reverse-engineering request")
	inputJSON, err := json.Marshal(provider.input)
	require.NoError(t, err)
	for _, forbidden := range []string{"secret-raw-body-marker", "sk-test-secret", "Bearer abc"} {
		require.NotContains(t, string(inputJSON), forbidden)
	}
}

func TestOpenAIContentSafetyProviderAuditDropsProviderRawSummaryAndMetadata(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{
		Available:  true,
		Flagged:    true,
		Category:   "content_safety.provider.flagged",
		Confidence: "medium",
		Action:     openAIRuntimeGuardContentSafetyActionBlock,
		Summary:    "raw prompt: my private medical legal text",
		Metadata: map[string]string{
			"excerpt": "my private medical legal text",
			"policy":  "provider-policy-v1",
		},
	}}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	body := []byte(`{"model":"gpt-5.4","input":"Ordinary request with private medical legal text"}`)

	decision := evaluateOpenAIRuntimeGuardContentSafetyWithProvider(context.Background(), account, ContentModerationProtocolOpenAIResponses, body, provider)

	require.True(t, decision.Blocked, "%#v", decision)
	auditJSON, err := json.Marshal(decision.Audit)
	require.NoError(t, err)
	require.Contains(t, string(auditJSON), "content_safety.provider.flagged")
	require.Contains(t, string(auditJSON), provider.input.TextHash)
	require.Contains(t, string(auditJSON), "provider-policy-v1")
	for _, forbidden := range []string{"raw prompt", "private medical legal text", "Ordinary request with"} {
		require.NotContains(t, string(auditJSON), forbidden)
	}
}

func TestOpenAIContentSafetyProviderWSBlocksBeforeUpstream(t *testing.T) {
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{
		Available:  true,
		Flagged:    true,
		Category:   "content_safety.provider.flagged",
		Confidence: "medium",
		Action:     openAIRuntimeGuardContentSafetyActionBlock,
	}}
	svc := &OpenAIGatewayService{openAIContentSafetyProvider: provider}
	account := newOpenAIRuntimeGuardContentSafetyOAuthAccount("")
	payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":"Review whether this ambiguous security request is safe."}`)

	_, blocked, err := svc.ApplyOpenAIRuntimeGuardToWSResponseCreatePayload(account, payload)

	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, "content_safety.provider.flagged", blocked.Decision.Category)
	require.Equal(t, 1, provider.calls)
}

func TestForwardAsChatCompletionsContentSafetyProviderCalledOnce(t *testing.T) {
	upstream, _, c, svc, account := newOpenAIRuntimeGuardForwardHarness(t)
	upstream.resp = openAIRuntimeGuardResponsesCompletedSSE("gpt-5.4")
	provider := &recordingOpenAIContentSafetyProvider{result: OpenAIContentSafetyProviderResult{Available: true}}
	svc.openAIContentSafetyProvider = provider
	body := []byte(`{"model":"gpt-5.4","stream":false,"messages":[{"role":"user","content":"Review whether this ambiguous security request is safe."}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, 1, provider.calls)
}
