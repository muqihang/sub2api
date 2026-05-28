package service

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type apiKeyAugmentRepoStub struct {
	created *APIKey
	current *APIKey
	updated *APIKey
}

func (s *apiKeyAugmentRepoStub) Create(ctx context.Context, key *APIKey) error {
	cloned := *key
	s.created = &cloned
	return nil
}

func (s *apiKeyAugmentRepoStub) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	if s.current == nil {
		return nil, ErrAPIKeyNotFound
	}
	cloned := *s.current
	return &cloned, nil
}

func (s *apiKeyAugmentRepoStub) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID call")
}

func (s *apiKeyAugmentRepoStub) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	panic("unexpected GetByKey call")
}

func (s *apiKeyAugmentRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth call")
}

func (s *apiKeyAugmentRepoStub) Update(ctx context.Context, key *APIKey) error {
	cloned := *key
	s.updated = &cloned
	return nil
}

func (s *apiKeyAugmentRepoStub) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}
func (s *apiKeyAugmentRepoStub) ListByUserID(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserID call")
}
func (s *apiKeyAugmentRepoStub) VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}
func (s *apiKeyAugmentRepoStub) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	panic("unexpected CountByUserID call")
}
func (s *apiKeyAugmentRepoStub) ExistsByKey(ctx context.Context, key string) (bool, error) {
	return false, nil
}
func (s *apiKeyAugmentRepoStub) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (s *apiKeyAugmentRepoStub) SearchAPIKeys(ctx context.Context, userID int64, keyword string, limit int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}
func (s *apiKeyAugmentRepoStub) ClearGroupIDByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}
func (s *apiKeyAugmentRepoStub) UpdateGroupIDByUserAndGroup(ctx context.Context, userID, oldGroupID, newGroupID int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}
func (s *apiKeyAugmentRepoStub) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}
func (s *apiKeyAugmentRepoStub) ListKeysByUserID(ctx context.Context, userID int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}
func (s *apiKeyAugmentRepoStub) ListKeysByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}
func (s *apiKeyAugmentRepoStub) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}
func (s *apiKeyAugmentRepoStub) UpdateLastUsed(ctx context.Context, id int64, usedAt time.Time) error {
	panic("unexpected UpdateLastUsed call")
}
func (s *apiKeyAugmentRepoStub) IncrementRateLimitUsage(ctx context.Context, id int64, cost float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}
func (s *apiKeyAugmentRepoStub) ResetRateLimitWindows(ctx context.Context, id int64) error {
	panic("unexpected ResetRateLimitWindows call")
}
func (s *apiKeyAugmentRepoStub) GetRateLimitData(ctx context.Context, id int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

type apiKeyAugmentUserRepoStub struct {
	user *User
}

func (s *apiKeyAugmentUserRepoStub) GetByID(ctx context.Context, id int64) (*User, error) {
	if s.user == nil || s.user.ID != id {
		return nil, ErrUserNotFound
	}
	return s.user, nil
}

func (s *apiKeyAugmentUserRepoStub) Create(ctx context.Context, user *User) error {
	panic("unexpected Create call")
}
func (s *apiKeyAugmentUserRepoStub) GetByEmail(ctx context.Context, email string) (*User, error) {
	panic("unexpected GetByEmail call")
}
func (s *apiKeyAugmentUserRepoStub) GetFirstAdmin(ctx context.Context) (*User, error) {
	panic("unexpected GetFirstAdmin call")
}
func (s *apiKeyAugmentUserRepoStub) Update(ctx context.Context, user *User) error {
	panic("unexpected Update call")
}
func (s *apiKeyAugmentUserRepoStub) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}
func (s *apiKeyAugmentUserRepoStub) GetUserAvatar(ctx context.Context, userID int64) (*UserAvatar, error) {
	panic("unexpected GetUserAvatar call")
}
func (s *apiKeyAugmentUserRepoStub) UpsertUserAvatar(ctx context.Context, userID int64, input UpsertUserAvatarInput) (*UserAvatar, error) {
	panic("unexpected UpsertUserAvatar call")
}
func (s *apiKeyAugmentUserRepoStub) DeleteUserAvatar(ctx context.Context, userID int64) error {
	panic("unexpected DeleteUserAvatar call")
}
func (s *apiKeyAugmentUserRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *apiKeyAugmentUserRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters UserListFilters) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (s *apiKeyAugmentUserRepoStub) GetLatestUsedAtByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserIDs call")
}
func (s *apiKeyAugmentUserRepoStub) GetLatestUsedAtByUserID(ctx context.Context, userID int64) (*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserID call")
}
func (s *apiKeyAugmentUserRepoStub) UpdateUserLastActiveAt(ctx context.Context, userID int64, activeAt time.Time) error {
	panic("unexpected UpdateUserLastActiveAt call")
}
func (s *apiKeyAugmentUserRepoStub) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	panic("unexpected UpdateBalance call")
}
func (s *apiKeyAugmentUserRepoStub) DeductBalance(ctx context.Context, id int64, amount float64) error {
	panic("unexpected DeductBalance call")
}
func (s *apiKeyAugmentUserRepoStub) UpdateConcurrency(ctx context.Context, id int64, amount int) error {
	panic("unexpected UpdateConcurrency call")
}
func (s *apiKeyAugmentUserRepoStub) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	panic("unexpected ExistsByEmail call")
}
func (s *apiKeyAugmentUserRepoStub) RemoveGroupFromAllowedGroups(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected RemoveGroupFromAllowedGroups call")
}
func (s *apiKeyAugmentUserRepoStub) AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	panic("unexpected AddGroupToAllowedGroups call")
}
func (s *apiKeyAugmentUserRepoStub) RemoveGroupFromUserAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	panic("unexpected RemoveGroupFromUserAllowedGroups call")
}
func (s *apiKeyAugmentUserRepoStub) ListUserAuthIdentities(ctx context.Context, userID int64) ([]UserAuthIdentityRecord, error) {
	panic("unexpected ListUserAuthIdentities call")
}
func (s *apiKeyAugmentUserRepoStub) UnbindUserAuthProvider(ctx context.Context, userID int64, provider string) error {
	panic("unexpected UnbindUserAuthProvider call")
}
func (s *apiKeyAugmentUserRepoStub) UpdateTotpSecret(ctx context.Context, userID int64, encryptedSecret *string) error {
	panic("unexpected UpdateTotpSecret call")
}
func (s *apiKeyAugmentUserRepoStub) EnableTotp(ctx context.Context, userID int64) error {
	panic("unexpected EnableTotp call")
}
func (s *apiKeyAugmentUserRepoStub) DisableTotp(ctx context.Context, userID int64) error {
	panic("unexpected DisableTotp call")
}

type apiKeyAugmentGroupRepoStub struct {
	groups map[int64]*Group
}

func (s *apiKeyAugmentGroupRepoStub) GetByID(ctx context.Context, id int64) (*Group, error) {
	group, ok := s.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	cloned := *group
	return &cloned, nil
}

func (s *apiKeyAugmentGroupRepoStub) Create(ctx context.Context, group *Group) error {
	panic("unexpected Create call")
}
func (s *apiKeyAugmentGroupRepoStub) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	panic("unexpected GetByIDLite call")
}
func (s *apiKeyAugmentGroupRepoStub) Update(ctx context.Context, group *Group) error {
	panic("unexpected Update call")
}
func (s *apiKeyAugmentGroupRepoStub) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}
func (s *apiKeyAugmentGroupRepoStub) DeleteCascade(ctx context.Context, id int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}
func (s *apiKeyAugmentGroupRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *apiKeyAugmentGroupRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (s *apiKeyAugmentGroupRepoStub) ListActive(ctx context.Context) ([]Group, error) {
	out := make([]Group, 0, len(s.groups))
	for _, group := range s.groups {
		if group == nil || group.Status != StatusActive {
			continue
		}
		cloned := *group
		out = append(out, cloned)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}
func (s *apiKeyAugmentGroupRepoStub) ListActiveByPlatform(ctx context.Context, platform string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}
func (s *apiKeyAugmentGroupRepoStub) ExistsByName(ctx context.Context, name string) (bool, error) {
	panic("unexpected ExistsByName call")
}
func (s *apiKeyAugmentGroupRepoStub) GetAccountCount(ctx context.Context, groupID int64) (int64, int64, error) {
	panic("unexpected GetAccountCount call")
}
func (s *apiKeyAugmentGroupRepoStub) DeleteAccountGroupsByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID call")
}
func (s *apiKeyAugmentGroupRepoStub) GetAccountIDsByGroupIDs(ctx context.Context, groupIDs []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs call")
}
func (s *apiKeyAugmentGroupRepoStub) BindAccountsToGroup(ctx context.Context, groupID int64, accountIDs []int64) error {
	panic("unexpected BindAccountsToGroup call")
}
func (s *apiKeyAugmentGroupRepoStub) UpdateSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders call")
}

type apiKeyAugmentUserSubRepoStub struct{}

func (apiKeyAugmentUserSubRepoStub) Create(ctx context.Context, sub *UserSubscription) error {
	panic("unexpected Create call")
}
func (apiKeyAugmentUserSubRepoStub) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	panic("unexpected GetByID call")
}
func (apiKeyAugmentUserSubRepoStub) GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	panic("unexpected GetByUserIDAndGroupID call")
}
func (apiKeyAugmentUserSubRepoStub) GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	panic("unexpected GetActiveByUserIDAndGroupID call")
}
func (apiKeyAugmentUserSubRepoStub) Update(ctx context.Context, sub *UserSubscription) error {
	panic("unexpected Update call")
}
func (apiKeyAugmentUserSubRepoStub) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}
func (apiKeyAugmentUserSubRepoStub) ListByUserID(ctx context.Context, userID int64) ([]UserSubscription, error) {
	panic("unexpected ListByUserID call")
}
func (apiKeyAugmentUserSubRepoStub) ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error) {
	return []UserSubscription{}, nil
}
func (apiKeyAugmentUserSubRepoStub) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (apiKeyAugmentUserSubRepoStub) List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (apiKeyAugmentUserSubRepoStub) ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	panic("unexpected ExistsByUserIDAndGroupID call")
}
func (apiKeyAugmentUserSubRepoStub) ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error {
	panic("unexpected ExtendExpiry call")
}
func (apiKeyAugmentUserSubRepoStub) UpdateStatus(ctx context.Context, subscriptionID int64, status string) error {
	panic("unexpected UpdateStatus call")
}
func (apiKeyAugmentUserSubRepoStub) UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error {
	panic("unexpected UpdateNotes call")
}
func (apiKeyAugmentUserSubRepoStub) ActivateWindows(ctx context.Context, id int64, start time.Time) error {
	panic("unexpected ActivateWindows call")
}
func (apiKeyAugmentUserSubRepoStub) ResetDailyUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	panic("unexpected ResetDailyUsage call")
}
func (apiKeyAugmentUserSubRepoStub) ResetWeeklyUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	panic("unexpected ResetWeeklyUsage call")
}
func (apiKeyAugmentUserSubRepoStub) ResetMonthlyUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	panic("unexpected ResetMonthlyUsage call")
}
func (apiKeyAugmentUserSubRepoStub) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	panic("unexpected IncrementUsage call")
}
func (apiKeyAugmentUserSubRepoStub) BatchUpdateExpiredStatus(ctx context.Context) (int64, error) {
	panic("unexpected BatchUpdateExpiredStatus call")
}

type apiKeyAugmentUserGroupRateRepoStub struct{}

func (apiKeyAugmentUserGroupRateRepoStub) GetByUserID(ctx context.Context, userID int64) (map[int64]float64, error) {
	panic("unexpected GetByUserID call")
}
func (apiKeyAugmentUserGroupRateRepoStub) GetByUserAndGroup(ctx context.Context, userID, groupID int64) (*float64, error) {
	panic("unexpected GetByUserAndGroup call")
}
func (apiKeyAugmentUserGroupRateRepoStub) GetRPMOverrideByUserAndGroup(ctx context.Context, userID, groupID int64) (*int, error) {
	panic("unexpected GetRPMOverrideByUserAndGroup call")
}
func (apiKeyAugmentUserGroupRateRepoStub) GetByGroupID(ctx context.Context, groupID int64) ([]UserGroupRateEntry, error) {
	panic("unexpected GetByGroupID call")
}
func (apiKeyAugmentUserGroupRateRepoStub) SyncUserGroupRates(ctx context.Context, userID int64, rates map[int64]*float64) error {
	panic("unexpected SyncUserGroupRates call")
}
func (apiKeyAugmentUserGroupRateRepoStub) SyncGroupRateMultipliers(ctx context.Context, groupID int64, entries []GroupRateMultiplierInput) error {
	panic("unexpected SyncGroupRateMultipliers call")
}
func (apiKeyAugmentUserGroupRateRepoStub) SyncGroupRPMOverrides(ctx context.Context, groupID int64, entries []GroupRPMOverrideInput) error {
	panic("unexpected SyncGroupRPMOverrides call")
}
func (apiKeyAugmentUserGroupRateRepoStub) ClearGroupRPMOverrides(ctx context.Context, groupID int64) error {
	panic("unexpected ClearGroupRPMOverrides call")
}
func (apiKeyAugmentUserGroupRateRepoStub) DeleteByGroupID(ctx context.Context, groupID int64) error {
	panic("unexpected DeleteByGroupID call")
}
func (apiKeyAugmentUserGroupRateRepoStub) DeleteByUserID(ctx context.Context, userID int64) error {
	panic("unexpected DeleteByUserID call")
}

func newAPIKeyAugmentService(user *User, groups ...*Group) (*APIKeyService, *apiKeyAugmentRepoStub) {
	groupMap := make(map[int64]*Group, len(groups))
	for _, group := range groups {
		groupMap[group.ID] = group
	}
	repo := &apiKeyAugmentRepoStub{}
	svc := NewAPIKeyService(
		repo,
		&apiKeyAugmentUserRepoStub{user: user},
		&apiKeyAugmentGroupRepoStub{groups: groupMap},
		apiKeyAugmentUserSubRepoStub{},
		apiKeyAugmentUserGroupRateRepoStub{},
		nil,
		&config.Config{Default: config.DefaultConfig{APIKeyPrefix: "sk-"}},
	)
	return svc, repo
}

func TestAPIKeyServiceCreateRejectsAugmentOnlyWithoutGroup(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	svc, _ := newAPIKeyAugmentService(user)

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:        "augment",
		AugmentOnly: true,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrAugmentGroupRequired)
}

func TestAPIKeyServiceCreateRejectsAugmentOnlyWithoutGroupEvenWhenDefaultEntitledGroupExists(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(24)
	svc, _ := newAPIKeyAugmentService(user, &Group{
		ID:                     groupID,
		Name:                   "augment",
		Status:                 StatusActive,
		Hydrated:               true,
		Platform:               PlatformOpenAI,
		AugmentGatewayEntitled: true,
	})

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:        "augment",
		AugmentOnly: true,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrAugmentGroupRequired)
}

func TestAPIKeyServiceCreateRejectsAugmentOnlyForNonEntitledGroup(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(11)
	svc, _ := newAPIKeyAugmentService(user, &Group{
		ID:       groupID,
		Name:     "generic",
		Status:   StatusActive,
		Hydrated: true,
		Platform: PlatformOpenAI,
	})

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:        "augment",
		GroupID:     &groupID,
		AugmentOnly: true,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrAugmentGroupNotEntitled)
}

func TestAPIKeyServiceCreateSetsRestrictedClientProductForAugmentOnlyKey(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(12)
	svc, repo := newAPIKeyAugmentService(user, &Group{
		ID:                     groupID,
		Name:                   "augment",
		Status:                 StatusActive,
		Hydrated:               true,
		Platform:               PlatformOpenAI,
		AugmentGatewayEntitled: true,
	})

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:        "augment",
		GroupID:     &groupID,
		AugmentOnly: true,
	})
	require.NoError(t, err)
	require.NotNil(t, key.RestrictedClientProduct)
	require.Equal(t, AugmentClientProductZhumeng, *key.RestrictedClientProduct)
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.created.RestrictedClientProduct)
	require.Equal(t, AugmentClientProductZhumeng, *repo.created.RestrictedClientProduct)
}

func TestAPIKeyServiceGetAvailableGroupsHidesInternalAugmentRoutingGroups(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	repo := &apiKeyAugmentRepoStub{}
	groupRepo := &apiKeyAugmentGroupRepoStub{
		groups: map[int64]*Group{
			11: {
				ID:       11,
				Name:     "augment-openai-routing",
				Status:   StatusActive,
				Hydrated: true,
				Platform: PlatformOpenAI,
			},
			12: {
				ID:                     12,
				Name:                   "Augment Local",
				Status:                 StatusActive,
				Hydrated:               true,
				Platform:               PlatformOpenAI,
				AugmentGatewayEntitled: true,
			},
			13: {
				ID:       13,
				Name:     "OpenAI Public",
				Status:   StatusActive,
				Hydrated: true,
				Platform: PlatformOpenAI,
			},
		},
	}
	svc := NewAPIKeyService(
		repo,
		&apiKeyAugmentUserRepoStub{user: user},
		groupRepo,
		apiKeyAugmentUserSubRepoStub{},
		apiKeyAugmentUserGroupRateRepoStub{},
		nil,
		&config.Config{Default: config.DefaultConfig{APIKeyPrefix: "sk-"}},
	)

	groups, err := svc.GetAvailableGroups(context.Background(), user.ID)
	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.Equal(t, []string{"Augment Local", "OpenAI Public"}, []string{groups[0].Name, groups[1].Name})
}

func TestAPIKeyServiceUpdateRevalidatesEntitlementWhenTogglingAugmentOnlyOn(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(13)
	svc, repo := newAPIKeyAugmentService(user, &Group{
		ID:       groupID,
		Name:     "generic",
		Status:   StatusActive,
		Hydrated: true,
		Platform: PlatformOpenAI,
	})
	repo.current = &APIKey{
		ID:        99,
		UserID:    user.ID,
		Key:       "sk-generic",
		Name:      "generic",
		GroupID:   &groupID,
		Status:    StatusActive,
		CreatedAt: time.Now(),
	}
	augmentOnly := true

	key, err := svc.Update(context.Background(), 99, user.ID, UpdateAPIKeyRequest{
		AugmentOnly: &augmentOnly,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrAugmentGroupNotEntitled)
}

func TestAPIKeyServiceUpdateClearsRestrictedClientProductWhenAugmentOnlyDisabled(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(14)
	product := AugmentClientProductZhumeng
	svc, repo := newAPIKeyAugmentService(user, &Group{
		ID:                     groupID,
		Name:                   "augment",
		Status:                 StatusActive,
		Hydrated:               true,
		Platform:               PlatformOpenAI,
		AugmentGatewayEntitled: true,
	})
	repo.current = &APIKey{
		ID:                      100,
		UserID:                  user.ID,
		Key:                     "sk-augment",
		Name:                    "augment",
		GroupID:                 &groupID,
		Status:                  StatusActive,
		RestrictedClientProduct: &product,
		CreatedAt:               time.Now(),
	}
	augmentOnly := false

	key, err := svc.Update(context.Background(), 100, user.ID, UpdateAPIKeyRequest{
		AugmentOnly: &augmentOnly,
	})
	require.NoError(t, err)
	require.Nil(t, key.RestrictedClientProduct)
	require.NotNil(t, repo.updated)
	require.Nil(t, repo.updated.RestrictedClientProduct)
}

func TestAPIKeyServiceCreateRejectsScopedFlagConflict(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(15)
	svc, _ := newAPIKeyAugmentService(user, &Group{
		ID:                     groupID,
		Name:                   "entitled-both",
		Status:                 StatusActive,
		Hydrated:               true,
		Platform:               PlatformOpenAI,
		AugmentGatewayEntitled: true,
		CodexGatewayEntitled:   true,
	})

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:        "conflict",
		GroupID:     &groupID,
		AugmentOnly: true,
		CodexOnly:   true,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrAPIKeyClientProductConflict)
}

func TestAPIKeyServiceCreateRejectsCodexOnlyWithoutGroup(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	svc, _ := newAPIKeyAugmentService(user)

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:      "codex",
		CodexOnly: true,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrCodexGroupRequired)
}

func TestAPIKeyServiceCreateAssignsDefaultCodexGroupWhenGroupOmitted(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	nonEntitledGroupID := int64(20)
	entitledGroupID := int64(21)
	svc, repo := newAPIKeyAugmentService(user,
		&Group{
			ID:       nonEntitledGroupID,
			Name:     "generic",
			Status:   StatusActive,
			Hydrated: true,
			Platform: PlatformOpenAI,
		},
		&Group{
			ID:                   entitledGroupID,
			Name:                 "codex",
			Status:               StatusActive,
			Hydrated:             true,
			Platform:             PlatformOpenAI,
			CodexGatewayEntitled: true,
		},
	)

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:      "codex",
		CodexOnly: true,
	})
	require.NoError(t, err)
	require.True(t, key.IsCodexOnly())
	require.NotNil(t, key.GroupID)
	require.Equal(t, entitledGroupID, *key.GroupID)
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.created.GroupID)
	require.Equal(t, entitledGroupID, *repo.created.GroupID)
}

func TestAPIKeyServiceCreateAssignsDefaultCodexExclusiveGroupOnlyWhenAllowed(t *testing.T) {
	t.Parallel()

	disallowedGroupID := int64(22)
	allowedGroupID := int64(23)
	user := &User{ID: 1, Status: StatusActive, AllowedGroups: []int64{allowedGroupID}}
	svc, repo := newAPIKeyAugmentService(user,
		&Group{
			ID:                   disallowedGroupID,
			Name:                 "codex-exclusive-disallowed",
			Status:               StatusActive,
			Hydrated:             true,
			Platform:             PlatformOpenAI,
			IsExclusive:          true,
			CodexGatewayEntitled: true,
		},
		&Group{
			ID:                   allowedGroupID,
			Name:                 "codex-exclusive-allowed",
			Status:               StatusActive,
			Hydrated:             true,
			Platform:             PlatformOpenAI,
			IsExclusive:          true,
			CodexGatewayEntitled: true,
		},
	)

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:      "codex",
		CodexOnly: true,
	})
	require.NoError(t, err)
	require.True(t, key.IsCodexOnly())
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.created.GroupID)
	require.Equal(t, allowedGroupID, *repo.created.GroupID)
}

func TestAPIKeyServiceCreateRejectsCodexOnlyForNonEntitledGroup(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(16)
	svc, _ := newAPIKeyAugmentService(user, &Group{
		ID:       groupID,
		Name:     "generic",
		Status:   StatusActive,
		Hydrated: true,
		Platform: PlatformOpenAI,
	})

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:      "codex",
		GroupID:   &groupID,
		CodexOnly: true,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrCodexGroupNotEntitled)
}

func TestAPIKeyServiceCreateSetsRestrictedClientProductForCodexOnlyKey(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(17)
	svc, repo := newAPIKeyAugmentService(user, &Group{
		ID:                   groupID,
		Name:                 "codex",
		Status:               StatusActive,
		Hydrated:             true,
		Platform:             PlatformOpenAI,
		CodexGatewayEntitled: true,
	})

	key, err := svc.Create(context.Background(), user.ID, CreateAPIKeyRequest{
		Name:      "codex",
		GroupID:   &groupID,
		CodexOnly: true,
	})
	require.NoError(t, err)
	require.True(t, key.IsCodexOnly())
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.created.RestrictedClientProduct)
	require.Equal(t, CodexUsageClientProduct, *repo.created.RestrictedClientProduct)
}

func TestAPIKeyServiceUpdateRevalidatesEntitlementWhenTogglingCodexOnlyOn(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(18)
	svc, repo := newAPIKeyAugmentService(user, &Group{
		ID:       groupID,
		Name:     "generic",
		Status:   StatusActive,
		Hydrated: true,
		Platform: PlatformOpenAI,
	})
	repo.current = &APIKey{
		ID:        101,
		UserID:    user.ID,
		Key:       "sk-generic",
		Name:      "generic",
		GroupID:   &groupID,
		Status:    StatusActive,
		CreatedAt: time.Now(),
	}
	codexOnly := true

	key, err := svc.Update(context.Background(), 101, user.ID, UpdateAPIKeyRequest{
		CodexOnly: &codexOnly,
	})
	require.Nil(t, key)
	require.ErrorIs(t, err, ErrCodexGroupNotEntitled)
}

func TestAPIKeyServiceUpdateClearsRestrictedClientProductWhenCodexOnlyDisabled(t *testing.T) {
	t.Parallel()

	user := &User{ID: 1, Status: StatusActive}
	groupID := int64(19)
	product := CodexUsageClientProduct
	svc, repo := newAPIKeyAugmentService(user, &Group{
		ID:                   groupID,
		Name:                 "codex",
		Status:               StatusActive,
		Hydrated:             true,
		Platform:             PlatformOpenAI,
		CodexGatewayEntitled: true,
	})
	repo.current = &APIKey{
		ID:                      102,
		UserID:                  user.ID,
		Key:                     "sk-codex",
		Name:                    "codex",
		GroupID:                 &groupID,
		Status:                  StatusActive,
		RestrictedClientProduct: &product,
		CreatedAt:               time.Now(),
	}
	codexOnly := false

	key, err := svc.Update(context.Background(), 102, user.ID, UpdateAPIKeyRequest{
		CodexOnly: &codexOnly,
	})
	require.NoError(t, err)
	require.False(t, key.IsCodexOnly())
	require.NotNil(t, repo.updated)
	require.Nil(t, repo.updated.RestrictedClientProduct)
}
