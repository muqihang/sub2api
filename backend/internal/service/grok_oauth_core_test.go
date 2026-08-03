package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokOAuthClientCoreStub struct {
	refreshResponse *xai.TokenResponse
	exchangeCalls   int
	refreshCalls    int
}

func (s *grokOAuthClientCoreStub) ExchangeCode(context.Context, string, string, string, string, string) (*xai.TokenResponse, error) {
	s.exchangeCalls++
	return &xai.TokenResponse{AccessToken: "exchange-access", RefreshToken: "exchange-refresh", TokenType: "Bearer", ExpiresIn: 3600}, nil
}

func (s *grokOAuthClientCoreStub) RefreshToken(context.Context, string, string, string) (*xai.TokenResponse, error) {
	s.refreshCalls++
	if s.refreshResponse != nil {
		return s.refreshResponse, nil
	}
	return &xai.TokenResponse{AccessToken: "refresh-access", RefreshToken: "refresh-rotated", TokenType: "Bearer", ExpiresIn: 3600}, nil
}

func TestGrokOAuthServiceExchangeCodeRequiresStateForCallbackURLAndConsumesSession(t *testing.T) {
	client := &grokOAuthClientCoreStub{}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	auth, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "http://127.0.0.1:56121/callback?code=code-without-state",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GROK_OAUTH_STATE_REQUIRED")
	require.Zero(t, client.exchangeCalls)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code-with-state",
		State:     auth.State,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GROK_OAUTH_SESSION_NOT_FOUND")
	require.Zero(t, client.exchangeCalls)
}

func TestGrokOAuthServiceRefreshTokenPreservesOriginalRefreshTokenWhenNotRotated(t *testing.T) {
	svc := NewGrokOAuthService(nil, &grokOAuthClientCoreStub{
		refreshResponse: &xai.TokenResponse{AccessToken: "new-access-token", TokenType: "Bearer", ExpiresIn: 3600},
	})
	defer svc.Stop()

	info, err := svc.RefreshToken(context.Background(), "original-refresh-token", "", "client-id")
	require.NoError(t, err)
	require.Equal(t, "new-access-token", info.AccessToken)
	require.Equal(t, "original-refresh-token", info.RefreshToken)
	require.Equal(t, "client-id", info.ClientID)
}

func TestGrokTokenRefresherRefreshPreservesBaseURLAndRotatedToken(t *testing.T) {
	svc := NewGrokOAuthService(nil, &grokOAuthClientCoreStub{
		refreshResponse: &xai.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", TokenType: "Bearer", ExpiresIn: 3600},
	})
	defer svc.Stop()
	refresher := NewGrokTokenRefresher(svc)
	account := &Account{ID: 77, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token":  "old-access",
		"refresh_token": "old-refresh",
		"base_url":      xai.DefaultCLIBaseURL,
		"client_id":     "client-id",
	}}

	creds, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "new-access", creds["access_token"])
	require.Equal(t, "new-refresh", creds["refresh_token"])
	require.Equal(t, xai.DefaultCLIBaseURL, creds["base_url"])
}

func TestGrokTokenProviderRefreshesExpiredTokenOnRequestPath(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	account := &Account{ID: 54, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token":  "expired-access-token",
		"refresh_token": "refresh-token",
		"expires_at":    expiredAt,
		"base_url":      xai.DefaultCLIBaseURL,
		"client_id":     "client-id",
	}}
	repo := &grokCoreAccountRepo{}
	repo.accountsByID = map[int64]*Account{54: account}
	cache := &grokTokenCacheForCoreTest{lockResult: true}
	oauthSvc := NewGrokOAuthService(nil, &grokOAuthClientCoreStub{
		refreshResponse: &xai.TokenResponse{AccessToken: "new-access-token", TokenType: "Bearer", ExpiresIn: 3600},
	})
	defer oauthSvc.Stop()

	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), NewGrokTokenRefresher(oauthSvc))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "new-access-token", token)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, "new-access-token", repo.accountsByID[54].GetCredential("access_token"))
	require.Equal(t, "refresh-token", repo.accountsByID[54].GetCredential("refresh_token"))
	require.Equal(t, xai.DefaultCLIBaseURL, repo.accountsByID[54].GetCredential("base_url"))
	require.Equal(t, "grok:account:54", cache.setKey)
	require.Equal(t, "new-access-token", cache.setToken)
	require.Greater(t, cache.setTTL, time.Duration(0))
	require.Equal(t, 1, cache.releaseCalls)
}

func TestGrokTokenProviderRefreshFailureUnschedulesWithRedactedReason(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	account := &Account{ID: 55, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token":  "expired-access-token",
		"refresh_token": "refresh-token",
		"expires_at":    expiredAt,
	}}
	repo := &grokCoreAccountRepo{}
	repo.accountsByID = map[int64]*Account{55: account}
	cache := &grokTokenCacheForCoreTest{lockResult: true}
	tempCache := &tempUnschedCacheCoreStub{}
	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &grokCoreRefresherStub{err: errors.New("temporary refresh failure access_token=leaked-access refresh_token=leaked-refresh")})
	provider.SetTempUnschedCache(tempCache)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Equal(t, 1, repo.setTempUnschedCalls)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Contains(t, repo.lastTempUnschedReason, "access_token=***")
	require.Contains(t, repo.lastTempUnschedReason, "refresh_token=***")
	require.NotContains(t, repo.lastTempUnschedReason, "leaked-access")
	require.NotContains(t, repo.lastTempUnschedReason, "leaked-refresh")
	require.Equal(t, 1, tempCache.setCalls)
	require.NotNil(t, tempCache.lastState)
	require.NotContains(t, tempCache.lastState.ErrorMessage, "leaked-access")
	require.NotContains(t, tempCache.lastState.ErrorMessage, "leaked-refresh")
}

type grokTokenCacheForCoreTest struct {
	token        string
	setKey       string
	setToken     string
	setTTL       time.Duration
	lockResult   bool
	releaseCalls int
}

func (c *grokTokenCacheForCoreTest) GetAccessToken(context.Context, string) (string, error) {
	if c.token == "" {
		return "", errors.New("not cached")
	}
	return c.token, nil
}

func (c *grokTokenCacheForCoreTest) SetAccessToken(_ context.Context, key string, token string, ttl time.Duration) error {
	c.setKey = key
	c.setToken = token
	c.setTTL = ttl
	return nil
}

func (c *grokTokenCacheForCoreTest) DeleteAccessToken(context.Context, string) error { return nil }
func (c *grokTokenCacheForCoreTest) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return c.lockResult, nil
}
func (c *grokTokenCacheForCoreTest) ReleaseRefreshLock(context.Context, string) error {
	c.releaseCalls++
	return nil
}

type tempUnschedCacheCoreStub struct {
	setCalls  int
	lastState *TempUnschedState
}

func (s *tempUnschedCacheCoreStub) SetTempUnsched(_ context.Context, _ int64, state *TempUnschedState) error {
	s.setCalls++
	s.lastState = state
	return nil
}
func (s *tempUnschedCacheCoreStub) GetTempUnsched(context.Context, int64) (*TempUnschedState, error) {
	return nil, nil
}
func (s *tempUnschedCacheCoreStub) DeleteTempUnsched(context.Context, int64) error { return nil }

type grokCoreAccountRepo struct {
	accountsByID           map[int64]*Account
	updateCredentialsCalls int
	setTempUnschedCalls    int
	setErrorCalls          int
	lastTempUnschedReason  string
	lastErrorMessage       string
}

func (r *grokCoreAccountRepo) Create(context.Context, *Account) error { return nil }
func (r *grokCoreAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.accountsByID != nil {
		if account, ok := r.accountsByID[id]; ok {
			return account, nil
		}
	}
	return nil, errors.New("account not found")
}
func (r *grokCoreAccountRepo) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	out := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account, ok := r.accountsByID[id]; ok {
			out = append(out, account)
		}
	}
	return out, nil
}
func (r *grokCoreAccountRepo) ExistsByID(_ context.Context, id int64) (bool, error) {
	_, ok := r.accountsByID[id]
	return ok, nil
}
func (r *grokCoreAccountRepo) GetByCRSAccountID(context.Context, string) (*Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) FindByExtraField(context.Context, string, any) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) ListCRSAccountIDs(context.Context) (map[string]int64, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) Update(_ context.Context, account *Account) error {
	if r.accountsByID == nil {
		r.accountsByID = map[int64]*Account{}
	}
	r.accountsByID[account.ID] = account
	return nil
}
func (r *grokCoreAccountRepo) Delete(context.Context, int64) error { return nil }
func (r *grokCoreAccountRepo) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *grokCoreAccountRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *grokCoreAccountRepo) ListByGroup(context.Context, int64) ([]Account, error) { return nil, nil }
func (r *grokCoreAccountRepo) ListActive(context.Context) ([]Account, error)         { return nil, nil }
func (r *grokCoreAccountRepo) ListByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) UpdateLastUsed(context.Context, int64) error { return nil }
func (r *grokCoreAccountRepo) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}
func (r *grokCoreAccountRepo) SetError(_ context.Context, _ int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMessage = errorMsg
	return nil
}
func (r *grokCoreAccountRepo) ClearError(context.Context, int64) error           { return nil }
func (r *grokCoreAccountRepo) SetSchedulable(context.Context, int64, bool) error { return nil }
func (r *grokCoreAccountRepo) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *grokCoreAccountRepo) BindGroups(context.Context, int64, []int64) error   { return nil }
func (r *grokCoreAccountRepo) ListSchedulable(context.Context) ([]Account, error) { return nil, nil }
func (r *grokCoreAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) ListSchedulableByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) ListSchedulableUngroupedByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *grokCoreAccountRepo) SetRateLimited(context.Context, int64, time.Time) error { return nil }
func (r *grokCoreAccountRepo) SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error {
	return nil
}
func (r *grokCoreAccountRepo) SetOverloaded(context.Context, int64, time.Time) error { return nil }
func (r *grokCoreAccountRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.setTempUnschedCalls++
	r.lastTempUnschedReason = reason
	return nil
}
func (r *grokCoreAccountRepo) ClearTempUnschedulable(context.Context, int64) error      { return nil }
func (r *grokCoreAccountRepo) ClearRateLimit(context.Context, int64) error              { return nil }
func (r *grokCoreAccountRepo) ClearAntigravityQuotaScopes(context.Context, int64) error { return nil }
func (r *grokCoreAccountRepo) ClearModelRateLimits(context.Context, int64) error        { return nil }
func (r *grokCoreAccountRepo) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	return nil
}
func (r *grokCoreAccountRepo) UpdateSessionWindowEnd(context.Context, int64, time.Time) error {
	return nil
}
func (r *grokCoreAccountRepo) UpdateExtra(context.Context, int64, map[string]any) error { return nil }
func (r *grokCoreAccountRepo) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	return 0, nil
}
func (r *grokCoreAccountRepo) IncrementQuotaUsed(context.Context, int64, float64) error { return nil }
func (r *grokCoreAccountRepo) ResetQuotaUsed(context.Context, int64) error              { return nil }
func (r *grokCoreAccountRepo) RevertProxyFallback(context.Context, int64) error         { return nil }
func (r *grokCoreAccountRepo) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	r.updateCredentialsCalls++
	if r.accountsByID == nil {
		r.accountsByID = map[int64]*Account{}
	}
	account := r.accountsByID[id]
	if account == nil {
		account = &Account{ID: id}
		r.accountsByID[id] = account
	}
	account.Credentials = cloneCredentials(credentials)
	return nil
}

type grokCoreRefresherStub struct{ err error }

func (r *grokCoreRefresherStub) CanRefresh(*Account) bool                  { return true }
func (r *grokCoreRefresherStub) NeedsRefresh(*Account, time.Duration) bool { return true }
func (r *grokCoreRefresherStub) Refresh(context.Context, *Account) (map[string]any, error) {
	return nil, r.err
}
func (r *grokCoreRefresherStub) CacheKey(account *Account) string { return GrokTokenCacheKey(account) }
