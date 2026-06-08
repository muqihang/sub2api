package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexGatewayProviderProfile_DefaultFacts(t *testing.T) {
	deepseek := CodexGatewayProviderProfileFor(CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek"})
	require.Equal(t, "deepseek", deepseek.Provider)
	require.Equal(t, "deepseek", deepseek.ReasoningProtocol)
	require.Equal(t, []string{"high", "max"}, deepseek.SupportedEfforts)
	require.Equal(t, "max", deepseek.DefaultEffort)
	require.True(t, deepseek.SupportsToolReasoningReplay)
	require.False(t, deepseek.SupportsPromptCacheKey)
	require.True(t, deepseek.SupportsOfficialUserID)
	require.Equal(t, "deepseek_top_level", deepseek.CacheUsageShape)

	agnes := CodexGatewayProviderProfileFor(CodexGatewayModel{Slug: "agnes-2.0-flash", Provider: "agnes"})
	require.Equal(t, "agnes", agnes.Provider)
	require.Equal(t, "agnes", agnes.ReasoningProtocol)
	require.True(t, agnes.SupportsPromptCacheKey)
	require.False(t, agnes.SupportsOfficialUserID)

	claude := CodexGatewayProviderProfileFor(CodexGatewayModel{Slug: "claude-opus-4-8", Provider: "anthropic"})
	require.Equal(t, "anthropic", claude.Provider)
	require.Equal(t, "anthropic", claude.ReasoningProtocol)
	require.False(t, claude.SupportsOfficialUserID)
	require.False(t, claude.SupportsPromptCacheKey)

	openai := CodexGatewayProviderProfileFor(CodexGatewayModel{Slug: "gpt-5.5", Provider: "openai"})
	require.Equal(t, "openai", openai.Provider)
	require.Equal(t, "openai_responses", openai.ReasoningProtocol)
	require.True(t, openai.SupportsPromptCacheKey)
	require.Equal(t, "openai_nested", openai.CacheUsageShape)
}

func TestCodexGatewayProviderProfile_DoesNotChangeDeepSeekRequestSnapshot(t *testing.T) {
	req := CodexGatewayResponsesCreateRequest{
		Model: "deepseek-v4-pro",
		Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]`),
		Tools: json.RawMessage(`[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]`),
	}
	model := CodexGatewayModel{Slug: "deepseek-v4-pro", Provider: "deepseek", UpstreamModel: "deepseek-v4-pro"}
	before, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{SessionKey: "session_profile_snapshot", IsolationKey: "iso_profile_snapshot"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)

	profile := CodexGatewayProviderProfileFor(model)
	require.Equal(t, "deepseek", profile.Provider)

	after, err := BuildCodexGatewayDeepSeekRequest(model, req, nil, CodexGatewayDeepSeekRequestContext{SessionKey: "session_profile_snapshot", IsolationKey: "iso_profile_snapshot"}, CodexGatewayDeepSeekRequestConfig{})
	require.NoError(t, err)
	require.Equal(t, before.Body, after.Body)
	require.Equal(t, before.ToolNameMap, after.ToolNameMap)
}
