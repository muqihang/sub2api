package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func testGeminiCredentialsAccessorConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("44", 32)
	return cfg
}

func TestGeminiCredentialsAccessor_EncryptedOAuthCredentialsRoundTrip(t *testing.T) {
	cfg := testGeminiCredentialsAccessorConfig()
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token":  "access-secret",
		"refresh_token": "refresh-secret",
		"project_id":    "my-project",
		"oauth_type":    "code_assist",
	})
	require.NoError(t, err)

	accessor := NewGeminiCredentialsAccessor(cfg, protector)
	account := &Account{
		Platform:    PlatformGemini,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}

	accessToken, err := accessor.GeminiAccessToken(account)
	require.NoError(t, err)
	require.Equal(t, "access-secret", accessToken)

	refreshToken, err := accessor.GeminiRefreshToken(account)
	require.NoError(t, err)
	require.Equal(t, "refresh-secret", refreshToken)

	projectID, err := accessor.GeminiProjectID(account)
	require.NoError(t, err)
	require.Equal(t, "my-project", projectID)
}

func TestGeminiCredentialsAccessor_EncryptedAPIKey(t *testing.T) {
	cfg := testGeminiCredentialsAccessorConfig()
	protector, err := ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)

	protected, err := protector.ProtectCredentials(map[string]any{
		"api_key": "AIza-secret-key",
	})
	require.NoError(t, err)

	accessor := NewGeminiCredentialsAccessor(cfg, protector)
	account := &Account{
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Credentials: protected,
	}

	apiKey, err := accessor.GeminiAPIKey(account)
	require.NoError(t, err)
	require.Equal(t, "AIza-secret-key", apiKey)
}

func TestGeminiCredentialsAccessor_PlaintextAllowedOutsideProduction(t *testing.T) {
	accessor := NewGeminiCredentialsAccessor(&config.Config{}, nil)
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "plain-access",
		},
	}

	accessToken, err := accessor.GeminiAccessToken(account)
	require.NoError(t, err)
	require.Equal(t, "plain-access", accessToken)
}

func TestGeminiCredentialsAccessor_BuildProtectedOAuthCredentials(t *testing.T) {
	cfg := testGeminiCredentialsAccessorConfig()
	svc := NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	defer svc.Stop()

	protected, err := svc.BuildProtectedAccountCredentials(&GeminiTokenInfo{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		ExpiresAt:    1700000000,
		ProjectID:    "runtime-project",
		OAuthType:    "code_assist",
	})
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(protected["access_token"].(string), geminiSecretProtectorPrefix))
	require.True(t, strings.HasPrefix(protected["refresh_token"].(string), geminiSecretProtectorPrefix))
	require.Equal(t, "runtime-project", protected["project_id"])
}
