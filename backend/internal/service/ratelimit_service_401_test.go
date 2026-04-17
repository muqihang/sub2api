//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type rateLimitAccountRepoStub struct {
	mockAccountRepoForGemini
	setErrorCalls          int
	tempCalls              int
	updateCredentialsCalls int
	updateExtraCalls       int
	lastCredentials        map[string]any
	lastErrorMsg           string
	lastExtra              map[string]any
}

func (r *rateLimitAccountRepoStub) SetError(ctx context.Context, id int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = errorMsg
	return nil
}

func (r *rateLimitAccountRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	return nil
}

func (r *rateLimitAccountRepoStub) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	r.updateCredentialsCalls++
	r.lastCredentials = cloneCredentials(credentials)
	return nil
}

func (r *rateLimitAccountRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.updateExtraCalls++
	r.lastExtra = cloneCredentials(updates)
	return nil
}

type tokenCacheInvalidatorRecorder struct {
	accounts []*Account
	err      error
}

func (r *tokenCacheInvalidatorRecorder) InvalidateToken(ctx context.Context, account *Account) error {
	r.accounts = append(r.accounts, account)
	return r.err
}

type openAI401RefreshLockCacheStub struct{}

func (s *openAI401RefreshLockCacheStub) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	return "", errors.New("not cached")
}

func (s *openAI401RefreshLockCacheStub) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	return nil
}

func (s *openAI401RefreshLockCacheStub) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	return nil
}

func (s *openAI401RefreshLockCacheStub) AcquireRefreshLock(ctx context.Context, cacheKey string, ttl time.Duration) (bool, error) {
	return true, nil
}

func (s *openAI401RefreshLockCacheStub) ReleaseRefreshLock(ctx context.Context, cacheKey string) error {
	return nil
}

type openAI401ExecutorStub struct {
	credentials map[string]any
	err         error
}

func (s *openAI401ExecutorStub) CanRefresh(account *Account) bool { return true }
func (s *openAI401ExecutorStub) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	return true
}
func (s *openAI401ExecutorStub) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return cloneCredentials(s.credentials), nil
}
func (s *openAI401ExecutorStub) CacheKey(account *Account) string {
	return OpenAITokenCacheKey(account)
}

func TestRateLimitService_HandleUpstreamError_OAuth401SetsTempUnschedulable(t *testing.T) {
	t.Run("gemini", func(t *testing.T) {
		repo := &rateLimitAccountRepoStub{}
		invalidator := &tokenCacheInvalidatorRecorder{}
		service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		service.SetTokenCacheInvalidator(invalidator)
		account := &Account{
			ID:       100,
			Platform: PlatformGemini,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       401,
						"keywords":         []any{"unauthorized"},
						"duration_minutes": 30,
						"description":      "custom rule",
					},
				},
			},
		}

		shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

		require.True(t, shouldDisable)
		require.Equal(t, 0, repo.setErrorCalls)
		require.Equal(t, 1, repo.tempCalls)
		require.Len(t, invalidator.accounts, 1)
	})

	t.Run("antigravity_401_uses_SetError", func(t *testing.T) {
		// Antigravity 401 由 applyErrorPolicy 的 temp_unschedulable_rules 控制，
		// HandleUpstreamError 中走 SetError 路径。
		repo := &rateLimitAccountRepoStub{}
		invalidator := &tokenCacheInvalidatorRecorder{}
		service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		service.SetTokenCacheInvalidator(invalidator)
		account := &Account{
			ID:       100,
			Platform: PlatformAntigravity,
			Type:     AccountTypeOAuth,
		}

		shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

		require.True(t, shouldDisable)
		require.Equal(t, 1, repo.setErrorCalls)
		require.Equal(t, 0, repo.tempCalls)
		require.Empty(t, invalidator.accounts)
	})
}

// TestRateLimitService_HandleUpstreamError_OAuth401InvalidatorError
// OpenAI OAuth 401 缓存失效出错时仍走 temp_unschedulable
func TestRateLimitService_HandleUpstreamError_OAuth401InvalidatorError(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{err: errors.New("boom")}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       101,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Len(t, invalidator.accounts, 1)
}

func TestRateLimitService_HandleUpstreamError_NonOAuth401(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       102,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Empty(t, invalidator.accounts)
}

func TestRateLimitService_HandleUpstreamError_OAuth401UsesCredentialsUpdater(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       103,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token",
		},
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.NotEmpty(t, repo.lastCredentials["expires_at"])
}

func TestRateLimitService_HandleUpstreamError_OpenAI401RefreshSuccessAvoidsDisableState(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	account := &Account{
		ID:       104,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-at",
			"refresh_token": "rt-1",
			"expires_at":    time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
		},
		Extra: map[string]any{
			"openai_pool_role":    OpenAIPoolRoleMain,
			"openai_auth_state":   OpenAIAuthStateHealthy,
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
	}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	invalidator := &tokenCacheInvalidatorRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	service.SetOpenAIAuthRecovery(
		NewOAuthRefreshAPI(repo, &openAI401RefreshLockCacheStub{}),
		&openAI401ExecutorStub{credentials: map[string]any{
			"access_token":  "new-at",
			"refresh_token": "new-rt",
			"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		}},
	)

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte(`{"detail":"Unauthorized"}`))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Len(t, invalidator.accounts, 1)
	require.Equal(t, OpenAIAuthStateHealthy, repo.lastExtra["openai_auth_state"])
	require.Equal(t, OpenAIValidationOutcomeRTValidated, repo.lastExtra["openai_validation_outcome"])
}

func TestRateLimitService_HandleUpstreamError_OpenAI401RetryableRefreshFailureCoolsDown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	account := &Account{
		ID:       105,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-at",
			"refresh_token": "rt-1",
		},
		Extra: map[string]any{
			"openai_pool_role":    OpenAIPoolRoleMain,
			"openai_auth_state":   OpenAIAuthStateHealthy,
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
	}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAIAuthRecovery(
		NewOAuthRefreshAPI(repo, &openAI401RefreshLockCacheStub{}),
		&openAI401ExecutorStub{err: errors.New("network timeout")},
	)

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte(`{"detail":"Unauthorized"}`))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, OpenAIAuthStateCooling, repo.lastExtra["openai_auth_state"])
	require.Equal(t, OpenAIValidationOutcomeRTValidationRetryableFailure, repo.lastExtra["openai_validation_outcome"])
}

func TestRateLimitService_HandleUpstreamError_OpenAI401TerminalRefreshFailureSetsError(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	account := &Account{
		ID:       106,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-at",
			"refresh_token": "rt-1",
		},
		Extra: map[string]any{
			"openai_pool_role":    OpenAIPoolRoleMain,
			"openai_auth_state":   OpenAIAuthStateHealthy,
			"openai_token_source": OpenAITokenSourceRTManaged,
		},
	}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAIAuthRecovery(
		NewOAuthRefreshAPI(repo, &openAI401RefreshLockCacheStub{}),
		&openAI401ExecutorStub{err: errors.New("invalid_grant: refresh token expired")},
	)

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte(`{"detail":"Unauthorized"}`))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, OpenAIAuthStateTerminal, repo.lastExtra["openai_auth_state"])
	require.Equal(t, OpenAIValidationOutcomeRTValidationTerminalFailure, repo.lastExtra["openai_validation_outcome"])
}
