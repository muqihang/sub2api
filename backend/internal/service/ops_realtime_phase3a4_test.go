package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type opsStatsAccountRepoStub struct {
	AccountRepository
	opsCalled       bool
	opsPlatform     string
	opsGroupID      *int64
	listFilterCalls int
	accounts        []Account
}

func (r *opsStatsAccountRepoStub) ListOpsAccountsForStats(_ context.Context, platformFilter string, groupIDFilter *int64) ([]Account, error) {
	r.opsCalled = true
	r.opsPlatform = platformFilter
	if groupIDFilter != nil {
		gid := *groupIDFilter
		r.opsGroupID = &gid
	}
	return append([]Account(nil), r.accounts...), nil
}

func (r *opsStatsAccountRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	r.listFilterCalls++
	return nil, nil, nil
}

type opsFallbackAccountRepoStub struct {
	AccountRepository
	seenGroupIDs []int64
}

func (r *opsFallbackAccountRepoStub) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	r.seenGroupIDs = append(r.seenGroupIDs, groupID)
	return []Account{}, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Total: 0, Pages: 0}, nil
}

func TestOpsListAllAccountsForOpsUsesOptimizedRepository(t *testing.T) {
	groupID := int64(42)
	repo := &opsStatsAccountRepoStub{accounts: []Account{{ID: 1, Platform: PlatformOpenAI}}}
	svc := &OpsService{accountRepo: repo}

	accounts, err := svc.listAllAccountsForOps(context.Background(), PlatformOpenAI, &groupID)

	require.NoError(t, err)
	require.Equal(t, []Account{{ID: 1, Platform: PlatformOpenAI}}, accounts)
	require.True(t, repo.opsCalled)
	require.Equal(t, PlatformOpenAI, repo.opsPlatform)
	require.NotNil(t, repo.opsGroupID)
	require.Equal(t, groupID, *repo.opsGroupID)
	require.Equal(t, 0, repo.listFilterCalls)
}

func TestOpsListAllAccountsForOpsFallbackPassesGroupFilter(t *testing.T) {
	groupID := int64(7)
	repo := &opsFallbackAccountRepoStub{}
	svc := &OpsService{accountRepo: repo}

	accounts, err := svc.listAllAccountsForOps(context.Background(), PlatformOpenAI, &groupID)

	require.NoError(t, err)
	require.Empty(t, accounts)
	require.Equal(t, []int64{groupID}, repo.seenGroupIDs)
}
