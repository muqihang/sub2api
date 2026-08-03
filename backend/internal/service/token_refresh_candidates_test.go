package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type tokenRefreshCandidateSelectionRepo struct {
	AccountRepository
	listActiveAccounts              []Account
	listTokenRefreshCandidates      []Account
	listByPlatformAccounts          []Account
	listActiveCalls                 int
	listTokenRefreshCandidatesCalls int
	listByPlatformCalls             int
	setTempUnschedCalls             int
	lastTempUnschedReason           string
}

func (r *tokenRefreshCandidateSelectionRepo) ListActive(context.Context) ([]Account, error) {
	r.listActiveCalls++
	return append([]Account(nil), r.listActiveAccounts...), nil
}

func (r *tokenRefreshCandidateSelectionRepo) ListTokenRefreshCandidates(context.Context) ([]Account, error) {
	r.listTokenRefreshCandidatesCalls++
	return append([]Account(nil), r.listTokenRefreshCandidates...), nil
}

func (r *tokenRefreshCandidateSelectionRepo) ListByPlatform(_ context.Context, platform string) ([]Account, error) {
	r.listByPlatformCalls++
	if platform != PlatformOpenAI {
		return nil, nil
	}
	return append([]Account(nil), r.listByPlatformAccounts...), nil
}

func (r *tokenRefreshCandidateSelectionRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.setTempUnschedCalls++
	r.lastTempUnschedReason = reason
	return nil
}

func TestTokenRefreshCandidates_UsesRepositoryCandidatesInsteadOfBroadActive(t *testing.T) {
	expiresSoon := time.Now().Add(5 * time.Minute)
	repo := &tokenRefreshCandidateSelectionRepo{
		listActiveAccounts: []Account{
			{ID: 99, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive},
		},
		listTokenRefreshCandidates: []Account{
			{
				ID:        20,
				Platform:  PlatformAnthropic,
				Type:      AccountTypeSetupToken,
				Status:    StatusActive,
				ExpiresAt: &expiresSoon,
				Credentials: map[string]any{
					"expires_at": expiresSoon.Format(time.RFC3339),
				},
			},
		},
	}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil)

	accounts, err := svc.listManagedAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, repo.listTokenRefreshCandidatesCalls)
	require.Equal(t, 0, repo.listActiveCalls, "production repository supports candidate listing, so broad ListActive must not be used")
	require.Len(t, accounts, 1)
	require.Equal(t, int64(20), accounts[0].ID)
	require.Equal(t, AccountTypeSetupToken, accounts[0].Type)
}

func TestTokenRefreshCandidates_KeepsMalformedClaudeRefreshCandidate(t *testing.T) {
	expiresSoon := time.Now().Add(5 * time.Minute)
	repo := &tokenRefreshCandidateSelectionRepo{
		listTokenRefreshCandidates: []Account{
			{
				ID:        21,
				Platform:  PlatformAnthropic,
				Type:      AccountTypeSetupToken,
				Status:    StatusActive,
				ExpiresAt: &expiresSoon,
				Credentials: map[string]any{
					"expires_at": expiresSoon.Format(time.RFC3339),
				},
			},
		},
	}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil)

	accounts, err := svc.listManagedAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(21), accounts[0].ID)
	require.Equal(t, "", accounts[0].GetCredential("refresh_token"))
}

func TestTokenRefreshCandidates_SkipsRetryExhaustedOpenAISupplementOnly(t *testing.T) {
	future := time.Now().Add(10 * time.Minute)
	repo := &tokenRefreshCandidateSelectionRepo{
		listByPlatformAccounts: []Account{
			{
				ID:                      30,
				Platform:                PlatformOpenAI,
				Type:                    AccountTypeOAuth,
				Status:                  StatusActive,
				TempUnschedulableUntil:  &future,
				TempUnschedulableReason: TokenRefreshRetryExhaustedReasonPrefix + " transient upstream error",
				Extra: map[string]any{
					"openai_pool_role":    OpenAIPoolRoleMain,
					"openai_auth_state":   OpenAIAuthStateCooling,
					"openai_token_source": OpenAITokenSourceRTManaged,
				},
				Credentials: map[string]any{"refresh_token": "rt-retry-exhausted"},
			},
			{
				ID:                      31,
				Platform:                PlatformOpenAI,
				Type:                    AccountTypeOAuth,
				Status:                  StatusActive,
				TempUnschedulableUntil:  &future,
				TempUnschedulableReason: "request path 401 cooldown",
				Extra: map[string]any{
					"openai_pool_role":    OpenAIPoolRoleMain,
					"openai_auth_state":   OpenAIAuthStateCooling,
					"openai_token_source": OpenAITokenSourceRTManaged,
				},
				Credentials: map[string]any{"refresh_token": "rt-request-cooldown"},
			},
		},
	}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil)

	accounts, err := svc.listManagedAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, repo.listByPlatformCalls)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(31), accounts[0].ID)
}

func TestTokenRefreshCandidates_RetryExhaustedReasonUsesSharedPrefix(t *testing.T) {
	repo := &tokenRefreshCandidateSelectionRepo{}
	cfg := &config.Config{TokenRefresh: config.TokenRefreshConfig{MaxRetries: 1}}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	account := &Account{ID: 32, Platform: PlatformGemini, Type: AccountTypeOAuth}
	refresher := &tokenRefreshCandidateRefresher{err: errors.New("transient refresh failure")}

	err := svc.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Equal(t, 1, repo.setTempUnschedCalls)
	require.Contains(t, repo.lastTempUnschedReason, TokenRefreshRetryExhaustedReasonPrefix)
}

type tokenRefreshCandidateRefresher struct {
	err error
}

func (r *tokenRefreshCandidateRefresher) CanRefresh(*Account) bool                  { return true }
func (r *tokenRefreshCandidateRefresher) NeedsRefresh(*Account, time.Duration) bool { return true }
func (r *tokenRefreshCandidateRefresher) Refresh(context.Context, *Account) (map[string]any, error) {
	return nil, r.err
}
func (r *tokenRefreshCandidateRefresher) CacheKey(*Account) string {
	return "token-refresh-candidate-test"
}
