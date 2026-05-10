package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type openaiOAuthClientRefreshStub struct {
	refreshCalls int32
	lastProxyURL string
}

func (s *openaiOAuthClientRefreshStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientRefreshStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	atomic.AddInt32(&s.refreshCalls, 1)
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientRefreshStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	atomic.AddInt32(&s.refreshCalls, 1)
	s.lastProxyURL = proxyURL
	return nil, errors.New("not implemented")
}

func TestOpenAIOAuthService_RefreshAccountToken_NoRefreshTokenUsesExistingAccessToken(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{}
	svc := NewOpenAIOAuthService(nil, client)

	expiresAt := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "existing-access-token",
			"expires_at":   expiresAt,
			"client_id":    "client-id-1",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "existing-access-token", info.AccessToken)
	require.Equal(t, "client-id-1", info.ClientID)
	require.Zero(t, atomic.LoadInt32(&client.refreshCalls), "existing access token should be reused without calling refresh")
}

func TestOpenAIOAuthService_RefreshAccountToken_UsesGatewayBucketProxy(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{}
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
		{Name: "bucket-a", Enabled: true, ProxyURL: "socks5://127.0.0.1:9001"},
	}
	core := NewOpenAIGatewayCoreService(nil, cfg, nil)
	svc := NewOpenAIOAuthService(nil, client)
	svc.SetGatewayCoreService(core)

	account := &Account{
		ID:       88,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt-1",
			"client_id":     "client-id-1",
		},
		Extra: map[string]any{
			"openai_gateway_egress_bucket": "bucket-a",
		},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	require.Error(t, err)
	require.Equal(t, "socks5://127.0.0.1:9001", client.lastProxyURL)
}

func TestOpenAIOAuthService_BuildAccountCredentials_EncryptsProtectedFields(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("66", 32)
	core := NewOpenAIGatewayCoreService(nil, cfg, nil)
	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientRefreshStub{})
	svc.SetGatewayCoreService(core)

	creds, err := svc.BuildAccountCredentials(&OpenAITokenInfo{
		AccessToken:      "new-access-token",
		RefreshToken:     "new-refresh-token",
		IDToken:          "new-id-token",
		ChatGPTAccountID: "acct-1",
		ExpiresAt:        time.Now().Add(1 * time.Hour).Unix(),
	})
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(creds["access_token"].(string), openAISecretProtectorPrefix))
	require.True(t, strings.HasPrefix(creds["refresh_token"].(string), openAISecretProtectorPrefix))
	require.True(t, strings.HasPrefix(creds["id_token"].(string), openAISecretProtectorPrefix))
	require.Equal(t, "acct-1", creds["chatgpt_account_id"])
}

func TestOpenAIOAuthService_RefreshAccountToken_UsesEncryptedExistingAccessToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("77", 32)
	protector, err := ProvideOpenAISecretProtector(cfg)
	require.NoError(t, err)

	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token": "existing-access-token",
		"id_token":     "existing-id-token",
	})
	require.NoError(t, err)
	protected["expires_at"] = time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	protected["client_id"] = "client-id-1"

	client := &openaiOAuthClientRefreshStub{}
	svc := NewOpenAIOAuthService(nil, client)
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, cfg, nil))

	account := &Account{
		ID:          77,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: protected,
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "existing-access-token", info.AccessToken)
	require.Equal(t, "existing-id-token", info.IDToken)
	require.Zero(t, atomic.LoadInt32(&client.refreshCalls))
}
