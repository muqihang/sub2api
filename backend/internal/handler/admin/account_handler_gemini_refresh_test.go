package admin

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type geminiRefreshOAuthClientStub struct {
	refreshResp *geminicli.TokenResponse
	refreshErr  error
}

func (s *geminiRefreshOAuthClientStub) ExchangeCode(ctx context.Context, oauthType, code, codeVerifier, redirectURI, proxyURL string) (*geminicli.TokenResponse, error) {
	return nil, nil
}

func (s *geminiRefreshOAuthClientStub) RefreshToken(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error) {
	if s.refreshErr != nil {
		return nil, s.refreshErr
	}
	return s.refreshResp, nil
}

func TestAccountHandlerRefreshSingleAccount_GeminiProtectsMergedCredentials(t *testing.T) {
	adminSvc := newStubAdminService()
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.CredentialEncryptionKey = strings.Repeat("5d", 32)

	protector, err := service.ProvideGeminiSecretProtector(cfg)
	require.NoError(t, err)
	protected, err := protector.ProtectCredentials(map[string]any{
		"access_token":  "old-access",
		"refresh_token": "old-refresh",
		"oauth_type":    "ai_studio",
	})
	require.NoError(t, err)

	handler := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		service.NewGeminiOAuthService(nil, &geminiRefreshOAuthClientStub{
			refreshResp: &geminicli.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
				ExpiresIn:    3600,
				TokenType:    "Bearer",
				Scope:        "openid email",
			},
		}, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	account := &service.Account{
		ID:          21,
		Platform:    service.PlatformGemini,
		Type:        service.AccountTypeOAuth,
		Credentials: protected,
	}

	_, warning, err := handler.refreshSingleAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "", warning)
	require.Len(t, adminSvc.updatedAccounts, 1)
	updated := adminSvc.updatedAccounts[0].input.Credentials
	require.True(t, strings.HasPrefix(updated["access_token"].(string), "genc:v1:"))
	require.True(t, strings.HasPrefix(updated["refresh_token"].(string), "genc:v1:"))
}
