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
	lastErrorMsg   string
	lastTempReason string
}

func (r *formalRateLimitRepo) Create(context.Context, *Account) error { return nil }
func (r *formalRateLimitRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.accountsByID != nil {
		if a := r.accountsByID[id]; a != nil {
			return a, nil
		}
	}
	return nil, errors.New("account not found")
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
func (r *formalRateLimitRepo) SetRateLimited(context.Context, int64, time.Time) error { return nil }
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
func (r *formalRateLimitRepo) UpdateExtra(context.Context, int64, map[string]any) error { return nil }
func (r *formalRateLimitRepo) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	return 0, nil
}
func (r *formalRateLimitRepo) IncrementQuotaUsed(context.Context, int64, float64) error { return nil }
func (r *formalRateLimitRepo) ResetQuotaUsed(context.Context, int64) error              { return nil }

func TestRateLimitService_FormalPoolSetupToken401Quarantines(t *testing.T) {
	repo := &formalRateLimitRepo{accountsByID: map[int64]*Account{77: {
		ID:          77,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       map[string]any{FormalPoolExtraOnboardingStage: FormalPoolStageProduction},
	}}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := repo.accountsByID[77]

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusUnauthorized, http.Header{}, []byte(`{"error":{"message":"invalid credentials"}}`))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, FormalPoolStageQuarantined, repo.accountsByID[77].Extra[FormalPoolExtraOnboardingStage])
	require.False(t, repo.accountsByID[77].Schedulable)
	require.Equal(t, StatusError, repo.accountsByID[77].Status)
	require.NotEmpty(t, repo.accountsByID[77].Extra[FormalPoolExtraRiskEventRef])
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
