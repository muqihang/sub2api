package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type augmentOfficialPoolSessionRepository struct {
	sql sqlExecutor
	now func() time.Time
}

func NewAugmentOfficialPoolSessionRepository(sqlDB *sql.DB) *augmentOfficialPoolSessionRepository {
	return &augmentOfficialPoolSessionRepository{
		sql: sqlDB,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (r *augmentOfficialPoolSessionRepository) CreateBindIntent(ctx context.Context, input service.AugmentOfficialPoolBindIntentStoreCreateInput) (*service.AugmentOfficialPoolBindIntentStoreRecord, error) {
	bindIntentID, err := newAugmentOfficialSessionToken()
	if err != nil {
		return nil, fmt.Errorf("generate augment official pool bind intent id: %w", err)
	}
	now := r.now().UTC()
	record := &service.AugmentOfficialPoolBindIntentStoreRecord{
		AdminUserID:     input.AdminUserID,
		BindIntentID:    bindIntentID,
		StateHash:       input.StateHash,
		Mode:            input.Mode,
		Source:          input.Source,
		TenantAllowlist: append([]string(nil), input.TenantAllowlist...),
		ExpiresAt:       now.Add(augmentOfficialSessionBindIntentTTL),
	}
	allowlistJSON, err := marshalStringSlice(record.TenantAllowlist)
	if err != nil {
		return nil, err
	}
	query := `
		INSERT INTO augment_official_pool_bind_intents (
			admin_user_id, bind_intent_id, state_hash, mode, source, tenant_allowlist, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	if err := scanSingleRow(ctx, r.sql, query, []any{
		record.AdminUserID,
		record.BindIntentID,
		record.StateHash,
		record.Mode,
		record.Source,
		allowlistJSON,
		record.ExpiresAt,
	}, &record.ID, &record.CreatedAt); err != nil {
		return nil, err
	}
	return record, nil
}

func (r *augmentOfficialPoolSessionRepository) ConsumeBindIntent(ctx context.Context, bindIntentID string, adminUserID int64) (*service.AugmentOfficialPoolBindIntentStoreRecord, error) {
	now := r.now().UTC()
	query := `
		WITH candidate AS (
			SELECT id, admin_user_id, bind_intent_id, state_hash, mode, source, tenant_allowlist, expires_at, consumed_at, created_at
			FROM augment_official_pool_bind_intents
			WHERE bind_intent_id = $1
			FOR UPDATE
		),
		updated AS (
			UPDATE augment_official_pool_bind_intents AS intents
			SET consumed_at = $3
			FROM candidate
			WHERE intents.id = candidate.id
				AND candidate.admin_user_id = $2
				AND candidate.expires_at > $3
				AND candidate.consumed_at IS NULL
			RETURNING intents.id
		)
		SELECT
			candidate.id,
			candidate.admin_user_id,
			candidate.bind_intent_id,
			candidate.state_hash,
			candidate.mode,
			candidate.source,
			candidate.tenant_allowlist,
			candidate.expires_at,
			candidate.consumed_at,
			candidate.created_at,
			EXISTS (SELECT 1 FROM updated) AS claimed
		FROM candidate
	`
	record := &service.AugmentOfficialPoolBindIntentStoreRecord{}
	var allowlistJSON []byte
	var consumedAt sql.NullTime
	var claimed bool
	err := scanSingleRow(ctx, r.sql, query, []any{bindIntentID, adminUserID, now},
		&record.ID,
		&record.AdminUserID,
		&record.BindIntentID,
		&record.StateHash,
		&record.Mode,
		&record.Source,
		&allowlistJSON,
		&record.ExpiresAt,
		&consumedAt,
		&record.CreatedAt,
		&claimed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAugmentOfficialBindTokenInvalid
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(allowlistJSON, &record.TenantAllowlist); err != nil {
		return nil, err
	}
	if consumedAt.Valid {
		t := consumedAt.Time.UTC()
		record.ConsumedAt = &t
	}
	if record.AdminUserID != adminUserID {
		return nil, service.ErrAugmentOfficialPoolBindIntentInvalid
	}
	if record.ExpiresAt.UTC().Before(now) || record.ExpiresAt.UTC().Equal(now) {
		return nil, service.ErrAugmentOfficialCredentialExpired
	}
	if !claimed {
		return nil, service.ErrAugmentOfficialSessionBindIntentConsumed
	}
	record.ConsumedAt = &now
	return record, nil
}

func (r *augmentOfficialPoolSessionRepository) UpsertPoolSession(ctx context.Context, input service.AugmentOfficialPoolStoredSessionInput) (*service.AugmentOfficialPoolStoredAdminView, error) {
	scopesJSON, err := marshalStringSlice(input.Scopes)
	if err != nil {
		return nil, err
	}
	now := r.now().UTC()
	query := `
		INSERT INTO augment_official_pool_sessions (
			source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at,
			last_error_at, last_error_code, status, encrypted_credential_payload, credential_schema_version,
			key_version, fingerprint, created_at, updated_at, health_score, created_by_admin_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18
		)
		ON CONFLICT (fingerprint) DO UPDATE
		SET
			source = EXCLUDED.source,
			tenant_origin = EXCLUDED.tenant_origin,
			portal_origin = EXCLUDED.portal_origin,
			scopes = EXCLUDED.scopes,
			expires_at = EXCLUDED.expires_at,
			last_refresh_at = EXCLUDED.last_refresh_at,
			last_success_at = EXCLUDED.last_success_at,
			last_error_at = EXCLUDED.last_error_at,
			last_error_code = EXCLUDED.last_error_code,
			status = EXCLUDED.status,
			encrypted_credential_payload = EXCLUDED.encrypted_credential_payload,
			credential_schema_version = EXCLUDED.credential_schema_version,
			key_version = EXCLUDED.key_version,
			updated_at = EXCLUDED.updated_at,
			health_score = EXCLUDED.health_score,
			cooldown_until = NULL,
			leased_at = NULL,
			leased_until = NULL,
			created_by_admin_id = EXCLUDED.created_by_admin_id
		RETURNING
			id,
			source,
			tenant_origin,
			portal_origin,
			scopes,
			expires_at,
			last_refresh_at,
			last_success_at,
			last_error_at,
			last_error_code,
			status,
			credential_schema_version,
			key_version,
			fingerprint,
			created_at,
			updated_at,
			last_used_at,
			cooldown_until,
			leased_at,
			leased_until,
			health_score,
			created_by_admin_id,
			(encrypted_credential_payload IS NOT NULL) AS has_credential_payload
	`
	return r.scanAdminViewRow(ctx, query, []any{
		input.Source,
		input.TenantOrigin,
		input.PortalOrigin,
		scopesJSON,
		normalizeTimePtr(input.ExpiresAt),
		normalizeTimePtr(input.LastRefreshAt),
		normalizeTimePtr(input.LastSuccessAt),
		normalizeTimePtr(input.LastErrorAt),
		input.LastErrorCode,
		input.Status,
		input.EncryptedCredentialPayload,
		input.CredentialSchemaVersion,
		input.KeyVersion,
		input.Fingerprint,
		now,
		now,
		input.HealthScore,
		input.CreatedByAdminID,
	})
}

func (r *augmentOfficialPoolSessionRepository) ListAdminSessions(ctx context.Context) ([]service.AugmentOfficialPoolStoredAdminView, error) {
	query := `
		SELECT
			id, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at,
			last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint,
			created_at, updated_at, last_used_at, cooldown_until, leased_at, leased_until,
			health_score, created_by_admin_id, (encrypted_credential_payload IS NOT NULL) AS has_credential_payload
		FROM augment_official_pool_sessions
		ORDER BY updated_at DESC, id DESC
	`
	rows, err := r.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]service.AugmentOfficialPoolStoredAdminView, 0)
	for rows.Next() {
		record, err := scanAugmentOfficialPoolAdminView(rows)
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

func (r *augmentOfficialPoolSessionRepository) GetAdminSession(ctx context.Context, sessionID int64) (*service.AugmentOfficialPoolStoredAdminView, error) {
	query := `
		SELECT
			id, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at,
			last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint,
			created_at, updated_at, last_used_at, cooldown_until, leased_at, leased_until,
			health_score, created_by_admin_id, (encrypted_credential_payload IS NOT NULL) AS has_credential_payload
		FROM augment_official_pool_sessions
		WHERE id = $1
	`
	record, err := r.scanAdminViewRow(ctx, query, []any{sessionID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (r *augmentOfficialPoolSessionRepository) AcquireUsableSession(ctx context.Context, source string, now, leaseUntil time.Time) (*service.AugmentOfficialPoolStoredCredentialRow, error) {
	query := `
		WITH candidate AS (
			SELECT id
			FROM augment_official_pool_sessions
			WHERE source = $1
				AND status = $2
				AND encrypted_credential_payload IS NOT NULL
				AND (expires_at IS NULL OR expires_at > $3)
				AND (cooldown_until IS NULL OR cooldown_until <= $3)
				AND (leased_until IS NULL OR leased_until <= $3)
			ORDER BY health_score DESC, COALESCE(last_success_at, created_at) DESC, updated_at DESC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE augment_official_pool_sessions AS pool
			SET leased_at = $3, leased_until = $4, updated_at = $3
			FROM candidate
			WHERE pool.id = candidate.id
			RETURNING
				pool.id,
				pool.source,
				pool.tenant_origin,
				pool.portal_origin,
				pool.scopes,
				pool.expires_at,
				pool.status,
				pool.encrypted_credential_payload,
				pool.credential_schema_version,
				pool.key_version,
				pool.fingerprint,
				pool.health_score
		)
		SELECT * FROM updated
	`
	record := &service.AugmentOfficialPoolStoredCredentialRow{}
	var scopesJSON []byte
	var portalOrigin sql.NullString
	var expiresAt sql.NullTime
	err := scanSingleRow(ctx, r.sql, query, []any{source, service.AugmentOfficialPoolSessionStatusActive, now, leaseUntil},
		&record.ID,
		&record.Source,
		&record.TenantOrigin,
		&portalOrigin,
		&scopesJSON,
		&expiresAt,
		&record.Status,
		&record.EncryptedCredentialPayload,
		&record.CredentialSchemaVersion,
		&record.KeyVersion,
		&record.Fingerprint,
		&record.HealthScore,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(scopesJSON, &record.Scopes); err != nil {
		return nil, err
	}
	applyNullString(&record.PortalOrigin, portalOrigin)
	applyNullTime(&record.ExpiresAt, expiresAt)
	return record, nil
}

func (r *augmentOfficialPoolSessionRepository) ReleaseLease(ctx context.Context, sessionID int64, input service.AugmentOfficialPoolLeaseReleaseInput) (*service.AugmentOfficialPoolStoredAdminView, error) {
	query := `
		UPDATE augment_official_pool_sessions
		SET
			leased_at = NULL,
			leased_until = NULL,
			last_used_at = CASE WHEN $2 THEN $1 ELSE last_used_at END,
			last_success_at = CASE WHEN $2 THEN $1 ELSE last_success_at END,
			last_error_at = CASE WHEN $2 THEN last_error_at ELSE $1 END,
			last_error_code = CASE WHEN $2 THEN NULL::text ELSE $3::text END,
			cooldown_until = CASE WHEN $2 THEN NULL::timestamptz ELSE $4::timestamptz END,
			updated_at = $1
		WHERE id = $5
		RETURNING
			id, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at,
			last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint,
			created_at, updated_at, last_used_at, cooldown_until, leased_at, leased_until,
			health_score, created_by_admin_id, (encrypted_credential_payload IS NOT NULL) AS has_credential_payload
	`
	record, err := r.scanAdminViewRow(ctx, query, []any{
		input.Now.UTC(),
		input.Success,
		nullStringPtr(input.ErrorCode),
		normalizeTimePtr(input.CooldownUntil),
		sessionID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (r *augmentOfficialPoolSessionRepository) RevokePoolSession(ctx context.Context, sessionID int64, status string, now time.Time) (*service.AugmentOfficialPoolStoredAdminView, error) {
	query := `
		UPDATE augment_official_pool_sessions
		SET
			encrypted_credential_payload = NULL,
			status = $2,
			last_error_code = NULL,
			leased_at = NULL,
			leased_until = NULL,
			cooldown_until = NULL,
			updated_at = $3
		WHERE id = $1
		RETURNING
			id, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at,
			last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint,
			created_at, updated_at, last_used_at, cooldown_until, leased_at, leased_until,
			health_score, created_by_admin_id, (encrypted_credential_payload IS NOT NULL) AS has_credential_payload
	`
	record, err := r.scanAdminViewRow(ctx, query, []any{sessionID, status, now.UTC()})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (r *augmentOfficialPoolSessionRepository) scanAdminViewRow(ctx context.Context, query string, args []any) (*service.AugmentOfficialPoolStoredAdminView, error) {
	record := &service.AugmentOfficialPoolStoredAdminView{}
	var scopesJSON []byte
	var portalOrigin sql.NullString
	var expiresAt sql.NullTime
	var lastRefreshAt sql.NullTime
	var lastSuccessAt sql.NullTime
	var lastErrorAt sql.NullTime
	var lastErrorCode sql.NullString
	var lastUsedAt sql.NullTime
	var cooldownUntil sql.NullTime
	var leasedAt sql.NullTime
	var leasedUntil sql.NullTime
	if err := scanSingleRow(ctx, r.sql, query, args,
		&record.ID,
		&record.Source,
		&record.TenantOrigin,
		&portalOrigin,
		&scopesJSON,
		&expiresAt,
		&lastRefreshAt,
		&lastSuccessAt,
		&lastErrorAt,
		&lastErrorCode,
		&record.Status,
		&record.CredentialSchemaVersion,
		&record.KeyVersion,
		&record.Fingerprint,
		&record.CreatedAt,
		&record.UpdatedAt,
		&lastUsedAt,
		&cooldownUntil,
		&leasedAt,
		&leasedUntil,
		&record.HealthScore,
		&record.CreatedByAdminID,
		&record.HasCredentialPayload,
	); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(scopesJSON, &record.Scopes); err != nil {
		return nil, err
	}
	applyNullString(&record.PortalOrigin, portalOrigin)
	applyNullTime(&record.ExpiresAt, expiresAt)
	applyNullTime(&record.LastRefreshAt, lastRefreshAt)
	applyNullTime(&record.LastSuccessAt, lastSuccessAt)
	applyNullTime(&record.LastErrorAt, lastErrorAt)
	applyNullString(&record.LastErrorCode, lastErrorCode)
	applyNullTime(&record.LastUsedAt, lastUsedAt)
	applyNullTime(&record.CooldownUntil, cooldownUntil)
	applyNullTime(&record.LeasedAt, leasedAt)
	applyNullTime(&record.LeasedUntil, leasedUntil)
	return record, nil
}

func scanAugmentOfficialPoolAdminView(rows *sql.Rows) (service.AugmentOfficialPoolStoredAdminView, error) {
	record := service.AugmentOfficialPoolStoredAdminView{}
	var scopesJSON []byte
	var portalOrigin sql.NullString
	var expiresAt sql.NullTime
	var lastRefreshAt sql.NullTime
	var lastSuccessAt sql.NullTime
	var lastErrorAt sql.NullTime
	var lastErrorCode sql.NullString
	var lastUsedAt sql.NullTime
	var cooldownUntil sql.NullTime
	var leasedAt sql.NullTime
	var leasedUntil sql.NullTime
	if err := rows.Scan(
		&record.ID,
		&record.Source,
		&record.TenantOrigin,
		&portalOrigin,
		&scopesJSON,
		&expiresAt,
		&lastRefreshAt,
		&lastSuccessAt,
		&lastErrorAt,
		&lastErrorCode,
		&record.Status,
		&record.CredentialSchemaVersion,
		&record.KeyVersion,
		&record.Fingerprint,
		&record.CreatedAt,
		&record.UpdatedAt,
		&lastUsedAt,
		&cooldownUntil,
		&leasedAt,
		&leasedUntil,
		&record.HealthScore,
		&record.CreatedByAdminID,
		&record.HasCredentialPayload,
	); err != nil {
		return service.AugmentOfficialPoolStoredAdminView{}, err
	}
	if err := unmarshalStringSlice(scopesJSON, &record.Scopes); err != nil {
		return service.AugmentOfficialPoolStoredAdminView{}, err
	}
	applyNullString(&record.PortalOrigin, portalOrigin)
	applyNullTime(&record.ExpiresAt, expiresAt)
	applyNullTime(&record.LastRefreshAt, lastRefreshAt)
	applyNullTime(&record.LastSuccessAt, lastSuccessAt)
	applyNullTime(&record.LastErrorAt, lastErrorAt)
	applyNullString(&record.LastErrorCode, lastErrorCode)
	applyNullTime(&record.LastUsedAt, lastUsedAt)
	applyNullTime(&record.CooldownUntil, cooldownUntil)
	applyNullTime(&record.LeasedAt, leasedAt)
	applyNullTime(&record.LeasedUntil, leasedUntil)
	return record, nil
}

func nullStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
