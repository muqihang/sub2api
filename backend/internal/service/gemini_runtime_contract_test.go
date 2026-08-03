//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiRuntimeContract_CodeAssistRequiresProjectID(t *testing.T) {
	t.Parallel()

	contract, err := ResolveGeminiRuntimeContract(&Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_type": "code_assist",
		},
	})
	require.NoError(t, err)
	require.Equal(t, GeminiAccountFamilyCodeAssist, contract.AccountFamily)
	require.Equal(t, GeminiUpstreamFamilyCodeAssist, contract.UpstreamFamily)
	require.True(t, contract.RequiresProjectID)
	require.Equal(t, GeminiTokenSourceOAuth, contract.TokenSource)
	require.True(t, contract.SupportsThoughtSignature)
}

func TestGeminiRuntimeContract_GoogleOneMayStartWithoutProjectIDButHasTierSemantics(t *testing.T) {
	t.Parallel()

	contract, err := ResolveGeminiRuntimeContract(&Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_type": "google_one",
		},
	})
	require.NoError(t, err)
	require.Equal(t, GeminiAccountFamilyGoogleOne, contract.AccountFamily)
	require.Equal(t, GeminiUpstreamFamilyCodeAssist, contract.UpstreamFamily)
	require.False(t, contract.RequiresProjectID)
	require.True(t, contract.HasTierSemantics)
	require.Equal(t, GeminiTokenSourceOAuth, contract.TokenSource)
}

func TestGeminiRuntimeContract_AIStudioHasNoProjectIDRequirement(t *testing.T) {
	t.Parallel()

	contract, err := ResolveGeminiRuntimeContract(&Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_type": "ai_studio",
		},
	})
	require.NoError(t, err)
	require.Equal(t, GeminiAccountFamilyAIStudioOAuth, contract.AccountFamily)
	require.Equal(t, GeminiUpstreamFamilyAIStudio, contract.UpstreamFamily)
	require.False(t, contract.RequiresProjectID)
	require.Equal(t, GeminiTokenSourceOAuth, contract.TokenSource)
}

func TestGeminiRuntimeContract_APIKeyMapsToAIStudioFamily(t *testing.T) {
	t.Parallel()

	contract, err := ResolveGeminiRuntimeContract(&Account{
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, GeminiAccountFamilyAIStudioAPIKey, contract.AccountFamily)
	require.Equal(t, GeminiUpstreamFamilyAIStudio, contract.UpstreamFamily)
	require.False(t, contract.RequiresProjectID)
	require.Equal(t, GeminiTokenSourceAPIKey, contract.TokenSource)
}

func TestGeminiRuntimeContract_ServiceAccountMapsToVertexFamily(t *testing.T) {
	t.Parallel()

	contract, err := ResolveGeminiRuntimeContract(&Account{
		Platform: PlatformGemini,
		Type:     AccountTypeServiceAccount,
		Credentials: map[string]any{
			"service_account_json": `{"project_id":"vertex-proj"}`,
		},
	})
	require.NoError(t, err)
	require.Equal(t, GeminiAccountFamilyVertexServiceAccount, contract.AccountFamily)
	require.Equal(t, GeminiUpstreamFamilyVertex, contract.UpstreamFamily)
	require.True(t, contract.RequiresProjectID)
	require.Equal(t, GeminiTokenSourceServiceAccount, contract.TokenSource)
	require.True(t, contract.SupportsThoughtSignature)
}
