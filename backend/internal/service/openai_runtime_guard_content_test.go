package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRuntimeGuardInvalidEncryptedContentFixturePreservesSingleRetryMetadata(t *testing.T) {
	fixture := openAIRuntimeGuardFixtureByID(t, "invalid_encrypted_content_trim_retry_once")
	requireOpenAIRuntimeGuardDecision(t, fixture, "retry_repair", 2)
	require.NotNil(t, fixture.UpstreamError)
	require.Equal(t, "invalid_encrypted_content", fixture.UpstreamError.Code)
	require.NotNil(t, fixture.Expect.Retry)
	require.Equal(t, 1, fixture.Expect.Retry.MaxAttempts)
	require.Equal(t, "input[].encrypted_content", fixture.Expect.Retry.TrimPath)
	require.Equal(t, "content.invalid_encrypted_content", fixture.Expect.Category)
	require.NotEmpty(t, fixture.Expect.Metric)

	firstRequest := openAIRuntimeGuardInputItem(t, openAIRuntimeGuardFixtureRequest(t, fixture), 0)
	_, hadEncryptedContent := firstRequest["encrypted_content"]
	require.True(t, hadEncryptedContent)

	retryBody := decodeOpenAIRuntimeGuardJSON(t, fixture.Expect.Retry.Request)
	retryReasoning := openAIRuntimeGuardInputItem(t, retryBody, 0)
	require.NotContains(t, retryReasoning, "encrypted_content")
}

func TestOpenAIRuntimeGuardContentSafetyFixturesCoverMVPBlocksAndAllowedDefensiveWork(t *testing.T) {
	blockIDs := []string{
		"content_safety_clear_sexual_block",
		"content_safety_credential_theft_block",
		"content_safety_malware_block",
	}
	for _, id := range blockIDs {
		t.Run(id, func(t *testing.T) {
			fixture := openAIRuntimeGuardFixtureByID(t, id)
			requireOpenAIRuntimeGuardDecision(t, fixture, "block", 0)
			require.Contains(t, fixture.Expect.Category, "content_safety.")
			require.Equal(t, "openai_runtime_guard.content_safety.blocked", fixture.Expect.Metric)
		})
	}

	allowed := openAIRuntimeGuardFixtureByID(t, "content_safety_defensive_security_not_blocked")
	requireOpenAIRuntimeGuardDecision(t, allowed, "pass", 1)
	require.Equal(t, "content_safety.defensive_security_allowed", allowed.Expect.Category)
	require.Equal(t, "openai_runtime_guard.content_safety.allowed", allowed.Expect.Metric)
}

func TestOpenAIRuntimeGuardContentSafetyFixturesKeepRequestsSanitized(t *testing.T) {
	for _, fixture := range loadOpenAIRuntimeGuardFixtureCatalog(t).Fixtures {
		if fixture.Area != "content_safety" {
			continue
		}
		t.Run(fixture.ID, func(t *testing.T) {
			var request map[string]any
			require.NoError(t, json.Unmarshal(fixture.Request, &request))
			text, _ := request["input"].(string)
			require.NotContains(t, text, "sk-")
			require.NotContains(t, text, "Bearer ")
			require.NotContains(t, text, "BEGIN PRIVATE KEY")
		})
	}
}
