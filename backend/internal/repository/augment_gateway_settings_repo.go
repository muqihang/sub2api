package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type AugmentGatewayProviderGroupRecord = service.AugmentGatewayProviderGroupSetting
type AugmentGatewayModelRecord = service.AugmentGatewayModelSetting

type augmentGatewaySettingsRepository struct {
	db  *sql.DB
	now func() time.Time
}

func NewAugmentGatewaySettingsRepository(sqlDB *sql.DB) *augmentGatewaySettingsRepository {
	return newAugmentGatewaySettingsRepositoryWithSQL(sqlDB)
}

func newAugmentGatewaySettingsRepositoryWithSQL(sqlDB *sql.DB) *augmentGatewaySettingsRepository {
	return &augmentGatewaySettingsRepository{
		db: sqlDB,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (r *augmentGatewaySettingsRepository) ListLatest(ctx context.Context, namespacePrefix string) ([]service.AugmentGatewaySettingsVersion, error) {
	query := `
		SELECT DISTINCT ON (namespace)
			namespace,
			settings_json,
			version,
			previous_version,
			rollback_snapshot_json,
			actor_admin_id,
			request_id,
			before_json,
			after_json,
			action,
			result,
			created_at,
			updated_at
		FROM augment_gateway_settings_versions
		WHERE namespace LIKE $1
		ORDER BY namespace, version DESC
	`
	rows, err := r.db.QueryContext(ctx, query, namespacePrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []service.AugmentGatewaySettingsVersion
	for rows.Next() {
		record, err := scanAugmentGatewaySettingsVersionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *augmentGatewaySettingsRepository) GetLatest(ctx context.Context, namespace string) (*service.AugmentGatewaySettingsVersion, error) {
	return r.getLatest(ctx, r.db, namespace)
}

func (r *augmentGatewaySettingsRepository) Put(ctx context.Context, input service.AugmentGatewaySettingsWriteInput) (*service.AugmentGatewaySettingsVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := r.getLatest(ctx, tx, input.Namespace)
	if err != nil {
		return nil, err
	}
	if err := validateAugmentGatewayExpectedVersion(current, input.ExpectedVersion); err != nil {
		return nil, err
	}

	now := r.now()
	version := int64(1)
	var previousVersion *int64
	var beforeJSON json.RawMessage
	var rollbackSnapshotJSON json.RawMessage
	if current != nil {
		version = current.Version + 1
		previousVersion = ptrInt64(current.Version)
		beforeJSON = cloneJSON(current.SettingsJSON)
		rollbackSnapshotJSON = cloneJSON(current.SettingsJSON)
	} else {
		rollbackSnapshotJSON = cloneJSON(input.SettingsJSON)
	}
	afterJSON := cloneJSON(input.SettingsJSON)
	action := input.Action
	if action == "" {
		action = service.AugmentGatewaySettingsActionUpdate
	}

	record, err := r.insertVersion(ctx, tx, service.AugmentGatewaySettingsVersion{
		Namespace:            input.Namespace,
		SettingsJSON:         afterJSON,
		Version:              version,
		PreviousVersion:      previousVersion,
		RollbackSnapshotJSON: rollbackSnapshotJSON,
		ActorAdminID:         ptrInt64(input.ActorAdminID),
		RequestID:            input.RequestID,
		BeforeJSON:           beforeJSON,
		AfterJSON:            afterJSON,
		Action:               action,
		Result:               service.AugmentGatewaySettingsResultSuccess,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		if isAugmentGatewaySettingsUniqueViolation(err) {
			return nil, service.ErrAugmentGatewaySettingsVersionConflict
		}
		return nil, err
	}
	if err := r.insertAudit(ctx, tx, *record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return record, nil
}

func (r *augmentGatewaySettingsRepository) Rollback(ctx context.Context, input service.AugmentGatewaySettingsRollbackInput) (*service.AugmentGatewaySettingsVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := r.getLatest(ctx, tx, input.Namespace)
	if err != nil {
		return nil, err
	}
	if err := validateAugmentGatewayExpectedVersion(current, input.ExpectedVersion); err != nil {
		return nil, err
	}

	target, err := r.getByVersion(ctx, tx, input.Namespace, input.TargetVersion)
	if err != nil {
		return nil, err
	}
	now := r.now()
	record, err := r.insertVersion(ctx, tx, service.AugmentGatewaySettingsVersion{
		Namespace:            input.Namespace,
		SettingsJSON:         cloneJSON(target.SettingsJSON),
		Version:              current.Version + 1,
		PreviousVersion:      ptrInt64(current.Version),
		RollbackSnapshotJSON: cloneJSON(current.SettingsJSON),
		ActorAdminID:         ptrInt64(input.ActorAdminID),
		RequestID:            input.RequestID,
		BeforeJSON:           cloneJSON(current.SettingsJSON),
		AfterJSON:            cloneJSON(target.SettingsJSON),
		Action:               service.AugmentGatewaySettingsActionRollback,
		Result:               service.AugmentGatewaySettingsResultSuccess,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		if isAugmentGatewaySettingsUniqueViolation(err) {
			return nil, service.ErrAugmentGatewaySettingsVersionConflict
		}
		return nil, err
	}
	if err := r.insertAudit(ctx, tx, *record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return record, nil
}

func (r *augmentGatewaySettingsRepository) getLatest(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, namespace string) (*service.AugmentGatewaySettingsVersion, error) {
	query := `
		SELECT namespace, settings_json, version, previous_version, rollback_snapshot_json, actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at
		FROM augment_gateway_settings_versions
		WHERE namespace = $1
		ORDER BY version DESC
		LIMIT 1
	`
	record, err := scanAugmentGatewaySettingsVersion(ctx, q, query, []any{namespace})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (r *augmentGatewaySettingsRepository) getByVersion(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, namespace string, version int64) (*service.AugmentGatewaySettingsVersion, error) {
	query := `
		SELECT namespace, settings_json, version, previous_version, rollback_snapshot_json, actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at
		FROM augment_gateway_settings_versions
		WHERE namespace = $1 AND version = $2
	`
	return scanAugmentGatewaySettingsVersion(ctx, q, query, []any{namespace, version})
}

func (r *augmentGatewaySettingsRepository) insertVersion(ctx context.Context, tx *sql.Tx, record service.AugmentGatewaySettingsVersion) (*service.AugmentGatewaySettingsVersion, error) {
	query := `
		INSERT INTO augment_gateway_settings_versions (
			namespace, settings_json, version, previous_version, rollback_snapshot_json,
			actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING namespace, settings_json, version, previous_version, rollback_snapshot_json, actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at
	`
	return scanAugmentGatewaySettingsVersion(ctx, tx, query, []any{
		record.Namespace,
		record.SettingsJSON,
		record.Version,
		record.PreviousVersion,
		record.RollbackSnapshotJSON,
		record.ActorAdminID,
		record.RequestID,
		record.BeforeJSON,
		record.AfterJSON,
		record.Action,
		record.Result,
		record.CreatedAt,
		record.UpdatedAt,
	})
}

func (r *augmentGatewaySettingsRepository) insertAudit(ctx context.Context, tx *sql.Tx, record service.AugmentGatewaySettingsVersion) error {
	query := `
		INSERT INTO augment_gateway_settings_audit_logs (
			namespace, version, previous_version, actor_admin_id, request_id,
			before_json, after_json, rollback_snapshot_json, action, result, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := tx.ExecContext(ctx, query,
		record.Namespace,
		record.Version,
		record.PreviousVersion,
		record.ActorAdminID,
		record.RequestID,
		record.BeforeJSON,
		record.AfterJSON,
		record.RollbackSnapshotJSON,
		record.Action,
		record.Result,
		record.CreatedAt,
	)
	return err
}

func scanAugmentGatewaySettingsVersion(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, query string, args []any) (*service.AugmentGatewaySettingsVersion, error) {
	record := &service.AugmentGatewaySettingsVersion{}
	var previousVersion sql.NullInt64
	var actorAdminID sql.NullInt64
	var requestID sql.NullString
	if err := scanSingleRow(
		ctx,
		q,
		query,
		args,
		&record.Namespace,
		&record.SettingsJSON,
		&record.Version,
		&previousVersion,
		&record.RollbackSnapshotJSON,
		&actorAdminID,
		&requestID,
		&record.BeforeJSON,
		&record.AfterJSON,
		&record.Action,
		&record.Result,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if previousVersion.Valid {
		record.PreviousVersion = ptrInt64(previousVersion.Int64)
	}
	if actorAdminID.Valid {
		record.ActorAdminID = ptrInt64(actorAdminID.Int64)
	}
	if requestID.Valid {
		record.RequestID = requestID.String
	}
	return record, nil
}

func scanAugmentGatewaySettingsVersionRow(rows *sql.Rows) (service.AugmentGatewaySettingsVersion, error) {
	var record service.AugmentGatewaySettingsVersion
	var previousVersion sql.NullInt64
	var actorAdminID sql.NullInt64
	var requestID sql.NullString
	err := rows.Scan(
		&record.Namespace,
		&record.SettingsJSON,
		&record.Version,
		&previousVersion,
		&record.RollbackSnapshotJSON,
		&actorAdminID,
		&requestID,
		&record.BeforeJSON,
		&record.AfterJSON,
		&record.Action,
		&record.Result,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return service.AugmentGatewaySettingsVersion{}, err
	}
	if previousVersion.Valid {
		record.PreviousVersion = ptrInt64(previousVersion.Int64)
	}
	if actorAdminID.Valid {
		record.ActorAdminID = ptrInt64(actorAdminID.Int64)
	}
	if requestID.Valid {
		record.RequestID = requestID.String
	}
	return record, nil
}

func validateAugmentGatewayExpectedVersion(current *service.AugmentGatewaySettingsVersion, expectedVersion int64) error {
	if expectedVersion <= 0 {
		return nil
	}
	if current == nil || current.Version != expectedVersion {
		return service.ErrAugmentGatewaySettingsVersionConflict
	}
	return nil
}

func ptrInt64(value int64) *int64 {
	v := value
	return &v
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func isAugmentGatewaySettingsUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == "23505"
}
