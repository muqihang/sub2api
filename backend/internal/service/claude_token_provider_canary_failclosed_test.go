package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type canaryRefreshTokenCacheStub struct {
	mu     sync.Mutex
	tokens map[string]string
}

func newCanaryRefreshTokenCacheStub() *canaryRefreshTokenCacheStub {
	return &canaryRefreshTokenCacheStub{tokens: map[string]string{}}
}

func (c *canaryRefreshTokenCacheStub) GetAccessToken(_ context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens[key], nil
}

func (c *canaryRefreshTokenCacheStub) SetAccessToken(_ context.Context, key, token string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[key] = token
	return nil
}

func (c *canaryRefreshTokenCacheStub) DeleteAccessToken(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, key)
	return nil
}

func (c *canaryRefreshTokenCacheStub) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (c *canaryRefreshTokenCacheStub) ReleaseRefreshLock(context.Context, string) error {
	return nil
}

type canaryRefreshAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *canaryRefreshAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *canaryRefreshAccountRepo) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	if r.account == nil || r.account.ID != id {
		r.account = &Account{ID: id}
	}
	r.account.Credentials = cloneCredentials(credentials)
	return nil
}

type canaryRefreshExecutorStub struct {
	needsRefresh bool
	err          error
	refreshCalls int
}

func (e *canaryRefreshExecutorStub) CanRefresh(*Account) bool { return true }
func (e *canaryRefreshExecutorStub) NeedsRefresh(*Account, time.Duration) bool {
	return e.needsRefresh
}
func (e *canaryRefreshExecutorStub) Refresh(context.Context, *Account) (map[string]any, error) {
	e.refreshCalls++
	if e.err != nil {
		return nil, e.err
	}
	return map[string]any{"access_token": "new-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}, nil
}
func (e *canaryRefreshExecutorStub) CacheKey(account *Account) string {
	return ClaudeTokenCacheKey(account)
}

func TestClaudeTokenProviderCanaryRefreshErrorFailsClosed(t *testing.T) {
	expiresAt := time.Now().Add(time.Minute).Format(time.RFC3339)
	account := &Account{
		ID:       3001,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
		Extra: map[string]any{
			"cc_gateway_canary_only": true,
		},
	}
	repo := &canaryRefreshAccountRepo{account: account}
	cache := newCanaryRefreshTokenCacheStub()
	executor := &canaryRefreshExecutorStub{
		needsRefresh: true,
		err:          errors.New("proxy refused"),
	}
	api := NewOAuthRefreshAPI(repo, cache)
	provider := NewClaudeTokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(api, executor)

	token, err := provider.GetAccessToken(context.Background(), account)

	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "proxy refused")
	require.Equal(t, 1, executor.refreshCalls)
}

func TestClaudeTokenProviderExplicitRefreshFailClosedFailsClosed(t *testing.T) {
	expiresAt := time.Now().Add(time.Minute).Format(time.RFC3339)
	account := &Account{
		ID:       3002,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
		Extra: map[string]any{
			"oauth_refresh_fail_closed": "true",
		},
	}
	repo := &canaryRefreshAccountRepo{account: account}
	cache := newCanaryRefreshTokenCacheStub()
	executor := &canaryRefreshExecutorStub{
		needsRefresh: true,
		err:          errors.New("refresh unavailable"),
	}
	api := NewOAuthRefreshAPI(repo, cache)
	provider := NewClaudeTokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(api, executor)

	token, err := provider.GetAccessToken(context.Background(), account)

	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "refresh unavailable")
}
