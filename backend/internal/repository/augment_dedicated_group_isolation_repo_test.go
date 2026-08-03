package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newAugmentIsolationSQLiteClient(t *testing.T) (*sql.DB, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return db, client
}

func mustCreateAugmentIsolationUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) *service.User {
	t.Helper()

	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	return userEntityToService(user)
}

func TestAugmentRepositoryGroupEntitlementRoundTrip(t *testing.T) {
	db, client := newAugmentIsolationSQLiteClient(t)
	repo := newGroupRepositoryWithSQL(client, db)
	ctx := context.Background()

	group := &service.Group{
		Name:                   "augment-entitled",
		Platform:               service.PlatformAnthropic,
		RateMultiplier:         1.0,
		Status:                 service.StatusActive,
		SubscriptionType:       service.SubscriptionTypeStandard,
		AugmentGatewayEntitled: true,
	}

	require.NoError(t, repo.Create(ctx, group))

	got, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.True(t, got.AugmentGatewayEntitled)

	group.AugmentGatewayEntitled = false
	require.NoError(t, repo.Update(ctx, group))

	got, err = repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.False(t, got.AugmentGatewayEntitled)
}

func TestAPIKeyRestrictionRepositoryAuthRoundTrip(t *testing.T) {
	db, client := newAugmentIsolationSQLiteClient(t)
	apiKeyRepo := newAPIKeyRepositoryWithSQL(client, db)
	groupRepo := newGroupRepositoryWithSQL(client, db)
	ctx := context.Background()

	user := mustCreateAugmentIsolationUser(t, ctx, client, "augment-repo@test.com")
	group := &service.Group{
		Name:                   "augment-group",
		Platform:               service.PlatformAnthropic,
		RateMultiplier:         1.0,
		Status:                 service.StatusActive,
		SubscriptionType:       service.SubscriptionTypeStandard,
		AugmentGatewayEntitled: true,
	}
	require.NoError(t, groupRepo.Create(ctx, group))

	restrictedClientProduct := "zhumeng_augment"
	key := &service.APIKey{
		UserID:                  user.ID,
		Key:                     "sk-augment-restricted",
		Name:                    "Augment Restricted",
		GroupID:                 &group.ID,
		Status:                  service.StatusActive,
		RestrictedClientProduct: &restrictedClientProduct,
	}
	require.NoError(t, apiKeyRepo.Create(ctx, key))

	gotByID, err := apiKeyRepo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, gotByID.RestrictedClientProduct)
	require.Equal(t, restrictedClientProduct, *gotByID.RestrictedClientProduct)

	gotForAuth, err := apiKeyRepo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, gotForAuth.RestrictedClientProduct)
	require.Equal(t, restrictedClientProduct, *gotForAuth.RestrictedClientProduct)
	require.NotNil(t, gotForAuth.Group)
	require.True(t, gotForAuth.Group.AugmentGatewayEntitled)
}

func TestAPIKeyRestrictionRepositoryDefaultsRemainBackwardCompatible(t *testing.T) {
	db, client := newAugmentIsolationSQLiteClient(t)
	apiKeyRepo := newAPIKeyRepositoryWithSQL(client, db)
	groupRepo := newGroupRepositoryWithSQL(client, db)
	ctx := context.Background()

	user := mustCreateAugmentIsolationUser(t, ctx, client, "augment-defaults@test.com")
	group := &service.Group{
		Name:             "default-group",
		Platform:         service.PlatformAnthropic,
		RateMultiplier:   1.0,
		Status:           service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard,
	}
	require.NoError(t, groupRepo.Create(ctx, group))

	gotGroup, err := groupRepo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.False(t, gotGroup.AugmentGatewayEntitled)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-unrestricted",
		Name:    "Unrestricted",
		GroupID: &group.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, apiKeyRepo.Create(ctx, key))

	gotKey, err := apiKeyRepo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Nil(t, gotKey.RestrictedClientProduct)
}
