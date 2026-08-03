package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type apiKeyConcurrencyRepoStub struct {
	keys []APIKey
	key  *APIKey
}

func (s *apiKeyConcurrencyRepoStub) Create(context.Context, *APIKey) error {
	panic("unexpected Create")
}
func (s *apiKeyConcurrencyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	if s.key == nil {
		return nil, ErrAPIKeyNotFound
	}
	clone := *s.key
	return &clone, nil
}
func (s *apiKeyConcurrencyRepoStub) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID")
}
func (s *apiKeyConcurrencyRepoStub) GetByKey(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKey")
}
func (s *apiKeyConcurrencyRepoStub) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth")
}
func (s *apiKeyConcurrencyRepoStub) Update(context.Context, *APIKey) error {
	panic("unexpected Update")
}
func (s *apiKeyConcurrencyRepoStub) Delete(context.Context, int64) error { panic("unexpected Delete") }
func (s *apiKeyConcurrencyRepoStub) DeleteWithAudit(context.Context, int64) error {
	panic("unexpected DeleteWithAudit")
}
func (s *apiKeyConcurrencyRepoStub) ListByUserID(_ context.Context, _ int64, params pagination.PaginationParams, _ APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	keys := append([]APIKey(nil), s.keys...)
	return keys, &pagination.PaginationResult{Total: int64(len(keys)), Page: params.Page, PageSize: params.PageSize, Pages: 1}, nil
}
func (s *apiKeyConcurrencyRepoStub) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership")
}
func (s *apiKeyConcurrencyRepoStub) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected CountByUserID")
}
func (s *apiKeyConcurrencyRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	panic("unexpected ExistsByKey")
}
func (s *apiKeyConcurrencyRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID")
}
func (s *apiKeyConcurrencyRepoStub) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys")
}
func (s *apiKeyConcurrencyRepoStub) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID")
}
func (s *apiKeyConcurrencyRepoStub) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup")
}
func (s *apiKeyConcurrencyRepoStub) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected CountByGroupID")
}
func (s *apiKeyConcurrencyRepoStub) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByUserID")
}
func (s *apiKeyConcurrencyRepoStub) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID")
}
func (s *apiKeyConcurrencyRepoStub) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed")
}
func (s *apiKeyConcurrencyRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed")
}
func (s *apiKeyConcurrencyRepoStub) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementRateLimitUsage")
}
func (s *apiKeyConcurrencyRepoStub) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected ResetRateLimitWindows")
}
func (s *apiKeyConcurrencyRepoStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData")
}

type apiKeyConcurrencyStatsCache struct {
	counts map[int64]int
	err    error
}

func (s *apiKeyConcurrencyStatsCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return false, nil
}
func (s *apiKeyConcurrencyStatsCache) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}
func (s *apiKeyConcurrencyStatsCache) GetAccountConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}
func (s *apiKeyConcurrencyStatsCache) GetAccountConcurrencyBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		out[id] = 0
	}
	return out, nil
}
func (s *apiKeyConcurrencyStatsCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (s *apiKeyConcurrencyStatsCache) DecrementAccountWaitCount(context.Context, int64) error {
	return nil
}
func (s *apiKeyConcurrencyStatsCache) GetAccountWaitingCount(context.Context, int64) (int, error) {
	return 0, nil
}
func (s *apiKeyConcurrencyStatsCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return false, nil
}
func (s *apiKeyConcurrencyStatsCache) ReleaseUserSlot(context.Context, int64, string) error {
	return nil
}
func (s *apiKeyConcurrencyStatsCache) GetUserConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}
func (s *apiKeyConcurrencyStatsCache) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (s *apiKeyConcurrencyStatsCache) DecrementWaitCount(context.Context, int64) error { return nil }
func (s *apiKeyConcurrencyStatsCache) GetAccountsLoadBatch(context.Context, []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return map[int64]*AccountLoadInfo{}, nil
}
func (s *apiKeyConcurrencyStatsCache) GetUsersLoadBatch(context.Context, []UserWithConcurrency) (map[int64]*UserLoadInfo, error) {
	return map[int64]*UserLoadInfo{}, nil
}
func (s *apiKeyConcurrencyStatsCache) CleanupExpiredAccountSlots(context.Context, int64) error {
	return nil
}
func (s *apiKeyConcurrencyStatsCache) CleanupExpiredAccountSlotKeys(context.Context) error {
	return nil
}
func (s *apiKeyConcurrencyStatsCache) CleanupStaleProcessSlots(context.Context, string) error {
	return nil
}

func (s *apiKeyConcurrencyStatsCache) TrackAPIKeySlot(context.Context, int64, string) error {
	return nil
}
func (s *apiKeyConcurrencyStatsCache) ReleaseAPIKeySlot(context.Context, int64, string) error {
	return nil
}
func (s *apiKeyConcurrencyStatsCache) GetAPIKeyConcurrencyBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		out[id] = s.counts[id]
	}
	return out, nil
}

func TestAPIKeyService_ListFillsCurrentConcurrency(t *testing.T) {
	repo := &apiKeyConcurrencyRepoStub{keys: []APIKey{{ID: 101, UserID: 7, Key: "sk-one"}, {ID: 102, UserID: 7, Key: "sk-two"}}}
	cache := &apiKeyConcurrencyStatsCache{counts: map[int64]int{101: 2, 102: 5}}
	svc := &APIKeyService{apiKeyRepo: repo}
	svc.SetConcurrencyService(NewConcurrencyService(cache))

	keys, page, err := svc.List(context.Background(), 7, pagination.PaginationParams{Page: 1, PageSize: 20}, APIKeyListFilters{})
	require.NoError(t, err)
	require.NotNil(t, page)
	require.Len(t, keys, 2)
	require.Equal(t, 2, keys[0].CurrentConcurrency)
	require.Equal(t, 5, keys[1].CurrentConcurrency)
}

func TestAPIKeyService_GetByIDFillsCurrentConcurrency(t *testing.T) {
	repo := &apiKeyConcurrencyRepoStub{key: &APIKey{ID: 201, UserID: 8, Key: "sk-single"}}
	cache := &apiKeyConcurrencyStatsCache{counts: map[int64]int{201: 3}}
	svc := &APIKeyService{apiKeyRepo: repo}
	svc.SetConcurrencyService(NewConcurrencyService(cache))

	key, err := svc.GetByID(context.Background(), 201)
	require.NoError(t, err)
	require.Equal(t, 3, key.CurrentConcurrency)
}

func TestAPIKeyService_CurrentConcurrencyFallsBackToZeroOnCacheError(t *testing.T) {
	repo := &apiKeyConcurrencyRepoStub{keys: []APIKey{{ID: 301, UserID: 9, Key: "sk-error"}}}
	cache := &apiKeyConcurrencyStatsCache{err: errors.New("redis unavailable")}
	svc := &APIKeyService{apiKeyRepo: repo}
	svc.SetConcurrencyService(NewConcurrencyService(cache))

	keys, _, err := svc.List(context.Background(), 9, pagination.PaginationParams{Page: 1, PageSize: 20}, APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, 0, keys[0].CurrentConcurrency)
}
