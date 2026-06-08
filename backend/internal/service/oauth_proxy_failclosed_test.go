package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type oauthProxyFailClosedClient struct {
	getOrgUUIDFunc   func(ctx context.Context, sessionKey, proxyURL string) (string, error)
	exchangeCodeFunc func(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*oauth.TokenResponse, error)
	refreshTokenFunc func(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error)
}

func (m *oauthProxyFailClosedClient) GetOrganizationUUID(ctx context.Context, sessionKey, proxyURL string) (string, error) {
	if m.getOrgUUIDFunc != nil {
		return m.getOrgUUIDFunc(ctx, sessionKey, proxyURL)
	}
	panic("GetOrganizationUUID not implemented")
}

func (m *oauthProxyFailClosedClient) GetAuthorizationCode(ctx context.Context, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL string) (string, error) {
	panic("GetAuthorizationCode not implemented")
}

func (m *oauthProxyFailClosedClient) ExchangeCodeForToken(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*oauth.TokenResponse, error) {
	if m.exchangeCodeFunc != nil {
		return m.exchangeCodeFunc(ctx, code, codeVerifier, state, proxyURL, isSetupToken)
	}
	panic("ExchangeCodeForToken not implemented")
}

func (m *oauthProxyFailClosedClient) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error) {
	if m.refreshTokenFunc != nil {
		return m.refreshTokenFunc(ctx, refreshToken, proxyURL)
	}
	panic("RefreshToken not implemented")
}

type oauthProxyFailClosedRepo struct {
	getByIDFunc func(ctx context.Context, id int64) (*Proxy, error)
}

func (m *oauthProxyFailClosedRepo) Create(ctx context.Context, proxy *Proxy) error {
	panic("Create not implemented")
}
func (m *oauthProxyFailClosedRepo) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, fmt.Errorf("proxy not found")
}
func (m *oauthProxyFailClosedRepo) ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	panic("ListByIDs not implemented")
}
func (m *oauthProxyFailClosedRepo) Update(ctx context.Context, proxy *Proxy) error {
	panic("Update not implemented")
}
func (m *oauthProxyFailClosedRepo) Delete(ctx context.Context, id int64) error {
	panic("Delete not implemented")
}
func (m *oauthProxyFailClosedRepo) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	panic("List not implemented")
}
func (m *oauthProxyFailClosedRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
	panic("ListWithFilters not implemented")
}
func (m *oauthProxyFailClosedRepo) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	panic("ListWithFiltersAndAccountCount not implemented")
}
func (m *oauthProxyFailClosedRepo) ListActive(ctx context.Context) ([]Proxy, error) {
	panic("ListActive not implemented")
}
func (m *oauthProxyFailClosedRepo) ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error) {
	panic("ListActiveWithAccountCount not implemented")
}
func (m *oauthProxyFailClosedRepo) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	panic("ExistsByHostPortAuth not implemented")
}
func (m *oauthProxyFailClosedRepo) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	panic("CountAccountsByProxyID not implemented")
}
func (m *oauthProxyFailClosedRepo) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	panic("ListAccountSummariesByProxyID not implemented")
}
func (m *oauthProxyFailClosedRepo) SweepExpiredProxies(ctx context.Context, now time.Time) (int64, error) {
	panic("SweepExpiredProxies not implemented")
}
func (m *oauthProxyFailClosedRepo) ListAllForFallback(ctx context.Context) ([]Proxy, error) {
	panic("ListAllForFallback not implemented")
}
func (m *oauthProxyFailClosedRepo) CountExpired(ctx context.Context) (int64, error) {
	panic("CountExpired not implemented")
}
func (m *oauthProxyFailClosedRepo) CountExpiringSoon(ctx context.Context, now time.Time) (int64, error) {
	panic("CountExpiringSoon not implemented")
}

func TestOAuthService_GenerateAuthURL_ProxyLookupFailureFailsClosed(t *testing.T) {
	t.Parallel()

	proxyRepo := &oauthProxyFailClosedRepo{
		getByIDFunc: func(ctx context.Context, id int64) (*Proxy, error) {
			return nil, fmt.Errorf("proxy lookup failed")
		},
	}
	svc := NewOAuthService(proxyRepo, &oauthProxyFailClosedClient{})
	defer svc.Stop()

	proxyID := int64(99)
	_, err := svc.GenerateAuthURL(context.Background(), &proxyID)
	if err == nil {
		t.Fatal("GenerateAuthURL 应在指定代理不可用时 fail closed")
	}
	if !containsAllOAuthProxyFailClosed(err.Error(), []string{"proxy", "99"}) {
		t.Fatalf("错误信息应包含 proxy id: got=%q", err.Error())
	}
}

func TestOAuthService_ExchangeCode_ProxyLookupFailureFailsClosedWithoutExchange(t *testing.T) {
	t.Parallel()

	exchangeCalled := false
	client := &oauthProxyFailClosedClient{
		exchangeCodeFunc: func(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*oauth.TokenResponse, error) {
			exchangeCalled = true
			return nil, fmt.Errorf("must not exchange without proxy")
		},
	}
	proxyRepo := &oauthProxyFailClosedRepo{
		getByIDFunc: func(ctx context.Context, id int64) (*Proxy, error) {
			return nil, fmt.Errorf("proxy unavailable")
		},
	}
	svc := NewOAuthService(proxyRepo, client)
	defer svc.Stop()

	result, err := svc.GenerateAuthURL(context.Background(), nil)
	if err != nil {
		t.Fatalf("GenerateAuthURL 返回错误: %v", err)
	}

	proxyID := int64(100)
	_, err = svc.ExchangeCode(context.Background(), &ExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "auth-code",
		ProxyID:   &proxyID,
	})
	if err == nil {
		t.Fatal("ExchangeCode 应在指定代理不可用时 fail closed")
	}
	if exchangeCalled {
		t.Fatal("ExchangeCodeForToken 不应在代理不可用时被调用")
	}
}

func TestOAuthService_ExchangeCode_UsesSessionProxyFromGeneratedAuthURL(t *testing.T) {
	t.Parallel()

	const wantProxyURL = "http://127.0.0.1:18080"
	var gotProxyURL string
	client := &oauthProxyFailClosedClient{
		exchangeCodeFunc: func(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*oauth.TokenResponse, error) {
			gotProxyURL = proxyURL
			return &oauth.TokenResponse{
				AccessToken: "at",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			}, nil
		},
	}
	proxyRepo := &oauthProxyFailClosedRepo{
		getByIDFunc: func(ctx context.Context, id int64) (*Proxy, error) {
			return &Proxy{
				ID:       id,
				Protocol: "http",
				Host:     "127.0.0.1",
				Port:     18080,
			}, nil
		},
	}
	svc := NewOAuthService(proxyRepo, client)
	defer svc.Stop()

	proxyID := int64(18080)
	result, err := svc.GenerateAuthURL(context.Background(), &proxyID)
	if err != nil {
		t.Fatalf("GenerateAuthURL 返回错误: %v", err)
	}

	_, err = svc.ExchangeCode(context.Background(), &ExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "auth-code",
	})
	if err != nil {
		t.Fatalf("ExchangeCode 返回错误: %v", err)
	}
	if gotProxyURL != wantProxyURL {
		t.Fatalf("ExchangeCodeForToken proxyURL 不匹配: got=%q want=%q", gotProxyURL, wantProxyURL)
	}
}

func TestOAuthService_RefreshAccountToken_ProxyLookupFailureFailsClosedWithoutRefresh(t *testing.T) {
	t.Parallel()

	refreshCalled := false
	client := &oauthProxyFailClosedClient{
		refreshTokenFunc: func(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error) {
			refreshCalled = true
			return nil, fmt.Errorf("must not refresh without proxy")
		},
	}
	proxyRepo := &oauthProxyFailClosedRepo{
		getByIDFunc: func(ctx context.Context, id int64) (*Proxy, error) {
			return nil, fmt.Errorf("proxy unavailable")
		},
	}
	svc := NewOAuthService(proxyRepo, client)
	defer svc.Stop()

	proxyID := int64(101)
	account := &Account{
		ID:       5,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		ProxyID:  &proxyID,
		Credentials: map[string]any{
			"refresh_token": "rt",
		},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	if err == nil {
		t.Fatal("RefreshAccountToken 应在账号代理不可用时 fail closed")
	}
	if refreshCalled {
		t.Fatal("RefreshToken 不应在代理不可用时被调用")
	}
}

func TestOAuthService_RefreshAccountToken_NoProxyIDFailsClosedWithoutRefresh(t *testing.T) {
	t.Parallel()

	refreshCalled := false
	client := &oauthProxyFailClosedClient{
		refreshTokenFunc: func(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error) {
			refreshCalled = true
			return nil, fmt.Errorf("must not refresh without account proxy")
		},
	}
	svc := NewOAuthService(&oauthProxyFailClosedRepo{}, client)
	defer svc.Stop()

	account := &Account{
		ID:       6,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt",
		},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	if err == nil {
		t.Fatal("RefreshAccountToken 应在 account.ProxyID 为空时 fail closed")
	}
	if refreshCalled {
		t.Fatal("RefreshToken 不应在 account.ProxyID 为空时被调用")
	}
}

func TestOAuthService_CookieAuth_ProxyLookupFailureFailsClosedWithoutOutbound(t *testing.T) {
	t.Parallel()

	getOrgCalled := false
	client := &oauthProxyFailClosedClient{
		getOrgUUIDFunc: func(ctx context.Context, sessionKey, proxyURL string) (string, error) {
			getOrgCalled = true
			return "", fmt.Errorf("must not call cookie auth without proxy")
		},
	}
	proxyRepo := &oauthProxyFailClosedRepo{
		getByIDFunc: func(ctx context.Context, id int64) (*Proxy, error) {
			return nil, fmt.Errorf("proxy unavailable")
		},
	}
	svc := NewOAuthService(proxyRepo, client)
	defer svc.Stop()

	proxyID := int64(102)
	_, err := svc.CookieAuth(context.Background(), &CookieAuthInput{
		SessionKey: "session-key",
		ProxyID:    &proxyID,
		Scope:      "full",
	})
	if err == nil {
		t.Fatal("CookieAuth 应在指定代理不可用时 fail closed")
	}
	if getOrgCalled {
		t.Fatal("GetOrganizationUUID 不应在代理不可用时被调用")
	}
}

func containsAllOAuthProxyFailClosed(text string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
