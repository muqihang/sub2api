package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type formalRateLimitRepo struct {
	accountsByID   map[int64]*Account
	setErrorCalls  int
	tempCalls      int
	rateLimitCalls int
	lastErrorMsg   string
	lastTempReason string
	lastResetAt    time.Time
	cloneOnGet     bool
}

type formalTempUnschedCache struct {
	states map[int64]*TempUnschedState
}

func (c *formalTempUnschedCache) SetTempUnsched(_ context.Context, accountID int64, state *TempUnschedState) error {
	if c.states == nil {
		c.states = map[int64]*TempUnschedState{}
	}
	c.states[accountID] = state
	return nil
}

func (c *formalTempUnschedCache) GetTempUnsched(_ context.Context, accountID int64) (*TempUnschedState, error) {
	if c.states == nil {
		return nil, nil
	}
	return c.states[accountID], nil
}

func (c *formalTempUnschedCache) DeleteTempUnsched(context.Context, int64) error { return nil }

func (r *formalRateLimitRepo) Create(context.Context, *Account) error { return nil }
func (r *formalRateLimitRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.accountsByID != nil {
		if a := r.accountsByID[id]; a != nil {
			if r.cloneOnGet {
				return cloneFormalRateLimitAccount(a), nil
			}
			return a, nil
		}
	}
	return nil, errors.New("account not found")
}

func cloneFormalRateLimitAccount(account *Account) *Account {
	if account == nil {
		return nil
	}
	out := *account
	out.Credentials = cloneCredentials(account.Credentials)
	out.Extra = cloneCredentials(account.Extra)
	return &out
}
func (r *formalRateLimitRepo) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	var out []*Account
	for _, id := range ids {
		if a, _ := r.GetByID(context.Background(), id); a != nil {
			out = append(out, a)
		}
	}
	return out, nil
}
func (r *formalRateLimitRepo) ExistsByID(_ context.Context, id int64) (bool, error) {
	_, ok := r.accountsByID[id]
	return ok, nil
}
func (r *formalRateLimitRepo) GetByCRSAccountID(context.Context, string) (*Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) FindByExtraField(context.Context, string, any) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) ListCRSAccountIDs(context.Context) (map[string]int64, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) Update(_ context.Context, account *Account) error {
	if r.accountsByID == nil {
		r.accountsByID = map[int64]*Account{}
	}
	r.accountsByID[account.ID] = account
	return nil
}
func (r *formalRateLimitRepo) Delete(context.Context, int64) error { return nil }
func (r *formalRateLimitRepo) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *formalRateLimitRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *formalRateLimitRepo) ListByGroup(context.Context, int64) ([]Account, error) { return nil, nil }
func (r *formalRateLimitRepo) ListActive(context.Context) ([]Account, error)         { return nil, nil }
func (r *formalRateLimitRepo) ListByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) UpdateLastUsed(context.Context, int64) error { return nil }
func (r *formalRateLimitRepo) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}
func (r *formalRateLimitRepo) SetError(_ context.Context, _ int64, msg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = msg
	return nil
}
func (r *formalRateLimitRepo) ClearError(context.Context, int64) error           { return nil }
func (r *formalRateLimitRepo) SetSchedulable(context.Context, int64, bool) error { return nil }
func (r *formalRateLimitRepo) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *formalRateLimitRepo) BindGroups(context.Context, int64, []int64) error   { return nil }
func (r *formalRateLimitRepo) ListSchedulable(context.Context) ([]Account, error) { return nil, nil }
func (r *formalRateLimitRepo) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) ListSchedulableByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) ListSchedulableUngroupedByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *formalRateLimitRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastResetAt = resetAt
	return nil
}
func (r *formalRateLimitRepo) SetModelRateLimit(context.Context, int64, string, time.Time) error {
	return nil
}
func (r *formalRateLimitRepo) SetOverloaded(context.Context, int64, time.Time) error { return nil }
func (r *formalRateLimitRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.tempCalls++
	r.lastTempReason = reason
	return nil
}
func (r *formalRateLimitRepo) ClearTempUnschedulable(context.Context, int64) error      { return nil }
func (r *formalRateLimitRepo) ClearRateLimit(context.Context, int64) error              { return nil }
func (r *formalRateLimitRepo) ClearAntigravityQuotaScopes(context.Context, int64) error { return nil }
func (r *formalRateLimitRepo) ClearModelRateLimits(context.Context, int64) error        { return nil }
func (r *formalRateLimitRepo) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	return nil
}
func (r *formalRateLimitRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	if r.accountsByID == nil {
		r.accountsByID = map[int64]*Account{}
	}
	account := r.accountsByID[id]
	if account == nil {
		account = &Account{ID: id}
		r.accountsByID[id] = account
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	for k, v := range updates {
		account.Extra[k] = v
	}
	return nil
}
func (r *formalRateLimitRepo) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	return 0, nil
}
func (r *formalRateLimitRepo) IncrementQuotaUsed(context.Context, int64, float64) error { return nil }
func (r *formalRateLimitRepo) ResetQuotaUsed(context.Context, int64) error              { return nil }

func TestRateLimitService_FormalPoolAnthropic429PersistsSafeWindowExtraWithoutQuarantine(t *testing.T) {
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{91: {
		ID:          91,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction},
	}}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.01")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.50")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200")

	shouldDisable := service.HandleUpstreamError(context.Background(), repo.accountsByID[91], http.StatusTooManyRequests, headers, []byte(`{"error":{"message":"rate limit exceeded"}}`))

	require.False(t, shouldDisable)
	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, int64(1770998400), repo.lastResetAt.Unix())
	require.Equal(t, 0, repo.setErrorCalls)
	account := repo.accountsByID[91]
	require.Equal(t, FormalPoolStageProduction, account.Extra[FormalPoolExtraOnboardingStage])
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
	require.Equal(t, "rate_limited", account.Extra["formal_pool_rate_limit_error_class"])
	require.Equal(t, "5h", account.Extra["formal_pool_rate_limit_window"])
	require.Equal(t, "rate_limited", account.Extra["formal_pool_rate_limit_action"])
	require.Equal(t, "past", account.Extra["formal_pool_rate_limit_reset_bucket"])
	require.NotEmpty(t, account.Extra["formal_pool_rate_limit_last_at"])
}

func TestRateLimitService_FormalPoolAnthropic429NoResetPersistsPassThroughExtra(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantClass string
	}{
		{name: "long context usage credits", body: `{"error":{"message":"long context usage credits required for this request"}}`, wantClass: "long_context_usage_credits"},
		{name: "usage credits required", body: `{"error":{"message":"usage credits required for this request"}}`, wantClass: "usage_credits_required"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := int64(92 + i)
			repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{id: {
				ID:          id,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeSetupToken,
				Status:      StatusActive,
				Schedulable: true,
				Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction},
			}}}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

			shouldDisable := service.HandleUpstreamError(context.Background(), repo.accountsByID[id], http.StatusTooManyRequests, http.Header{}, []byte(tc.body))

			require.False(t, shouldDisable)
			require.Equal(t, 0, repo.rateLimitCalls)
			account := repo.accountsByID[id]
			require.Equal(t, FormalPoolStageProduction, account.Extra[FormalPoolExtraOnboardingStage])
			require.Equal(t, tc.wantClass, account.Extra["formal_pool_rate_limit_error_class"])
			require.Equal(t, "no_reset", account.Extra["formal_pool_rate_limit_window"])
			require.Equal(t, "pass_through", account.Extra["formal_pool_rate_limit_action"])
			require.Equal(t, "missing", account.Extra["formal_pool_rate_limit_reset_bucket"])
			require.NotEmpty(t, account.Extra["formal_pool_rate_limit_last_at"])
		})
	}
}

func TestRateLimitService_FormalPoolSetupTokenFirst401WithRefreshTokenMarksRefreshRequired(t *testing.T) {
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{77: {
		ID:          77,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"refresh_token": "refresh-token"},
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction},
	}}}
	tempCache := &formalTempUnschedCache{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, tempCache)
	account := repo.accountsByID[77]

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusUnauthorized, http.Header{}, []byte(`{"error":{"message":"invalid credentials"}}`))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, FormalPoolStageProduction, repo.accountsByID[77].Extra[FormalPoolExtraOnboardingStage])
	require.True(t, repo.accountsByID[77].Schedulable)
	require.Equal(t, StatusActive, repo.accountsByID[77].Status)
	require.Empty(t, repo.accountsByID[77].Extra[FormalPoolExtraRiskEventRef])
	require.Contains(t, repo.lastTempReason, "refresh_required")
	require.NotNil(t, tempCache.states[77])
	require.Equal(t, http.StatusUnauthorized, tempCache.states[77].StatusCode)
	require.Equal(t, "refresh_required", tempCache.states[77].MatchedKeyword)
	status, err := service.GetTempUnschedStatus(context.Background(), 77)
	require.NoError(t, err)
	require.NotNil(t, status)
	require.Equal(t, "refresh_required", status.MatchedKeyword)
}

func TestRateLimitService_FormalPoolSetupTokenSecond401AfterRefreshQuarantines(t *testing.T) {
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{77: {
		ID:          77,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"refresh_token": "refresh-token"},
		Extra: map[string]any{
			FormalPoolExtraOnboardingStage:       FormalPoolStageProduction,
			"formal_pool_auth_refresh_attempted": true,
		},
	}}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := repo.accountsByID[77]

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusUnauthorized, http.Header{}, []byte(`{"error":{"message":"invalid credentials"}}`))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[77].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accountsByID[77].Schedulable)
	require.Equal(t, StatusError, repo.accountsByID[77].Status)
}

func TestRateLimitService_FormalPoolInvalidGrantQuarantinesWithRefreshTokenInvalid(t *testing.T) {
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{81: {
		ID:          81,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"refresh_token": "refresh-token"},
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction},
	}}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := repo.accountsByID[81]

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusUnauthorized, http.Header{}, []byte(`{"error":{"message":"invalid_grant: Refresh token not found or invalid"}}`))

	require.True(t, shouldDisable)
	require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[81].Extra[FormalPoolExtraOnboardingStage])
	require.Equal(t, "refresh_token_invalid", repo.accountsByID[81].Extra[FormalPoolExtraLastFailureCode])
	require.Equal(t, "refresh_token_invalid", repo.accountsByID[81].Extra[FormalPoolExtraQuarantineReason])
}

func TestRateLimitService_FormalPoolAnthropic403Quarantines(t *testing.T) {
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{78: {
		ID:          78,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageWarming},
	}}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := repo.accountsByID[78]

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"account on hold"}}`))

	require.True(t, shouldDisable)
	require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[78].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accountsByID[78].Schedulable)
	require.Equal(t, StatusError, repo.accountsByID[78].Status)
}

func TestRateLimitService_NonFormalSetupToken401KeepsLegacySetError(t *testing.T) {
	repo := &formalRateLimitRepo{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 79, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Status: StatusActive}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusUnauthorized, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
}

func TestRateLimitServiceSanitizesForbiddenAndTempUnschedMessages(t *testing.T) {
	body := []byte(`{"error":{"message":"token raw-token-marker prompt raw prompt marker email user@example.com uuid 99999999-8888-4777-8666-555555555555 cch=12345"}}`)
	msg := buildForbiddenErrorMessage("Forbidden:", "", body, "fallback")
	temp := truncateTempUnschedMessage(body, 1024)
	for _, out := range []string{msg, temp} {
		if strings.Contains(out, "raw-token-marker") || strings.Contains(out, "raw prompt marker") || strings.Contains(out, "user@example.com") || strings.Contains(out, "99999999-8888-4777-8666-555555555555") || strings.Contains(out, "cch=12345") {
			t.Fatalf("sensitive upstream body leaked: %s", out)
		}
	}
}

func TestRateLimitService_FormalPool403QuarantinePreemptsTempUnschedulable(t *testing.T) {
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{80: {
		ID:          80,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			FormalPoolExtraOnboardingStage: FormalPoolStageProduction,
			"temp_unschedulable_rules":     []any{map[string]any{"status": float64(403), "message_contains": "account"}},
		},
	}}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := repo.accountsByID[80]

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"account on hold"}}`))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.tempCalls, "formal-pool 403 must not be downgraded to temporary unschedulable")
	require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[80].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accountsByID[80].Schedulable)
}

func TestRateLimitService_FormalPoolHardQuarantineReasonBuckets(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantBucket string
	}{
		{name: "kyc", status: http.StatusForbidden, body: `{"error":{"message":"KYC verification required"}}`, wantBucket: "reason_risk_text"},
		{name: "risk", status: http.StatusForbidden, body: `{"error":{"message":"risk text detected"}}`, wantBucket: "reason_risk_text"},
		{name: "proxy mismatch", status: http.StatusForbidden, body: `{"error":{"message":"proxy_mismatch"}}`, wantBucket: "reason_proxy"},
		{name: "fallback", status: http.StatusForbidden, body: `{"error":{"message":"fallback detected"}}`, wantBucket: "reason_fallback"},
		{name: "verifier", status: http.StatusForbidden, body: `{"error":{"message":"verifier failed"}}`, wantBucket: "reason_verifier"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{90: {
				ID:          90,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"refresh_token": "refresh-token"},
				Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction},
			}}}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := repo.accountsByID[90]

			shouldDisable := service.HandleUpstreamError(context.Background(), account, tc.status, http.Header{}, []byte(tc.body))

			require.True(t, shouldDisable)
			require.Equal(t, 0, repo.tempCalls)
			require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[90].Extra[FormalPoolExtraOnboardingStage])
			require.False(t, repo.accountsByID[90].Schedulable)
			require.Equal(t, tc.wantBucket, repo.accountsByID[90].Extra[FormalPoolExtraQuarantineReason])
		})
	}
}
