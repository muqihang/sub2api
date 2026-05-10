package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func testOpenAISecretProtectorConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("11", 32)
	return cfg
}

func TestOpenAISecretProtector_ProtectCredentialsRoundTrip(t *testing.T) {
	protector, err := ProvideOpenAISecretProtector(testOpenAISecretProtectorConfig())
	require.NoError(t, err)
	require.NotNil(t, protector)

	input := map[string]any{
		"access_token":       "access-secret",
		"refresh_token":      "refresh-secret",
		"id_token":           "id-secret",
		"api_key":            "sk-secret-1234567890",
		"chatgpt_account_id": "acct-123",
		"base_url":           "https://api.openai.com/v1",
	}

	protected, err := protector.ProtectCredentials(input)
	require.NoError(t, err)
	require.NotEqual(t, input["access_token"], protected["access_token"])
	require.NotEqual(t, input["refresh_token"], protected["refresh_token"])
	require.NotEqual(t, input["id_token"], protected["id_token"])
	require.NotEqual(t, input["api_key"], protected["api_key"])
	require.Equal(t, "acct-123", protected["chatgpt_account_id"])
	require.Equal(t, "https://api.openai.com/v1", protected["base_url"])
	require.True(t, strings.HasPrefix(protected["access_token"].(string), "enc:v1:"))

	unprotected, err := protector.UnprotectCredentials(protected)
	require.NoError(t, err)
	require.Equal(t, input, unprotected)
}

func TestOpenAISecretProtector_EncryptValuePrefixRoundTrip(t *testing.T) {
	protector, err := ProvideOpenAISecretProtector(testOpenAISecretProtectorConfig())
	require.NoError(t, err)

	encrypted, err := protector.EncryptValue("refresh-secret")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(encrypted, "enc:v1:"))

	decrypted, err := protector.DecryptValue(encrypted)
	require.NoError(t, err)
	require.Equal(t, "refresh-secret", decrypted)
}

func TestOpenAISecretProtector_RejectsInvalidKeyLength(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("11", 31)

	protector, err := ProvideOpenAISecretProtector(cfg)
	require.Error(t, err)
	require.Nil(t, protector)
	require.Contains(t, err.Error(), "32 bytes")
}

func TestOpenAISecretProtector_ProductionRequiresKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.ProductionMode = true
	cfg.Gateway.OpenAICore.RequireEncryptedCredentials = true

	protector, err := ProvideOpenAISecretProtector(cfg)
	require.Error(t, err)
	require.Nil(t, protector)
	require.Contains(t, err.Error(), "credential_encryption_key")
}

func TestOpenAISecretProtector_DecryptErrorDoesNotExposeSecrets(t *testing.T) {
	protector, err := ProvideOpenAISecretProtector(testOpenAISecretProtectorConfig())
	require.NoError(t, err)

	ciphertext := "enc:v1:not-a-real-ciphertext"
	_, err = protector.DecryptValue(ciphertext)
	require.Error(t, err)
	require.NotContains(t, err.Error(), ciphertext)
	require.NotContains(t, err.Error(), "not-a-real-ciphertext")
}

func TestOpenAISecretProtector_MissingKeyAllowedOutsideProduction(t *testing.T) {
	protector, err := ProvideOpenAISecretProtector(&config.Config{})
	require.NoError(t, err)
	require.Nil(t, protector)
}
