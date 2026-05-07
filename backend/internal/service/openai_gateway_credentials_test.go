package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func testOpenAIGatewayCredentialsConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("22", 32)
	return cfg
}

func TestOpenAIGatewayCredentialAccessor_EncryptedAPIKey(t *testing.T) {
	cfg := testOpenAIGatewayCredentialsConfig()
	protector, err := ProvideOpenAISecretProtector(cfg)
	require.NoError(t, err)

	protected, err := protector.ProtectCredentials(map[string]any{
		"api_key": "sk-secret-1234567890",
	})
	require.NoError(t, err)

	creds := NewOpenAIGatewayCredentials(cfg, protector)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: protected,
	}
	apiKey, err := creds.OpenAIAPIKey(account)
	require.NoError(t, err)
	require.Equal(t, "sk-secret-1234567890", apiKey)
}

func TestOpenAIGatewayCredentialAccessor_EncryptedOAuthTokens(t *testing.T) {
	cfg := testOpenAIGatewayCredentialsConfig()
	protector, err := ProvideOpenAISecretProtector(cfg)
	require.NoError(t, err)

	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token":  "access-secret",
		"refresh_token": "refresh-secret",
		"client_id":     "client-id",
	})
	require.NoError(t, err)

	creds := NewOpenAIGatewayCredentials(cfg, protector)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}
	accessToken, err := creds.OpenAIAccessToken(account)
	require.NoError(t, err)
	require.Equal(t, "access-secret", accessToken)

	refreshToken, err := creds.OpenAIRefreshToken(account)
	require.NoError(t, err)
	require.Equal(t, "refresh-secret", refreshToken)

	clientID, err := creds.OpenAIClientID(account)
	require.NoError(t, err)
	require.Equal(t, "client-id", clientID)
}

func TestOpenAIGatewayCredentialAccessor_PlaintextAllowedOutsideProduction(t *testing.T) {
	creds := NewOpenAIGatewayCredentials(&config.Config{}, nil)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "plain-access",
		},
	}
	accessToken, err := creds.OpenAIAccessToken(account)
	require.NoError(t, err)
	require.Equal(t, "plain-access", accessToken)
}

func TestOpenAIGatewayCredentialAccessor_RejectsPlaintextInProduction(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.RequireEncryptedCredentials = true

	creds := NewOpenAIGatewayCredentials(cfg, nil)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "plain-access",
		},
	}
	_, err := creds.OpenAIAccessToken(account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "plaintext openai credential access_token")
}

func TestOpenAIGatewayCredentialAccessor_RejectsPlaintextAPIKeyInProduction(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.RequireEncryptedCredentials = true

	creds := NewOpenAIGatewayCredentials(cfg, nil)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-plain",
		},
	}
	_, err := creds.OpenAIAPIKey(account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "plaintext openai credential api_key")
}

func TestOpenAIGatewayCredentialAccessor_DetectsUnsafePlaintextCredentialsInProduction(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.RequireEncryptedCredentials = true

	creds := NewOpenAIGatewayCredentials(cfg, nil)
	oauthAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "plain-access",
		},
	}
	apiKeyAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-plain",
		},
	}

	require.True(t, creds.HasUnsafePlaintextCredentials(oauthAccount))
	require.True(t, creds.HasUnsafePlaintextCredentials(apiKeyAccount))
}
