package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newEntityRegistryTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE entity_registry (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_key TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT '',
			entity_type TEXT NOT NULL DEFAULT 'workspace',
			status TEXT NOT NULL DEFAULT 'active',
			metadata TEXT NOT NULL DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE entity_bindings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_id INTEGER NOT NULL REFERENCES entity_registry(id),
			api_key_id INTEGER,
			user_id INTEGER,
			group_id INTEGER,
			account_id INTEGER,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			status TEXT NOT NULL DEFAULT 'active',
			metadata TEXT NOT NULL DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)
	return db
}

func TestEntityRegistryRepository_CreateListGet(t *testing.T) {
	db := newEntityRegistryTestDB(t)
	repo := NewEntityRegistryRepository(db)
	ctx := context.Background()

	entity, err := repo.CreateEntity(ctx, service.CreateEntityInput{
		EntityKey:   "team-alpha",
		DisplayName: "Team Alpha",
		EntityType:  service.EntityTypeTeam,
		Metadata:    map[string]any{"region": "us"},
	})
	require.NoError(t, err)
	require.NotZero(t, entity.ID)
	require.Equal(t, "team-alpha", entity.EntityKey)
	require.Equal(t, service.EntityStatusActive, entity.Status)
	require.Equal(t, "us", entity.Metadata["region"])

	got, err := repo.GetEntityByKey(ctx, "team-alpha")
	require.NoError(t, err)
	require.Equal(t, entity.ID, got.ID)

	list, err := repo.ListEntities(ctx, service.EntityListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "team-alpha", list[0].EntityKey)
}

func TestEntityRegistryRepository_CreateListBindingsAndResolve(t *testing.T) {
	db := newEntityRegistryTestDB(t)
	repo := NewEntityRegistryRepository(db)
	ctx := context.Background()

	alpha, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-alpha", EntityType: service.EntityTypeTeam})
	require.NoError(t, err)
	beta, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-beta", EntityType: service.EntityTypeTeam})
	require.NoError(t, err)

	_, err = repo.CreateBinding(ctx, service.CreateEntityBindingInput{
		EntityID:  alpha.ID,
		APIKeyID:  entityRegistryPtrInt64(101),
		UserID:    entityRegistryPtrInt64(201),
		GroupID:   entityRegistryPtrInt64(301),
		IsDefault: true,
	})
	require.NoError(t, err)
	_, err = repo.CreateBinding(ctx, service.CreateEntityBindingInput{
		EntityID: beta.ID,
		UserID:   entityRegistryPtrInt64(201),
		GroupID:  entityRegistryPtrInt64(301),
	})
	require.NoError(t, err)

	bindings, err := repo.ListBindings(ctx, service.EntityBindingListFilter{UserID: entityRegistryPtrInt64(201)})
	require.NoError(t, err)
	require.Len(t, bindings, 2)

	resolved, err := repo.ResolveEntity(ctx, service.EntityResolutionInput{
		APIKeyID: 101,
		UserID:   201,
		GroupID:  entityRegistryPtrInt64(301),
	})
	require.NoError(t, err)
	require.Equal(t, alpha.ID, resolved.Entity.ID)
	require.Equal(t, service.EntityResolutionSourceDefaultBinding, resolved.Source)

	claimed, err := repo.ResolveEntity(ctx, service.EntityResolutionInput{
		APIKeyID:         101,
		UserID:           201,
		GroupID:          entityRegistryPtrInt64(301),
		ClaimedEntityKey: "team-beta",
	})
	require.NoError(t, err)
	require.Equal(t, beta.ID, claimed.Entity.ID)
	require.Equal(t, service.EntityResolutionSourceClaimedBinding, claimed.Source)

	_, err = repo.ResolveEntity(ctx, service.EntityResolutionInput{
		APIKeyID:         999,
		UserID:           999,
		GroupID:          entityRegistryPtrInt64(999),
		ClaimedEntityKey: "team-beta",
	})
	require.ErrorIs(t, err, service.ErrEntityNotAuthorized)
}

func TestEntityRegistryRepository_RejectsAccountScopedBindings(t *testing.T) {
	db := newEntityRegistryTestDB(t)
	repo := NewEntityRegistryRepository(db)
	ctx := context.Background()

	entity, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-alpha"})
	require.NoError(t, err)

	_, err = repo.CreateBinding(ctx, service.CreateEntityBindingInput{
		EntityID:  entity.ID,
		AccountID: entityRegistryPtrInt64(501),
		IsDefault: true,
	})
	require.Error(t, err)
}

func TestEntityRegistryRepository_ResolveIgnoresLegacyAccountScopedBindings(t *testing.T) {
	db := newEntityRegistryTestDB(t)
	repo := NewEntityRegistryRepository(db)
	ctx := context.Background()

	entity, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-alpha"})
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO entity_bindings (entity_id, account_id, is_default, status)
		VALUES (?, ?, TRUE, 'active')
	`, entity.ID, 501)
	require.NoError(t, err)

	resolved, err := repo.ResolveEntity(ctx, service.EntityResolutionInput{
		APIKeyID: 101,
		UserID:   201,
		GroupID:  entityRegistryPtrInt64(301),
	})
	require.NoError(t, err)
	require.Nil(t, resolved)
}

func TestEntityRegistryRepository_RejectsDuplicateSupportedDefaultScopes(t *testing.T) {
	db := newEntityRegistryTestDB(t)
	repo := NewEntityRegistryRepository(db)
	ctx := context.Background()

	alpha, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-alpha"})
	require.NoError(t, err)
	beta, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-beta"})
	require.NoError(t, err)

	_, err = repo.CreateBinding(ctx, service.CreateEntityBindingInput{
		EntityID:  alpha.ID,
		UserID:    entityRegistryPtrInt64(201),
		IsDefault: true,
	})
	require.NoError(t, err)
	_, err = repo.CreateBinding(ctx, service.CreateEntityBindingInput{
		EntityID:  beta.ID,
		UserID:    entityRegistryPtrInt64(201),
		IsDefault: true,
	})
	require.Error(t, err)

	_, err = repo.CreateBinding(ctx, service.CreateEntityBindingInput{
		EntityID:  beta.ID,
		GroupID:   entityRegistryPtrInt64(301),
		IsDefault: true,
	})
	require.NoError(t, err)
	_, err = repo.CreateBinding(ctx, service.CreateEntityBindingInput{
		EntityID:  alpha.ID,
		GroupID:   entityRegistryPtrInt64(301),
		IsDefault: true,
	})
	require.Error(t, err)
}

func TestEntityRegistryRepository_DefaultResolutionFailsClosedOnLegacyDuplicateTopScope(t *testing.T) {
	db := newEntityRegistryTestDB(t)
	repo := NewEntityRegistryRepository(db)
	ctx := context.Background()

	alpha, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-alpha"})
	require.NoError(t, err)
	beta, err := repo.CreateEntity(ctx, service.CreateEntityInput{EntityKey: "team-beta"})
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO entity_bindings (entity_id, user_id, is_default, status)
		VALUES (?, ?, TRUE, 'active'), (?, ?, TRUE, 'active')
	`, alpha.ID, 201, beta.ID, 201)
	require.NoError(t, err)

	resolved, err := repo.ResolveEntity(ctx, service.EntityResolutionInput{
		APIKeyID: 101,
		UserID:   201,
	})

	require.Error(t, err)
	require.Nil(t, resolved)
}

func entityRegistryPtrInt64(v int64) *int64 { return &v }
