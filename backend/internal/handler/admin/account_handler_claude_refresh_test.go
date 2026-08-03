package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type claudeRefreshOAuthClientStub struct {
	refreshErr error
}

func (s *claudeRefreshOAuthClientStub) GetOrganizationUUID(context.Context, string, string) (string, error) {
	return "", nil
}

func (s *claudeRefreshOAuthClientStub) GetAuthorizationCode(context.Context, string, string, string, string, string, string) (string, error) {
	return "", nil
}

func (s *claudeRefreshOAuthClientStub) ExchangeCodeForToken(context.Context, string, string, string, string, bool) (*oauth.TokenResponse, error) {
	return nil, nil
}

func (s *claudeRefreshOAuthClientStub) RefreshToken(context.Context, string, string) (*oauth.TokenResponse, error) {
	return nil, s.refreshErr
}

type accountRefreshProxyRepoStub struct{}

func (s accountRefreshProxyRepoStub) GetByID(context.Context, int64) (*service.Proxy, error) {
	return &service.Proxy{ID: 9, Protocol: "socks5", Host: "127.0.0.1", Port: 1080, Status: service.StatusActive}, nil
}

func (s accountRefreshProxyRepoStub) Create(context.Context, *service.Proxy) error { return nil }
func (s accountRefreshProxyRepoStub) ListByIDs(context.Context, []int64) ([]service.Proxy, error) {
	return nil, nil
}
func (s accountRefreshProxyRepoStub) Update(context.Context, *service.Proxy) error { return nil }
func (s accountRefreshProxyRepoStub) Delete(context.Context, int64) error          { return nil }
func (s accountRefreshProxyRepoStub) List(context.Context, pagination.PaginationParams) ([]service.Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s accountRefreshProxyRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]service.Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s accountRefreshProxyRepoStub) ListWithFiltersAndAccountCount(context.Context, pagination.PaginationParams, string, string, string) ([]service.ProxyWithAccountCount, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s accountRefreshProxyRepoStub) ListActive(context.Context) ([]service.Proxy, error) {
	return nil, nil
}
func (s accountRefreshProxyRepoStub) ListActiveWithAccountCount(context.Context) ([]service.ProxyWithAccountCount, error) {
	return nil, nil
}
func (s accountRefreshProxyRepoStub) ExistsByHostPortAuth(context.Context, string, int, string, string) (bool, error) {
	return false, nil
}
func (s accountRefreshProxyRepoStub) CountAccountsByProxyID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s accountRefreshProxyRepoStub) ListAccountSummariesByProxyID(context.Context, int64) ([]service.ProxyAccountSummary, error) {
	return nil, nil
}
func (s accountRefreshProxyRepoStub) SweepExpiredProxies(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (s accountRefreshProxyRepoStub) ListAllForFallback(context.Context) ([]service.Proxy, error) {
	return nil, nil
}
func (s accountRefreshProxyRepoStub) CountExpired(context.Context) (int64, error) {
	return 0, nil
}
func (s accountRefreshProxyRepoStub) CountExpiringSoon(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestAccountHandlerRefreshSingleAccount_ClaudeSetupTokenInvalidGrantReturnsSafeBadRequest(t *testing.T) {
	proxyID := int64(9)
	handler := NewAccountHandler(
		newStubAdminService(),
		service.NewOAuthService(accountRefreshProxyRepoStub{}, &claudeRefreshOAuthClientStub{
			refreshErr: errors.New("invalid_grant: Refresh token not found or invalid"),
		}),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	account := &service.Account{
		ID:       15,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeSetupToken,
		ProxyID:  &proxyID,
		Credentials: map[string]any{
			"refresh_token": "refresh-secret-do-not-leak",
		},
	}

	_, _, err := handler.refreshSingleAccount(context.Background(), account)
	require.Error(t, err)
	require.Equal(t, 400, infraerrors.Code(err))
	require.Equal(t, "REFRESH_TOKEN_INVALID", infraerrors.Reason(err))
	require.Contains(t, infraerrors.Message(err), "Refresh Token")
	require.Contains(t, infraerrors.Message(err), "Setup Token")
	require.NotContains(t, infraerrors.Message(err), "refresh-secret-do-not-leak")
	require.NotContains(t, err.Error(), "refresh-secret-do-not-leak")
}
