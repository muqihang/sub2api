package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func testGeminiSecretProtectorConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("33", 32)
	return cfg
}

func TestGeminiSecretProtector_ProtectCredentialsRoundTrip(t *testing.T) {
	protector, err := ProvideGeminiSecretProtector(testGeminiSecretProtectorConfig())
	require.NoError(t, err)
	require.NotNil(t, protector)

	input := map[string]any{
		"access_token":         "access-secret",
		"refresh_token":        "refresh-secret",
		"api_key":              "AIza-secret-key",
		"service_account_json": `{"project_id":"vertex-proj","client_email":"svc@vertex-proj.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"}`,
		"project_id":           "runtime-project",
	}

	protected, err := protector.ProtectCredentials(input)
	require.NoError(t, err)
	require.NotEqual(t, input["access_token"], protected["access_token"])
	require.NotEqual(t, input["refresh_token"], protected["refresh_token"])
	require.NotEqual(t, input["api_key"], protected["api_key"])
	require.NotEqual(t, input["service_account_json"], protected["service_account_json"])
	require.Equal(t, "runtime-project", protected["project_id"])
	require.True(t, strings.HasPrefix(protected["access_token"].(string), geminiSecretProtectorPrefix))

	unprotected, err := protector.UnprotectCredentials(protected)
	require.NoError(t, err)
	require.Equal(t, input, unprotected)
}

func TestGeminiSecretProtector_DecryptErrorDoesNotExposeSecrets(t *testing.T) {
	protector, err := ProvideGeminiSecretProtector(testGeminiSecretProtectorConfig())
	require.NoError(t, err)

	ciphertext := geminiSecretProtectorPrefix + "not-a-real-ciphertext"
	_, err = protector.DecryptValue(ciphertext)
	require.Error(t, err)
	require.NotContains(t, err.Error(), ciphertext)
	require.NotContains(t, err.Error(), "not-a-real-ciphertext")
}

func TestGeminiSecretProtector_MissingKeyAllowedOutsideProduction(t *testing.T) {
	protector, err := ProvideGeminiSecretProtector(&config.Config{})
	require.NoError(t, err)
	require.Nil(t, protector)
}

func TestGeminiSecretProtector_ProductionRequiresKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gemini.ProductionMode = true

	protector, err := ProvideGeminiSecretProtector(cfg)
	require.Error(t, err)
	require.Nil(t, protector)
	require.Contains(t, err.Error(), "credential_encryption_key")
}
