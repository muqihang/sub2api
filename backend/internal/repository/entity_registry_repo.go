package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type entityRegistrySQLDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type entityRegistryRepository struct {
	db entityRegistrySQLDB
}

func NewEntityRegistryRepository(sqlDB *sql.DB) service.EntityRegistryRepository {
	return &entityRegistryRepository{db: sqlDB}
}

func (r *entityRegistryRepository) CreateEntity(ctx context.Context, input service.CreateEntityInput) (*service.Entity, error) {
	normalized, err := service.ValidateCreateEntityInput(input)
	if err != nil {
		return nil, err
	}
	metadata, err := marshalEntityJSON(normalized.Metadata)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO entity_registry (entity_key, display_name, entity_type, status, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, entity_key, display_name, entity_type, status, metadata, created_at, updated_at
	`, normalized.EntityKey, normalized.DisplayName, normalized.EntityType, normalized.Status, metadata)
	return scanEntity(row)
}

func (r *entityRegistryRepository) GetEntityByID(ctx context.Context, id int64) (*service.Entity, error) {
	if id <= 0 {
		return nil, service.ErrEntityNotFound
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, entity_key, display_name, entity_type, status, metadata, created_at, updated_at
		FROM entity_registry
		WHERE id = $1
	`, id)
	return scanEntity(row)
}

func (r *entityRegistryRepository) GetEntityByKey(ctx context.Context, key string) (*service.Entity, error) {
	key = service.NormalizeEntityKey(key)
	if key == "" {
		return nil, service.ErrEntityNotFound
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, entity_key, display_name, entity_type, status, metadata, created_at, updated_at
		FROM entity_registry
		WHERE entity_key = $1
	`, key)
	return scanEntity(row)
}

func (r *entityRegistryRepository) ListEntities(ctx context.Context, filter service.EntityListFilter) ([]service.Entity, error) {
	var args []any
	where := []string{"1 = 1"}
	if status := strings.TrimSpace(filter.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if entityType := strings.TrimSpace(filter.EntityType); entityType != "" {
		args = append(args, entityType)
		where = append(where, fmt.Sprintf("entity_type = $%d", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, entity_key, display_name, entity_type, status, metadata, created_at, updated_at
		FROM entity_registry
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY id ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []service.Entity
	for rows.Next() {
		entity, err := scanEntityRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *entity)
	}
	return out, rows.Err()
}

func (r *entityRegistryRepository) CreateBinding(ctx context.Context, input service.CreateEntityBindingInput) (*service.EntityBinding, error) {
	normalized, err := service.ValidateCreateEntityBindingInput(input)
	if err != nil {
		return nil, err
	}
	if err := r.rejectActiveDefaultScopeConflict(ctx, normalized); err != nil {
		return nil, err
	}
	metadata, err := marshalEntityJSON(normalized.Metadata)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO entity_bindings (
			entity_id, api_key_id, user_id, group_id, account_id, is_default, status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, entity_id, api_key_id, user_id, group_id, account_id, is_default, status, metadata, created_at, updated_at
	`, normalized.EntityID, normalized.APIKeyID, normalized.UserID, normalized.GroupID, normalized.AccountID, normalized.IsDefault, normalized.Status, metadata)
	return scanEntityBinding(row)
}

func (r *entityRegistryRepository) ListBindings(ctx context.Context, filter service.EntityBindingListFilter) ([]service.EntityBinding, error) {
	var args []any
	where := []string{"1 = 1"}
	addPtr := func(column string, value *int64) {
		if value == nil {
			return
		}
		args = append(args, *value)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addPtr("entity_id", filter.EntityID)
	addPtr("api_key_id", filter.APIKeyID)
	addPtr("user_id", filter.UserID)
	addPtr("group_id", filter.GroupID)
	if status := strings.TrimSpace(filter.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, entity_id, api_key_id, user_id, group_id, account_id, is_default, status, metadata, created_at, updated_at
		FROM entity_bindings
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY id ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []service.EntityBinding
	for rows.Next() {
		binding, err := scanEntityBindingRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *binding)
	}
	return out, rows.Err()
}

func (r *entityRegistryRepository) ResolveEntity(ctx context.Context, input service.EntityResolutionInput) (*service.ResolvedEntity, error) {
	claimed := service.NormalizeEntityKey(input.ClaimedEntityKey)
	if claimed != "" {
		resolved, err := r.resolveClaimedEntity(ctx, input, claimed)
		if err != nil {
			if errors.Is(err, service.ErrEntityNotFound) {
				return nil, service.ErrEntityNotAuthorized
			}
			return nil, err
		}
		if resolved == nil {
			return nil, service.ErrEntityNotAuthorized
		}
		return resolved, nil
	}
	return r.resolveDefaultEntity(ctx, input)
}

func (r *entityRegistryRepository) resolveClaimedEntity(ctx context.Context, input service.EntityResolutionInput, claimed string) (*service.ResolvedEntity, error) {
	row := r.db.QueryRowContext(ctx, resolutionQuery(`
		e.entity_key = $1
	`, `
		ORDER BY
			CASE WHEN b.api_key_id IS NOT NULL THEN 4 ELSE 0 END +
			CASE WHEN b.user_id IS NOT NULL THEN 2 ELSE 0 END +
			CASE WHEN b.group_id IS NOT NULL THEN 1 ELSE 0 END DESC,
			b.id ASC
		LIMIT 1
	`), resolutionArgs(claimed, input)...)
	return scanResolvedEntity(row, service.EntityResolutionSourceClaimedBinding)
}

func (r *entityRegistryRepository) resolveDefaultEntity(ctx context.Context, input service.EntityResolutionInput) (*service.ResolvedEntity, error) {
	rows, err := r.db.QueryContext(ctx, resolutionQuery(`
		$1 = '' AND b.is_default = TRUE
	`, `
		ORDER BY
			CASE WHEN b.api_key_id IS NOT NULL THEN 4 ELSE 0 END +
			CASE WHEN b.user_id IS NOT NULL THEN 2 ELSE 0 END +
			CASE WHEN b.group_id IS NOT NULL THEN 1 ELSE 0 END DESC,
			b.id ASC
		LIMIT 2
	`), resolutionArgs("", input)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resolved []*service.ResolvedEntity
	for rows.Next() {
		item, scanErr := scanResolvedEntityRows(rows, service.EntityResolutionSourceDefaultBinding)
		if scanErr != nil {
			return nil, scanErr
		}
		resolved = append(resolved, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, nil
	}
	if len(resolved) > 1 && entityBindingScopeScore(resolved[0].Binding) == entityBindingScopeScore(resolved[1].Binding) {
		return nil, service.ErrEntityAmbiguous
	}
	return resolved[0], nil
}

func resolutionQuery(entityPredicate, suffix string) string {
	return `
		SELECT
			e.id, e.entity_key, e.display_name, e.entity_type, e.status, e.metadata, e.created_at, e.updated_at,
			b.id, b.entity_id, b.api_key_id, b.user_id, b.group_id, b.account_id, b.is_default, b.status, b.metadata, b.created_at, b.updated_at
		FROM entity_registry e
		JOIN entity_bindings b ON b.entity_id = e.id
		WHERE e.status = 'active'
		  AND b.status = 'active'
		  AND ` + entityPredicate + `
		  AND (b.api_key_id IS NULL OR b.api_key_id = $2)
		  AND (b.user_id IS NULL OR b.user_id = $3)
		  AND (b.group_id IS NULL OR b.group_id = $4)
		  AND b.account_id IS NULL
		  AND (b.api_key_id = $2 OR b.user_id = $3 OR b.group_id = $4)
	` + suffix
}

func resolutionArgs(first string, input service.EntityResolutionInput) []any {
	var groupID any
	if input.GroupID != nil {
		groupID = *input.GroupID
	}
	return []any{first, input.APIKeyID, input.UserID, groupID}
}

func (r *entityRegistryRepository) rejectActiveDefaultScopeConflict(ctx context.Context, input service.CreateEntityBindingInput) error {
	if !input.IsDefault || input.Status != service.EntityStatusActive {
		return nil
	}
	checks := []struct {
		column string
		value  *int64
	}{
		{column: "api_key_id", value: input.APIKeyID},
		{column: "user_id", value: input.UserID},
		{column: "group_id", value: input.GroupID},
	}
	for _, check := range checks {
		if check.value == nil {
			continue
		}
		var id int64
		err := r.db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT id
			FROM entity_bindings
			WHERE is_default = TRUE
			  AND status = 'active'
			  AND %s = $1
			LIMIT 1
		`, check.column), *check.value).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		return infraerrors.Conflict("ENTITY_DEFAULT_BINDING_CONFLICT", "default entity binding already exists for "+check.column)
	}
	return nil
}

func entityBindingScopeScore(binding *service.EntityBinding) int {
	if binding == nil {
		return 0
	}
	score := 0
	if binding.APIKeyID != nil {
		score += 4
	}
	if binding.UserID != nil {
		score += 2
	}
	if binding.GroupID != nil {
		score++
	}
	return score
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEntity(row scanner) (*service.Entity, error) {
	entity, err := scanEntityScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrEntityNotFound
	}
	return entity, err
}

func scanEntityRows(rows *sql.Rows) (*service.Entity, error) {
	return scanEntityScanner(rows)
}

func scanEntityScanner(row scanner) (*service.Entity, error) {
	var entity service.Entity
	var metadata string
	if err := row.Scan(
		&entity.ID,
		&entity.EntityKey,
		&entity.DisplayName,
		&entity.EntityType,
		&entity.Status,
		&metadata,
		&entity.CreatedAt,
		&entity.UpdatedAt,
	); err != nil {
		return nil, err
	}
	entity.Metadata = unmarshalEntityJSON(metadata)
	return &entity, nil
}

func scanEntityBinding(row scanner) (*service.EntityBinding, error) {
	binding, err := scanEntityBindingScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrEntityNotFound
	}
	return binding, err
}

func scanEntityBindingRows(rows *sql.Rows) (*service.EntityBinding, error) {
	return scanEntityBindingScanner(rows)
}

func scanEntityBindingScanner(row scanner) (*service.EntityBinding, error) {
	var binding service.EntityBinding
	var apiKeyID, userID, groupID, accountID sql.NullInt64
	var metadata string
	if err := row.Scan(
		&binding.ID,
		&binding.EntityID,
		&apiKeyID,
		&userID,
		&groupID,
		&accountID,
		&binding.IsDefault,
		&binding.Status,
		&metadata,
		&binding.CreatedAt,
		&binding.UpdatedAt,
	); err != nil {
		return nil, err
	}
	binding.APIKeyID = nullableInt64Ptr(apiKeyID)
	binding.UserID = nullableInt64Ptr(userID)
	binding.GroupID = nullableInt64Ptr(groupID)
	binding.AccountID = nullableInt64Ptr(accountID)
	binding.Metadata = unmarshalEntityJSON(metadata)
	return &binding, nil
}

func scanResolvedEntity(row scanner, source string) (*service.ResolvedEntity, error) {
	var entity service.Entity
	var entityMetadata string
	var binding service.EntityBinding
	var apiKeyID, userID, groupID, accountID sql.NullInt64
	var bindingMetadata string
	if err := row.Scan(
		&entity.ID,
		&entity.EntityKey,
		&entity.DisplayName,
		&entity.EntityType,
		&entity.Status,
		&entityMetadata,
		&entity.CreatedAt,
		&entity.UpdatedAt,
		&binding.ID,
		&binding.EntityID,
		&apiKeyID,
		&userID,
		&groupID,
		&accountID,
		&binding.IsDefault,
		&binding.Status,
		&bindingMetadata,
		&binding.CreatedAt,
		&binding.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrEntityNotFound
		}
		return nil, err
	}
	entity.Metadata = unmarshalEntityJSON(entityMetadata)
	binding.APIKeyID = nullableInt64Ptr(apiKeyID)
	binding.UserID = nullableInt64Ptr(userID)
	binding.GroupID = nullableInt64Ptr(groupID)
	binding.AccountID = nullableInt64Ptr(accountID)
	binding.Metadata = unmarshalEntityJSON(bindingMetadata)
	binding.Entity = &entity
	return &service.ResolvedEntity{Entity: entity, Binding: &binding, Source: source}, nil
}

func scanResolvedEntityRows(rows *sql.Rows, source string) (*service.ResolvedEntity, error) {
	return scanResolvedEntity(rows, source)
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func marshalEntityJSON(value map[string]any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalEntityJSON(value string) map[string]any {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(value), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
