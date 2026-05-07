package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type openaiTokenRefresherScopeClientStub struct {
	resp *openai.TokenResponse
	err  error
}

func (s *openaiTokenRefresherScopeClientStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiTokenRefresherScopeClientStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiTokenRefresherScopeClientStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func TestOpenAITokenRefresher_RejectsResponsesWriteScopeMissing(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiTokenRefresherScopeClientStub{
		resp: &openai.TokenResponse{
			AccessToken:  "at-new",
			RefreshToken: "rt-new",
			ExpiresIn:    3600,
			Scope:        "openid email profile model.request model.read",
		},
	})
	refresher := NewOpenAITokenRefresher(svc, nil)

	account := &Account{
		ID:       73,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt-old",
			"client_id":     "client-1",
		},
		Extra: map[string]any{
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
	}

	_, err := refresher.Refresh(context.Background(), account)
	require.Error(t, err)
	require.ErrorContains(t, err, openAIAuthErrorCodeResponsesWriteMissing)
}

func TestOpenAITokenRefresher_RefreshReturnsEncryptedCredentialsWhenConfigured(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiTokenRefresherScopeClientStub{
		resp: &openai.TokenResponse{
			AccessToken:  "at-new",
			RefreshToken: "rt-new",
			ExpiresIn:    3600,
			Scope:        "openid email profile api.responses.write",
		},
	})
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("88", 32)
	svc.SetGatewayCoreService(NewOpenAIGatewayCoreService(nil, cfg, nil))
	refresher := NewOpenAITokenRefresher(svc, nil)

	account := &Account{
		ID:       74,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt-old",
			"client_id":     "client-1",
		},
		Extra: map[string]any{
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
	}

	creds, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(creds["access_token"].(string), openAISecretProtectorPrefix))
	require.True(t, strings.HasPrefix(creds["refresh_token"].(string), openAISecretProtectorPrefix))
}
