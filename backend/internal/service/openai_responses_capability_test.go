package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

func TestAccountShouldUseOpenAIResponsesAPI_RequiresCurrentTargetEvidence(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://provider.example/v1",
			"model_mapping": map[string]any{
				"gpt-5.6-sol":  "upstream-sol",
				"gpt-5.6-luna": "upstream-luna",
			},
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesSupported:  false,
			openai_compat.ExtraKeyResponsesProbeModel: "upstream-sol",
		},
	}
	account.Extra[openai_compat.ExtraKeyResponsesProbeTarget] = account.OpenAIResponsesTargetFingerprint("upstream-sol")

	require.False(t, account.ShouldUseOpenAIResponsesAPI("gpt-5.6-sol"))
	require.True(t, account.ShouldUseOpenAIResponsesAPI("gpt-5.6-luna"), "evidence for another mapped model is unknown")

	account.Credentials["base_url"] = "https://new-provider.example/v1"
	require.True(t, account.ShouldUseOpenAIResponsesAPI("gpt-5.6-sol"), "evidence for an old target is unknown")
	require.Equal(t, openai_compat.ResponsesSupportUnknown, account.ResolveOpenAIResponsesSupportForModel("gpt-5.6-sol"))
}

func TestAccountShouldUseOpenAIResponsesAPI_LegacyEvidenceIsUnknown(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra:    map[string]any{openai_compat.ExtraKeyResponsesSupported: false},
	}

	require.True(t, account.ShouldUseOpenAIResponsesAPI("gpt-5.6-sol"))

	account.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceChatCompletions)
	require.False(t, account.ShouldUseOpenAIResponsesAPI("gpt-5.6-sol"))
	account.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceResponses)
	require.True(t, account.ShouldUseOpenAIResponsesAPI("gpt-5.6-sol"))
}
