package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type augmentAuthServiceStub struct {
	generateTokenPairFn func(ctx context.Context, user *User, familyID string) (*TokenPair, error)
	refreshTokenPairFn  func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error)
	validateTokenFn     func(token string) (*JWTClaims, error)
}

func (s *augmentAuthServiceStub) GenerateTokenPair(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
	return s.generateTokenPairFn(ctx, user, familyID)
}

func (s *augmentAuthServiceStub) RefreshTokenPair(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
	return s.refreshTokenPairFn(ctx, refreshToken)
}

func (s *augmentAuthServiceStub) ValidateToken(token string) (*JWTClaims, error) {
	return s.validateTokenFn(token)
}

type augmentUserServiceStub struct {
	users map[int64]*User
}

func (s *augmentUserServiceStub) GetByID(ctx context.Context, id int64) (*User, error) {
	user, ok := s.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return user, nil
}

type augmentAPIKeyServiceStub struct {
	keyByValue       map[string]*APIKey
	keysByUser       map[int64][]APIKey
	availableByUser  map[int64][]Group
	listResultTotal  int64
	listResultByUser map[int64]int64
	createFn         func(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error)
}

func (s *augmentAPIKeyServiceStub) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	apiKey, ok := s.keyByValue[key]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	return apiKey, nil
}

func (s *augmentAPIKeyServiceStub) List(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	keys := append([]APIKey(nil), s.keysByUser[userID]...)
	total := s.listResultTotal
	if perUser, ok := s.listResultByUser[userID]; ok {
		total = perUser
	}
	if total == 0 {
		total = int64(len(keys))
	}
	return keys, &pagination.PaginationResult{
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    1,
	}, nil
}

func (s *augmentAPIKeyServiceStub) GetAvailableGroups(ctx context.Context, userID int64) ([]Group, error) {
	if s.availableByUser == nil {
		return []Group{testAugmentEntitledGroup(700)}, nil
	}
	return append([]Group(nil), s.availableByUser[userID]...), nil
}

func (s *augmentAPIKeyServiceStub) Create(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error) {
	if s.createFn == nil {
		return nil, errors.New("unexpected create call")
	}
	return s.createFn(ctx, userID, req)
}

type augmentSubscriptionServiceStub struct {
	activeByUser map[int64][]UserSubscription
}

func (s *augmentSubscriptionServiceStub) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), s.activeByUser[userID]...), nil
}

type augmentSettingServiceStub struct {
	public   *PublicSettings
	siteName string
}

func (s *augmentSettingServiceStub) GetPublicSettings(ctx context.Context) (*PublicSettings, error) {
	if s.public == nil {
		return nil, errors.New("missing public settings")
	}
	return s.public, nil
}

func (s *augmentSettingServiceStub) GetSiteName(ctx context.Context) string {
	if s.siteName != "" {
		return s.siteName
	}
	if s.public != nil && s.public.SiteName != "" {
		return s.public.SiteName
	}
	return "Sub2API"
}

func testAugmentEntitledGroup(id int64) Group {
	return Group{
		ID:                     id,
		Name:                   "Augment Entitled",
		Platform:               PlatformOpenAI,
		Status:                 StatusActive,
		Hydrated:               true,
		AugmentGatewayEntitled: true,
		DefaultMappedModel:     "gpt-5.4",
	}
}

func testAugmentOnlyAPIKey(id, userID int64, key string, createdAt time.Time, group Group) APIKey {
	groupID := group.ID
	product := AugmentClientProductZhumeng
	groupCopy := group
	return APIKey{
		ID:                      id,
		UserID:                  userID,
		Key:                     key,
		Name:                    key,
		GroupID:                 &groupID,
		Group:                   &groupCopy,
		Status:                  StatusActive,
		RestrictedClientProduct: &product,
		CreatedAt:               createdAt,
	}
}

func TestAugmentPluginServiceExchangeGrantSingleUse(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 4, 21, 10, 30, 0, 0, time.UTC)
	user := &User{
		ID:          42,
		Email:       "user@example.com",
		Username:    "augment-user",
		Role:        RoleUser,
		Status:      StatusActive,
		Balance:     12.5,
		Concurrency: 3,
	}

	authStub := &augmentAuthServiceStub{
		generateTokenPairFn: func(ctx context.Context, gotUser *User, familyID string) (*TokenPair, error) {
			require.Equal(t, user.ID, gotUser.ID)
			return &TokenPair{
				AccessToken:  "access-123",
				RefreshToken: "refresh-123",
				ExpiresIn:    3600,
			}, nil
		},
		refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
			return nil, errors.New("unexpected refresh call")
		},
		validateTokenFn: func(token string) (*JWTClaims, error) {
			return nil, ErrInvalidToken
		},
	}
	entitledGroup := testAugmentEntitledGroup(701)
	augmentKey := testAugmentOnlyAPIKey(7010, user.ID, "sk-augment-quicklogin-single", fixedNow, entitledGroup)

	svc := NewAugmentPluginService(
		&config.Config{
			Server: config.ServerConfig{FrontendURL: "https://fallback.local"},
		},
		authStub,
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keysByUser:       map[int64][]APIKey{user.ID: {augmentKey}},
			availableByUser: map[int64][]Group{user.ID: {entitledGroup}},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{
			public: &PublicSettings{SiteName: "Local Site"},
		},
	)
	svc.now = func() time.Time { return fixedNow }

	grant, err := svc.CreateQuickLoginGrant(context.Background(), user.ID, AugmentQuickLoginGrantOptions{
		TenantURL: "https://tenant.local",
		Mode:      AugmentQuickLoginModeLocalCompat,
	})
	require.NoError(t, err)
	require.NotEmpty(t, grant.Grant)
	require.NotEmpty(t, grant.State)
	require.Equal(t, fixedNow.Add(augmentPluginGrantTTL).Format(time.RFC3339), grant.ExpiresAt)

	bundle, err := svc.ExchangeGrant(context.Background(), grant.Grant, grant.State, "https://tenant.local")
	require.NoError(t, err)
	require.Equal(t, "access-123", bundle.AccessToken)
	require.Equal(t, "refresh-123", bundle.RefreshToken)
	require.Equal(t, fixedNow.Add(time.Hour).Format(time.RFC3339), bundle.ExpiresAt)
	require.Equal(t, "https://tenant.local", bundle.TenantURL)
	require.Equal(t, defaultAugmentPluginScopes, bundle.Scopes)
	require.Equal(t, AugmentSessionSourceLocalCompat, bundle.SessionSource)

	_, err = svc.ExchangeGrant(context.Background(), grant.Grant, grant.State, "https://tenant.local")
	require.ErrorIs(t, err, ErrAugmentPluginGrantInvalid)
}

func TestAugmentPluginServiceCreateQuickLoginGrantOfficialPassthroughRequiresExplicitSession(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:     42,
		Email:  "user@example.com",
		Role:   RoleUser,
		Status: StatusActive,
	}
	entitledGroup := testAugmentEntitledGroup(702)

	svc := NewAugmentPluginService(
		&config.Config{},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
				return nil, errors.New("unexpected generate call")
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			availableByUser: map[int64][]Group{user.ID: {entitledGroup}},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "Local Site"}},
	)

	_, err := svc.CreateQuickLoginGrant(context.Background(), user.ID, AugmentQuickLoginGrantOptions{
		TenantURL: "https://tenant.local",
		Mode:      AugmentQuickLoginModeOfficialPassthrough,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "official passthrough")
}

func TestAugmentPluginServiceCreateQuickLoginGrantDefaultsBlankModeToLocalCompat(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:     42,
		Email:  "user@example.com",
		Role:   RoleUser,
		Status: StatusActive,
	}

	authStub := &augmentAuthServiceStub{
		generateTokenPairFn: func(ctx context.Context, gotUser *User, familyID string) (*TokenPair, error) {
			require.Equal(t, user.ID, gotUser.ID)
			return &TokenPair{
				AccessToken:  "access-local",
				RefreshToken: "refresh-local",
				ExpiresIn:    3600,
			}, nil
		},
		refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
			return nil, errors.New("unexpected refresh call")
		},
		validateTokenFn: func(token string) (*JWTClaims, error) {
			return nil, ErrInvalidToken
		},
	}
	entitledGroup := testAugmentEntitledGroup(703)
	augmentKey := testAugmentOnlyAPIKey(7030, user.ID, "sk-augment-quicklogin-default", time.Date(2026, 4, 21, 10, 30, 0, 0, time.UTC), entitledGroup)

	svc := NewAugmentPluginService(
		&config.Config{},
		authStub,
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keysByUser:       map[int64][]APIKey{user.ID: {augmentKey}},
			availableByUser: map[int64][]Group{user.ID: {entitledGroup}},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "Local Site"}},
	)

	grant, err := svc.CreateQuickLoginGrant(context.Background(), user.ID, AugmentQuickLoginGrantOptions{
		TenantURL: "https://tenant.local",
	})
	require.NoError(t, err)

	bundle, err := svc.ExchangeGrant(context.Background(), grant.Grant, grant.State, "https://tenant.local")
	require.NoError(t, err)
	require.Equal(t, "access-local", bundle.AccessToken)
	require.Equal(t, "refresh-local", bundle.RefreshToken)
	require.Equal(t, AugmentSessionSourceLocalCompat, bundle.SessionSource)
}

func TestAugmentPluginServiceExchangeGrantOfficialPassthroughUsesExplicitBundle(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:     42,
		Email:  "user@example.com",
		Role:   RoleUser,
		Status: StatusActive,
	}

	authStub := &augmentAuthServiceStub{
		generateTokenPairFn: func(ctx context.Context, gotUser *User, familyID string) (*TokenPair, error) {
			return nil, errors.New("unexpected generate call")
		},
		refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
			return nil, errors.New("unexpected refresh call")
		},
		validateTokenFn: func(token string) (*JWTClaims, error) {
			return nil, ErrInvalidToken
		},
	}
	entitledGroup := testAugmentEntitledGroup(704)
	augmentKey := testAugmentOnlyAPIKey(7040, user.ID, "sk-augment-official-explicit", time.Date(2026, 4, 21, 10, 30, 0, 0, time.UTC), entitledGroup)

	svc := NewAugmentPluginService(
		&config.Config{},
		authStub,
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keysByUser:       map[int64][]APIKey{user.ID: {augmentKey}},
			availableByUser: map[int64][]Group{user.ID: {entitledGroup}},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "Local Site"}},
	)

	grant, err := svc.CreateQuickLoginGrant(context.Background(), user.ID, AugmentQuickLoginGrantOptions{
		TenantURL: "https://tenant.local",
		Mode:      AugmentQuickLoginModeOfficialPassthrough,
		OfficialSessionBundle: &AugmentSessionBundle{
			AccessToken:  "official-access",
			RefreshToken: "official-refresh",
			ExpiresAt:    "2026-04-21T12:30:00Z",
			TenantURL:    "https://official.augment.local",
			Scopes:       []string{"augment:session", "augment:summary"},
		},
	})
	require.NoError(t, err)

	bundle, err := svc.ExchangeGrant(context.Background(), grant.Grant, grant.State, "https://tenant.local")
	require.NoError(t, err)
	require.Equal(t, "official-access", bundle.AccessToken)
	require.Equal(t, "official-refresh", bundle.RefreshToken)
	require.Equal(t, "2026-04-21T12:30:00Z", bundle.ExpiresAt)
	require.Equal(t, "https://official.augment.local", bundle.TenantURL)
	require.Equal(t, []string{"augment:session", "augment:summary"}, bundle.Scopes)
	require.Equal(t, AugmentSessionSourceOfficial, bundle.SessionSource)
}

func TestAugmentPluginServiceRefreshSessionSupportsLocalAndOfficialSources(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 4, 21, 10, 30, 0, 0, time.UTC)
	user := &User{
		ID:     42,
		Email:  "user@example.com",
		Role:   RoleUser,
		Status: StatusActive,
	}

	authStub := &augmentAuthServiceStub{
		generateTokenPairFn: func(ctx context.Context, gotUser *User, familyID string) (*TokenPair, error) {
			return nil, errors.New("unexpected generate call")
		},
		refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
			require.Equal(t, "refresh-local", refreshToken)
			return &TokenPairWithUser{
				TokenPair: TokenPair{
					AccessToken:  "access-local",
					RefreshToken: "refresh-local-next",
					ExpiresIn:    1800,
				},
			}, nil
		},
		validateTokenFn: func(token string) (*JWTClaims, error) {
			return nil, ErrInvalidToken
		},
	}

	svc := NewAugmentPluginService(
		&config.Config{},
		authStub,
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "Local Site"}},
	)
	svc.now = func() time.Time { return fixedNow }

	localBundle, err := svc.RefreshSessionWithOptions(context.Background(), "refresh-local", AugmentSessionRefreshOptions{
		TenantURL: "https://tenant.local",
		Mode:      AugmentQuickLoginModeLocalCompat,
	})
	require.NoError(t, err)
	require.Equal(t, "access-local", localBundle.AccessToken)
	require.Equal(t, "refresh-local-next", localBundle.RefreshToken)
	require.Equal(t, "https://tenant.local", localBundle.TenantURL)
	require.Equal(t, AugmentSessionSourceLocalCompat, localBundle.SessionSource)

	officialBundle, err := svc.RefreshSessionWithOptions(context.Background(), "refresh-official", AugmentSessionRefreshOptions{
		Mode: AugmentQuickLoginModeOfficialPassthrough,
		OfficialSessionBundle: &AugmentSessionBundle{
			AccessToken:  "official-access-next",
			RefreshToken: "official-refresh-next",
			ExpiresAt:    "2026-04-21T13:00:00Z",
			TenantURL:    "https://official.augment.local",
			Scopes:       []string{"augment:session"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "official-access-next", officialBundle.AccessToken)
	require.Equal(t, "official-refresh-next", officialBundle.RefreshToken)
	require.Equal(t, "https://official.augment.local", officialBundle.TenantURL)
	require.Equal(t, AugmentSessionSourceOfficial, officialBundle.SessionSource)
}

func TestAugmentPluginServiceIssueLegacyLoginTokenMarksLocalCompatSource(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:     7,
		Email:  "legacy@example.com",
		Role:   RoleUser,
		Status: StatusActive,
	}

	svc := NewAugmentPluginService(
		&config.Config{},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, gotUser *User, familyID string) (*TokenPair, error) {
				require.Equal(t, user.ID, gotUser.ID)
				return &TokenPair{
					AccessToken:  "legacy-access",
					RefreshToken: "legacy-refresh",
					ExpiresIn:    3600,
				}, nil
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "Local Site"}},
	)

	token, err := svc.IssueLegacyLoginToken(context.Background(), AugmentPluginPrincipal{
		Kind: augmentPrincipalKindJWT,
		User: user,
	}, "http://127.0.0.1:18082")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:18082", token.TenantURL)
	require.Equal(t, "legacy-access", token.AccessToken)
	require.Equal(t, AugmentSessionSourceLocalCompat, token.SessionSource)
}

func TestAugmentPluginServiceResolvePrincipalFallbacksToAPIKey(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:       7,
		Email:    "apikey@example.com",
		Username: "apikey-user",
		Role:     RoleUser,
		Status:   StatusActive,
	}
	apiKey := &APIKey{
		ID:        11,
		UserID:    user.ID,
		Key:       "sk-live-123",
		Name:      "default",
		Status:    StatusActive,
		CreatedAt: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
		User:      user,
	}

	svc := NewAugmentPluginService(
		&config.Config{},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
				return nil, errors.New("unexpected generate call")
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keyByValue: map[string]*APIKey{
				apiKey.Key: apiKey,
			},
			keysByUser: map[int64][]APIKey{
				user.ID: {*apiKey},
			},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{
			public: &PublicSettings{SiteName: "Key Site"},
		},
	)

	principal, err := svc.ResolvePrincipalFromBearer(context.Background(), apiKey.Key)
	require.NoError(t, err)
	require.Equal(t, augmentPrincipalKindAPIKey, principal.Kind)
	require.Equal(t, user.ID, principal.User.ID)
	require.Equal(t, apiKey.Key, principal.APIKey.Key)
}

func TestAugmentPluginServiceResolvePrincipalRejectsInvalidAPIKeys(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:       8,
		Email:    "apikey-invalid@example.com",
		Username: "apikey-invalid",
		Role:     RoleUser,
		Status:   StatusActive,
	}
	expiredAt := time.Now().Add(-time.Hour)

	testCases := []struct {
		name   string
		apiKey *APIKey
	}{
		{
			name: "disabled",
			apiKey: &APIKey{
				ID:        31,
				UserID:    user.ID,
				Key:       "sk-disabled",
				Name:      "disabled",
				Status:    StatusAPIKeyDisabled,
				CreatedAt: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
				User:      user,
			},
		},
		{
			name: "expired",
			apiKey: &APIKey{
				ID:        32,
				UserID:    user.ID,
				Key:       "sk-expired",
				Name:      "expired",
				Status:    StatusActive,
				CreatedAt: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
				ExpiresAt: &expiredAt,
				User:      user,
			},
		},
		{
			name: "quota exhausted",
			apiKey: &APIKey{
				ID:        33,
				UserID:    user.ID,
				Key:       "sk-quota",
				Name:      "quota",
				Status:    StatusActive,
				CreatedAt: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
				Quota:     10,
				QuotaUsed: 10,
				User:      user,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewAugmentPluginService(
				&config.Config{},
				&augmentAuthServiceStub{
					validateTokenFn: func(token string) (*JWTClaims, error) {
						return nil, ErrInvalidToken
					},
				},
				&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
				&augmentAPIKeyServiceStub{
					keyByValue: map[string]*APIKey{
						tc.apiKey.Key: tc.apiKey,
					},
				},
				&augmentSubscriptionServiceStub{},
				&augmentSettingServiceStub{},
			)

			principal, err := svc.ResolvePrincipalFromBearer(context.Background(), tc.apiKey.Key)
			require.ErrorIs(t, err, ErrInvalidToken)
			require.Nil(t, principal)
		})
	}
}

func TestAugmentPluginServiceBuildSummaryAndCompatMetadata(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 4, 21, 11, 0, 0, 0, time.UTC)
	user := &User{
		ID:          9,
		Email:       "summary@example.com",
		Username:    "summary-user",
		Role:        RoleUser,
		Status:      StatusActive,
		Balance:     23.75,
		Concurrency: 5,
	}

	primaryKey := APIKey{
		ID:        21,
		UserID:    user.ID,
		Key:       "sk-primary",
		Name:      "primary",
		Status:    StatusActive,
		CreatedAt: fixedNow.Add(-2 * time.Hour),
	}
	newerKey := APIKey{
		ID:        22,
		UserID:    user.ID,
		Key:       "sk-newer",
		Name:      "secondary",
		Status:    StatusActive,
		CreatedAt: fixedNow.Add(-time.Hour),
	}

	claudeGroup := Group{
		ID:                   100,
		Name:                 "Claude",
		Platform:             PlatformAntigravity,
		Status:               StatusActive,
		Hydrated:             true,
		SupportedModelScopes: []string{"claude", "gemini_text"},
	}
	openAIGroup := Group{
		ID:                 101,
		Name:               "OpenAI",
		Platform:           PlatformOpenAI,
		Status:             StatusActive,
		Hydrated:           true,
		DefaultMappedModel: "gpt-4.1",
	}

	svc := NewAugmentPluginService(
		&config.Config{
			Server: config.ServerConfig{FrontendURL: "https://fallback.local"},
		},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
				return nil, errors.New("unexpected generate call")
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keyByValue: map[string]*APIKey{
				primaryKey.Key: &APIKey{
					ID:        primaryKey.ID,
					UserID:    primaryKey.UserID,
					Key:       primaryKey.Key,
					Name:      primaryKey.Name,
					Status:    primaryKey.Status,
					CreatedAt: primaryKey.CreatedAt,
					User:      user,
				},
			},
			keysByUser: map[int64][]APIKey{
				user.ID: {primaryKey, newerKey},
			},
			availableByUser: map[int64][]Group{
				user.ID: {claudeGroup, openAIGroup},
			},
		},
		&augmentSubscriptionServiceStub{
			activeByUser: map[int64][]UserSubscription{
				user.ID: {{
					ID:        500,
					UserID:    user.ID,
					GroupID:   claudeGroup.ID,
					Status:    SubscriptionStatusActive,
					ExpiresAt: fixedNow.Add(48 * time.Hour),
					Group: &Group{
						ID:   claudeGroup.ID,
						Name: claudeGroup.Name,
					},
				}},
			},
		},
		&augmentSettingServiceStub{
			public: &PublicSettings{
				SiteName:   "Augment Local",
				APIBaseURL: "https://api.local.test",
			},
		},
	)
	svc.now = func() time.Time { return fixedNow }

	principal := AugmentPluginPrincipal{
		Kind: augmentPrincipalKindAPIKey,
		User: user,
		APIKey: &APIKey{
			ID:        primaryKey.ID,
			UserID:    primaryKey.UserID,
			Key:       primaryKey.Key,
			Name:      primaryKey.Name,
			Status:    primaryKey.Status,
			CreatedAt: primaryKey.CreatedAt,
		},
	}

	summary, err := svc.BuildSummary(context.Background(), principal)
	require.NoError(t, err)
	require.Equal(t, "sk-primary", summary.GatewayAPIKey)
	require.Equal(t, "sk-primary", summary.PrimaryAPIKey)
	require.Equal(t, user.Email, summary.User.Email)
	require.Equal(t, 1, summary.Plan.ActiveCount)

	compat, err := svc.BuildCompatMetadata(context.Background(), principal, "https://gateway.local")
	require.NoError(t, err)
	require.Equal(t, "https://gateway.local", compat.GatewayBaseURL)
	require.Equal(t, "gpt-4.1", compat.DefaultModel)
	require.Equal(t, "Augment Local", compat.SessionDisplay.SiteName)
	require.Equal(t, user.Email, compat.SessionDisplay.UserLabel)
	require.Equal(t, StatusActive, compat.AccountState.UserStatus)
	require.Equal(t, StatusActive, compat.AccountState.APIKeyStatus)
	require.ElementsMatch(t, []string{"claude", "gemini_text"}, compat.ModelRegistry.SupportedModelScopes)
	require.Len(t, compat.ModelRegistry.Groups, 2)
}

func TestAugmentPluginServiceBuildSummaryFailsWhenEntitledUserHasNoDeterministicAugmentKey(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:          13,
		Email:       "autokey@example.com",
		Username:    "augment-autokey",
		Role:        RoleUser,
		Status:      StatusActive,
		Balance:     7.5,
		Concurrency: 2,
	}

	var createCalls int
	entitledGroup := testAugmentEntitledGroup(706)
	svc := NewAugmentPluginService(
		&config.Config{},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
				return nil, errors.New("unexpected generate call")
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keysByUser: map[int64][]APIKey{
				user.ID: {},
			},
			availableByUser: map[int64][]Group{
				user.ID: {entitledGroup},
			},
			createFn: func(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error) {
				createCalls++
				require.Equal(t, user.ID, userID)
				require.Equal(t, "Augment Plugin", req.Name)
				return &APIKey{
					ID:        88,
					UserID:    user.ID,
					Key:       "sk-plugin-generated",
					Name:      req.Name,
					Status:    StatusActive,
					CreatedAt: time.Now(),
				}, nil
			},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{
			public: &PublicSettings{SiteName: "Augment Local"},
		},
	)

	summary, err := svc.BuildSummary(context.Background(), AugmentPluginPrincipal{
		Kind: augmentPrincipalKindJWT,
		User: user,
	})
	require.ErrorIs(t, err, ErrAugmentScopedAPIKeyRequired)
	require.Equal(t, 0, createCalls)
	require.Nil(t, summary)
}

func TestAugmentPluginServiceBuildSummaryFailsWhenEntitledUserHasMultipleAugmentKeys(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:          16,
		Email:       "ambiguous@example.com",
		Username:    "ambiguous-user",
		Role:        RoleUser,
		Status:      StatusActive,
		Balance:     1.5,
		Concurrency: 1,
	}
	group := testAugmentEntitledGroup(707)
	keyOne := testAugmentOnlyAPIKey(51, user.ID, "sk-augment-one", time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC), group)
	keyTwo := testAugmentOnlyAPIKey(52, user.ID, "sk-augment-two", time.Date(2026, 4, 20, 13, 0, 0, 0, time.UTC), group)

	svc := NewAugmentPluginService(
		&config.Config{},
		&augmentAuthServiceStub{validateTokenFn: func(token string) (*JWTClaims, error) { return nil, ErrInvalidToken }},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keysByUser: map[int64][]APIKey{
				user.ID: {keyOne, keyTwo},
			},
			availableByUser: map[int64][]Group{
				user.ID: {group},
			},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "Augment Local"}},
	)

	summary, err := svc.BuildSummary(context.Background(), AugmentPluginPrincipal{
		Kind: augmentPrincipalKindJWT,
		User: user,
	})
	require.ErrorIs(t, err, ErrAugmentScopedAPIKeyAmbiguous)
	require.Nil(t, summary)
}

func TestAugmentPluginServiceBuildSummaryJWTPrincipalSkipsExpiredAndQuotaExhaustedKeys(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 4, 25, 23, 0, 0, 0, time.UTC)
	expiredAt := fixedNow.Add(-time.Hour)
	entitledGroup := testAugmentEntitledGroup(705)
	user := &User{
		ID:          15,
		Email:       "usable-key@example.com",
		Username:    "usable-key-user",
		Role:        RoleUser,
		Status:      StatusActive,
		Balance:     4.5,
		Concurrency: 1,
	}

	expiredKey := testAugmentOnlyAPIKey(40, user.ID, "sk-expired-active", fixedNow.Add(-3*time.Hour), entitledGroup)
	expiredKey.ExpiresAt = &expiredAt
	quotaExhaustedKey := testAugmentOnlyAPIKey(41, user.ID, "sk-quota-active", fixedNow.Add(-2*time.Hour), entitledGroup)
	quotaExhaustedKey.Quota = 10
	quotaExhaustedKey.QuotaUsed = 10
	usableKey := testAugmentOnlyAPIKey(42, user.ID, "sk-usable-active", fixedNow.Add(-time.Hour), entitledGroup)

	svc := NewAugmentPluginService(
		&config.Config{},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
				return nil, errors.New("unexpected generate call")
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keysByUser: map[int64][]APIKey{
				user.ID: {expiredKey, quotaExhaustedKey, usableKey},
			},
			availableByUser: map[int64][]Group{
				user.ID: {entitledGroup},
			},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{
			public: &PublicSettings{SiteName: "Augment Local"},
		},
	)
	svc.now = func() time.Time { return fixedNow }

	summary, err := svc.BuildSummary(context.Background(), AugmentPluginPrincipal{
		Kind: augmentPrincipalKindJWT,
		User: user,
	})
	require.NoError(t, err)
	require.Equal(t, usableKey.Key, summary.GatewayAPIKey)
	require.Equal(t, usableKey.Key, summary.PrimaryAPIKey)
}

func TestAugmentPluginServiceBuildSummaryAPIKeyPrincipalDoesNotLeakSiblingActiveKey(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:          14,
		Email:       "isolated@example.com",
		Username:    "isolated-user",
		Role:        RoleUser,
		Status:      StatusActive,
		Balance:     5.5,
		Concurrency: 1,
	}

	olderKey := APIKey{
		ID:        30,
		UserID:    user.ID,
		Key:       "sk-older-active",
		Name:      "older",
		Status:    StatusActive,
		CreatedAt: time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC),
	}
	currentKey := APIKey{
		ID:        31,
		UserID:    user.ID,
		Key:       "sk-current-active",
		Name:      "current",
		Status:    StatusActive,
		CreatedAt: time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC),
	}

	svc := NewAugmentPluginService(
		&config.Config{},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
				return nil, errors.New("unexpected generate call")
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keysByUser: map[int64][]APIKey{
				user.ID: {olderKey, currentKey},
			},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{
			public: &PublicSettings{SiteName: "Augment Local"},
		},
	)

	summary, err := svc.BuildSummary(context.Background(), AugmentPluginPrincipal{
		Kind: augmentPrincipalKindAPIKey,
		User: user,
		APIKey: &APIKey{
			ID:        currentKey.ID,
			UserID:    currentKey.UserID,
			Key:       currentKey.Key,
			Name:      currentKey.Name,
			Status:    currentKey.Status,
			CreatedAt: currentKey.CreatedAt,
		},
	})
	require.NoError(t, err)
	require.Equal(t, currentKey.Key, summary.GatewayAPIKey)
	require.Equal(t, currentKey.Key, summary.PrimaryAPIKey)
}

func TestAugmentPluginServiceBuildCompatMetadataPrefersCurrentAPIKeyGroupDefaultModel(t *testing.T) {
	t.Parallel()

	user := &User{
		ID:          7,
		Email:       "bound-group@example.com",
		Username:    "bound-group",
		Role:        RoleAdmin,
		Status:      StatusActive,
		Balance:     5,
		Concurrency: 2,
	}
	openAIGroupID := int64(3)
	openAIGroup := Group{
		ID:                    openAIGroupID,
		Name:                  "openai-default",
		Platform:              PlatformOpenAI,
		Status:                StatusActive,
		Hydrated:              true,
		AllowMessagesDispatch: false,
		SupportedModelScopes:  []string{"claude", "gemini_text", "gemini_image"},
	}
	anthropicGroup := Group{
		ID:                   2,
		Name:                 "anthropic-default",
		Platform:             PlatformAnthropic,
		Status:               StatusActive,
		Hydrated:             true,
		SupportedModelScopes: []string{"claude", "gemini_text", "gemini_image"},
	}
	apiKey := &APIKey{
		ID:        11,
		UserID:    user.ID,
		Key:       "sk-bound-openai",
		Name:      "bound-openai",
		Status:    StatusActive,
		GroupID:   &openAIGroupID,
		Group:     &openAIGroup,
		CreatedAt: time.Date(2026, 4, 25, 1, 0, 0, 0, time.UTC),
		User:      user,
	}

	svc := NewAugmentPluginService(
		&config.Config{Server: config.ServerConfig{FrontendURL: "https://fallback.local"}},
		&augmentAuthServiceStub{
			generateTokenPairFn: func(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
				return nil, errors.New("unexpected generate call")
			},
			refreshTokenPairFn: func(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
				return nil, errors.New("unexpected refresh call")
			},
			validateTokenFn: func(token string) (*JWTClaims, error) {
				return nil, ErrInvalidToken
			},
		},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{
			keyByValue: map[string]*APIKey{
				apiKey.Key: apiKey,
			},
			keysByUser: map[int64][]APIKey{
				user.ID: {*apiKey},
			},
			availableByUser: map[int64][]Group{
				user.ID: {anthropicGroup, openAIGroup},
			},
		},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{
			public: &PublicSettings{
				SiteName:   "Augment Local",
				APIBaseURL: "https://api.local.test",
			},
		},
	)

	compat, err := svc.BuildCompatMetadata(context.Background(), AugmentPluginPrincipal{
		Kind:   augmentPrincipalKindAPIKey,
		User:   user,
		APIKey: apiKey,
	}, "https://gateway.local")
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", compat.DefaultModel)
}

func TestAugmentPluginServiceLegacyBlobRetrievalRoundTripReturnsStoredRecords(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := &config.Config{Pricing: config.PricingConfig{DataDir: tmpDir}}
	user := &User{
		ID:       77,
		Email:    "retrieval@example.com",
		Username: "retrieval-user",
		Role:     RoleAdmin,
		Status:   StatusActive,
	}

	svc := NewAugmentPluginService(
		cfg,
		&augmentAuthServiceStub{},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"}},
	)

	namespace := "phase-f-local-retrieval"
	stored := svc.StoreLegacyBlobsForNamespace(namespace, []AugmentLegacyUploadedBlob{
		{
			BlobName: "blob-gateway",
			Path:     "backend/internal/server/routes/gateway.go",
			Content:  "r.POST(\"/agents/codebase-retrieval\", h.Auth.AugmentLegacyCodebaseRetrieval)\n",
		},
	})
	require.Equal(t, []string{"blob-gateway"}, stored)

	checkpointID, err := svc.AdvanceLegacyCheckpointForNamespace(namespace, "", []string{"blob-gateway"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, checkpointID)

	svcReloaded := NewAugmentPluginService(
		cfg,
		&augmentAuthServiceStub{},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"}},
	)

	resolved := svcReloaded.ResolveLegacyBlobsForNamespace(namespace, checkpointID, nil, nil)
	require.False(t, resolved.CheckpointNotFound)
	require.Equal(t, namespace, resolved.Namespace)
	require.Len(t, resolved.Records, 1)
	require.Empty(t, resolved.Unknown)
	require.Equal(t, "blob-gateway", resolved.Records[0].BlobName)
	require.Equal(t, "backend/internal/server/routes/gateway.go", resolved.Records[0].Path)
	require.Equal(t, "r.POST(\"/agents/codebase-retrieval\", h.Auth.AugmentLegacyCodebaseRetrieval)\n", resolved.Records[0].Content)

	formatted := svcReloaded.BuildLegacyFormattedRetrieval("find the retrieval route", resolved, 2000)
	require.Contains(t, formatted, "[CODEBASE_RETRIEVAL]")
	require.Contains(t, formatted, "request: find the retrieval route")
	require.Contains(t, formatted, "backend/internal/server/routes/gateway.go")
	require.Contains(t, formatted, "r.POST(\"/agents/codebase-retrieval\", h.Auth.AugmentLegacyCodebaseRetrieval)")
}

func TestAugmentPluginServiceLegacyBlobRetrievalFallsBackWhenCheckpointMissing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := &config.Config{Pricing: config.PricingConfig{DataDir: tmpDir}}
	user := &User{
		ID:       78,
		Email:    "fallback@example.com",
		Username: "fallback-user",
		Role:     RoleAdmin,
		Status:   StatusActive,
	}

	svc := NewAugmentPluginService(
		cfg,
		&augmentAuthServiceStub{},
		&augmentUserServiceStub{users: map[int64]*User{user.ID: user}},
		&augmentAPIKeyServiceStub{},
		&augmentSubscriptionServiceStub{},
		&augmentSettingServiceStub{public: &PublicSettings{SiteName: "逐梦站", APIBaseURL: "http://127.0.0.1:18081"}},
	)

	namespace := "phase-f-fallback"
	stored := svc.StoreLegacyBlobsForNamespace(namespace, []AugmentLegacyUploadedBlob{
		{
			BlobName: "blob-runtime",
			Path:     "backend/internal/handler/auth_augment_runtime.go",
			Content:  "func (h *AuthHandler) AugmentLegacyCodebaseRetrieval(c *gin.Context) {}\n",
		},
	})
	require.Equal(t, []string{"blob-runtime"}, stored)

	resolved := svc.ResolveLegacyBlobsForNamespace(namespace, "checkpoint-does-not-exist", nil, nil)
	require.True(t, resolved.CheckpointNotFound)
	require.Equal(t, namespace, resolved.Namespace)
	require.NotEmpty(t, resolved.Records)
	require.Equal(t, "blob-runtime", resolved.Records[0].BlobName)
	require.Equal(t, "backend/internal/handler/auth_augment_runtime.go", resolved.Records[0].Path)
	require.Contains(t, svc.BuildLegacyFormattedRetrieval("find retrieval handler", resolved, 2000), "backend/internal/handler/auth_augment_runtime.go")
}

func TestAugmentPluginServiceLegacyFormattedRetrievalRanksExactRouteAboveNoise(t *testing.T) {
	t.Parallel()

	svc := NewAugmentPluginService(nil, nil, nil, nil, nil, nil)
	resolved := AugmentLegacyResolvedBlobs{
		Namespace: "phase-f-ranking",
		Records: []augmentLegacyBlobRecord{
			{
				BlobName: "blob-ent-noise",
				Path:     "ent/schema/generated_codebase_retrieval.go",
				Content:  "package ent\n// generated codebase retrieval metadata\n",
			},
			{
				BlobName: "blob-route",
				Path:     "backend/internal/server/routes/gateway.go",
				Content:  "r.POST(\"/agents/codebase-retrieval\", h.Auth.AugmentLegacyCodebaseRetrieval)\n",
			},
		},
	}

	formatted := svc.BuildLegacyFormattedRetrieval("where is /agents/codebase-retrieval registered?", resolved, 2000)
	require.Less(t,
		strings.Index(formatted, "### backend/internal/server/routes/gateway.go"),
		strings.Index(formatted, "### ent/schema/generated_codebase_retrieval.go"),
	)
}

func TestAugmentPluginServiceLegacyFormattedRetrievalRanksExactSymbolAbovePathNoise(t *testing.T) {
	t.Parallel()

	svc := NewAugmentPluginService(nil, nil, nil, nil, nil, nil)
	resolved := AugmentLegacyResolvedBlobs{
		Namespace: "phase-f-symbol-ranking",
		Records: []augmentLegacyBlobRecord{
			{
				BlobName: "blob-doc",
				Path:     "docs/notes/augment_retrieval.md",
				Content:  "AugmentLegacyCodebaseRetrieval is mentioned in notes but not implemented here.\n",
			},
			{
				BlobName: "blob-handler",
				Path:     "backend/internal/handler/auth_augment_runtime.go",
				Content:  "func (h *AuthHandler) AugmentLegacyCodebaseRetrieval(c *gin.Context) {\n\tc.JSON(http.StatusOK, gin.H{\"formatted_retrieval\": text})\n}\n",
			},
		},
	}

	formatted := svc.BuildLegacyFormattedRetrieval("Find AugmentLegacyCodebaseRetrieval implementation", resolved, 2000)
	require.Less(t,
		strings.Index(formatted, "### backend/internal/handler/auth_augment_runtime.go"),
		strings.Index(formatted, "### docs/notes/augment_retrieval.md"),
	)
}

func TestAugmentPluginServiceLegacyFormattedRetrievalSelectsMatchedSnippetFromLargeFile(t *testing.T) {
	t.Parallel()

	svc := NewAugmentPluginService(nil, nil, nil, nil, nil, nil)
	var content strings.Builder
	for index := 0; index < 80; index++ {
		content.WriteString("func unrelated")
		content.WriteString(string(rune('A' + index%26)))
		content.WriteString("() {}\n")
	}
	content.WriteString("func augmentLegacyBuildChatMessages() { /* target */ }\n")
	for index := 0; index < 80; index++ {
		content.WriteString("func trailing")
		content.WriteString(string(rune('A' + index%26)))
		content.WriteString("() {}\n")
	}
	resolved := AugmentLegacyResolvedBlobs{
		Namespace: "phase-f-snippet",
		Records: []augmentLegacyBlobRecord{
			{
				BlobName: "blob-runtime",
				Path:     "backend/internal/handler/auth_augment_runtime.go",
				Content:  content.String(),
			},
		},
	}

	formatted := svc.BuildLegacyFormattedRetrieval("augmentLegacyBuildChatMessages", resolved, 900)
	require.Contains(t, formatted, "### backend/internal/handler/auth_augment_runtime.go")
	require.Contains(t, formatted, "augmentLegacyBuildChatMessages")
	require.NotContains(t, formatted, "func unrelatedA")
	require.NotContains(t, formatted, "func trailingZ")
}
