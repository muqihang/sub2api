package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

const (
	augmentOfficialSessionModeOfficialPassthrough = "official_passthrough"

	augmentOfficialSessionSourceOfficialQuickLogin = "official_quick_login"
	augmentOfficialSessionSourceWukongQuickLogin   = "wukong_quick_login"
	augmentOfficialSessionSourceManualImport       = "manual_import"

	augmentOfficialSessionStatusActive  = "active"
	augmentOfficialSessionStatusRevoked = "revoked"

	augmentOfficialSessionBindIntentTTL = 5 * time.Minute
)

var (
	ErrAugmentOfficialSessionSourceInvalid          = errors.New("augment official session source is invalid for v1")
	ErrAugmentOfficialSessionModeInvalid            = errors.New("augment official session mode is invalid for v1")
	ErrAugmentOfficialSessionStatusInvalid          = errors.New("augment official session status is invalid")
	ErrAugmentOfficialSessionBindIntentNotFound     = errors.New("augment official session bind intent not found")
	ErrAugmentOfficialSessionBindIntentExpired      = errors.New("augment official session bind intent expired")
	ErrAugmentOfficialSessionBindIntentCrossUser    = errors.New("augment official session bind intent belongs to another user")
	ErrAugmentOfficialSessionBindIntentConsumed     = errors.New("augment official session bind intent already consumed")
	ErrAugmentOfficialSessionCredentialPayloadEmpty = errors.New("augment official session credential payload is empty")
)

type augmentOfficialSessionRepository struct {
	client   *dbent.Client
	sql      sqlExecutor
	now      func() time.Time
	newToken func() (string, error)
}

type AugmentOfficialSessionBindIntentCreateInput struct {
	UserID          int64
	StateHash       string
	Mode            string
	Source          string
	TenantAllowlist []string
}

type AugmentOfficialSessionBindIntent struct {
	ID              int64
	UserID          int64
	BindIntentID    string
	StateHash       string
	Mode            string
	Source          string
	TenantAllowlist []string
	ExpiresAt       time.Time
	ConsumedAt      *time.Time
	CreatedAt       time.Time
}

type AugmentOfficialSessionUpsertInput struct {
	UserID                     int64
	Mode                       string
	Source                     string
	TenantOrigin               string
	PortalOrigin               *string
	Scopes                     []string
	ExpiresAt                  *time.Time
	LastRefreshAt              *time.Time
	LastSuccessAt              *time.Time
	LastErrorAt                *time.Time
	LastErrorCode              *string
	Status                     string
	EncryptedCredentialPayload []byte
	CredentialSchemaVersion    int
	KeyVersion                 string
	Fingerprint                string
}

type AugmentOfficialSessionPublicView struct {
	UserID                  int64      `json:"user_id"`
	Mode                    string     `json:"mode"`
	Source                  string     `json:"source"`
	TenantOrigin            string     `json:"tenant_origin"`
	PortalOrigin            *string    `json:"portal_origin,omitempty"`
	Scopes                  []string   `json:"scopes"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt           *time.Time `json:"last_refresh_at,omitempty"`
	LastSuccessAt           *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt             *time.Time `json:"last_error_at,omitempty"`
	LastErrorCode           *string    `json:"last_error_code,omitempty"`
	Status                  string     `json:"status"`
	CredentialSchemaVersion int        `json:"credential_schema_version"`
	KeyVersion              string     `json:"key_version"`
	Fingerprint             string     `json:"fingerprint"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	RevokedAt               *time.Time `json:"revoked_at,omitempty"`
}

type AugmentOfficialSessionAdminView struct {
	UserID                  int64      `json:"user_id"`
	Mode                    string     `json:"mode"`
	Source                  string     `json:"source"`
	TenantOrigin            string     `json:"tenant_origin"`
	PortalOrigin            *string    `json:"portal_origin,omitempty"`
	Scopes                  []string   `json:"scopes"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt           *time.Time `json:"last_refresh_at,omitempty"`
	LastSuccessAt           *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt             *time.Time `json:"last_error_at,omitempty"`
	LastErrorCode           *string    `json:"last_error_code,omitempty"`
	Status                  string     `json:"status"`
	CredentialSchemaVersion int        `json:"credential_schema_version"`
	KeyVersion              string     `json:"key_version"`
	Fingerprint             string     `json:"fingerprint"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	RevokedAt               *time.Time `json:"revoked_at,omitempty"`
	HasCredentialPayload    bool       `json:"has_credential_payload"`
}

type AugmentOfficialSessionCredentialRow struct {
	UserID                     int64
	Mode                       string
	Source                     string
	TenantOrigin               string
	PortalOrigin               *string
	Scopes                     []string
	ExpiresAt                  *time.Time
	LastRefreshAt              *time.Time
	LastSuccessAt              *time.Time
	LastErrorAt                *time.Time
	LastErrorCode              *string
	Status                     string
	EncryptedCredentialPayload []byte
	CredentialSchemaVersion    int
	KeyVersion                 string
	Fingerprint                string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	RevokedAt                  *time.Time
}

func NewAugmentOfficialSessionRepository(client *dbent.Client, sqlDB *sql.DB) *augmentOfficialSessionRepository {
	return newAugmentOfficialSessionRepositoryWithSQL(client, sqlDB)
}

func newAugmentOfficialSessionRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *augmentOfficialSessionRepository {
	return &augmentOfficialSessionRepository{
		client: client,
		sql:    sqlq,
		now: func() time.Time {
			return time.Now().UTC()
		},
		newToken: newAugmentOfficialSessionToken,
	}
}

func (r *augmentOfficialSessionRepository) CreateBindIntent(ctx context.Context, input AugmentOfficialSessionBindIntentCreateInput) (*AugmentOfficialSessionBindIntent, error) {
	mode, err := normalizeAugmentOfficialSessionMode(input.Mode)
	if err != nil {
		return nil, err
	}
	source, err := normalizeAugmentOfficialSessionSource(input.Source)
	if err != nil {
		return nil, err
	}
	bindIntentID, err := r.newToken()
	if err != nil {
		return nil, fmt.Errorf("generate augment official bind intent id: %w", err)
	}

	now := r.now().UTC()
	intent := &AugmentOfficialSessionBindIntent{
		UserID:          input.UserID,
		BindIntentID:    bindIntentID,
		StateHash:       input.StateHash,
		Mode:            mode,
		Source:          source,
		TenantAllowlist: cloneStringSlice(input.TenantAllowlist),
		ExpiresAt:       now.Add(augmentOfficialSessionBindIntentTTL),
	}

	tenantAllowlistJSON, err := marshalStringSlice(intent.TenantAllowlist)
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO augment_official_session_bind_intents (
			user_id, bind_intent_id, state_hash, mode, source, tenant_allowlist, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{
			intent.UserID,
			intent.BindIntentID,
			intent.StateHash,
			intent.Mode,
			intent.Source,
			tenantAllowlistJSON,
			intent.ExpiresAt,
		},
		&intent.ID,
		&intent.CreatedAt,
	); err != nil {
		return nil, err
	}

	return intent, nil
}

func (r *augmentOfficialSessionRepository) ConsumeBindIntent(ctx context.Context, bindIntentID string, userID int64) (*AugmentOfficialSessionBindIntent, error) {
	now := r.now().UTC()
	query := `
		WITH candidate AS (
			SELECT id, user_id, bind_intent_id, state_hash, mode, source, tenant_allowlist, expires_at, consumed_at, created_at
			FROM augment_official_session_bind_intents
			WHERE bind_intent_id = $1
			FOR UPDATE
		),
		updated AS (
			UPDATE augment_official_session_bind_intents AS intents
			SET consumed_at = $3
			FROM candidate
			WHERE intents.id = candidate.id
				AND candidate.user_id = $2
				AND candidate.expires_at > $3
				AND candidate.consumed_at IS NULL
			RETURNING intents.id
		)
		SELECT
			candidate.id,
			candidate.user_id,
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

	intent := &AugmentOfficialSessionBindIntent{}
	var tenantAllowlistJSON []byte
	var consumedAt sql.NullTime
	var claimed bool
	err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{bindIntentID, userID, now},
		&intent.ID,
		&intent.UserID,
		&intent.BindIntentID,
		&intent.StateHash,
		&intent.Mode,
		&intent.Source,
		&tenantAllowlistJSON,
		&intent.ExpiresAt,
		&consumedAt,
		&intent.CreatedAt,
		&claimed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAugmentOfficialSessionBindIntentNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(tenantAllowlistJSON, &intent.TenantAllowlist); err != nil {
		return nil, err
	}

	if intent.UserID != userID {
		return nil, ErrAugmentOfficialSessionBindIntentCrossUser
	}
	if !intent.ExpiresAt.After(now) {
		return nil, ErrAugmentOfficialSessionBindIntentExpired
	}
	if consumedAt.Valid || !claimed {
		return nil, ErrAugmentOfficialSessionBindIntentConsumed
	}

	intent.ConsumedAt = ptrTime(now)
	return intent, nil
}

func (r *augmentOfficialSessionRepository) UpsertActiveSession(ctx context.Context, input AugmentOfficialSessionUpsertInput) (*AugmentOfficialSessionAdminView, error) {
	mode, err := normalizeAugmentOfficialSessionMode(input.Mode)
	if err != nil {
		return nil, err
	}
	source, err := normalizeAugmentOfficialSessionSource(input.Source)
	if err != nil {
		return nil, err
	}
	status, err := normalizeAugmentOfficialSessionStatus(input.Status)
	if err != nil {
		return nil, err
	}
	if len(input.EncryptedCredentialPayload) == 0 {
		return nil, ErrAugmentOfficialSessionCredentialPayloadEmpty
	}

	now := r.now().UTC()
	scopesJSON, err := marshalStringSlice(input.Scopes)
	if err != nil {
		return nil, err
	}

	query := `
		WITH updated AS (
			UPDATE augment_official_sessions AS sessions
			SET mode = $2,
				source = $3,
				tenant_origin = $4,
				portal_origin = $5,
				scopes = $6,
				expires_at = $7,
				last_refresh_at = $8,
				last_success_at = $9,
				last_error_at = $10,
				last_error_code = $11,
				status = $12,
				encrypted_credential_payload = $13,
				credential_schema_version = $14,
				key_version = $15,
				fingerprint = $16,
				updated_at = $17,
				revoked_at = NULL
			WHERE sessions.id = (
				SELECT id
				FROM augment_official_sessions
				WHERE user_id = $1
				ORDER BY updated_at DESC, id DESC
				LIMIT 1
				FOR UPDATE
			)
			RETURNING
				sessions.user_id,
				sessions.mode,
				sessions.source,
				sessions.tenant_origin,
				sessions.portal_origin,
				sessions.scopes,
				sessions.expires_at,
				sessions.last_refresh_at,
				sessions.last_success_at,
				sessions.last_error_at,
				sessions.last_error_code,
				sessions.status,
				sessions.credential_schema_version,
				sessions.key_version,
				sessions.fingerprint,
				sessions.created_at,
				sessions.updated_at,
				sessions.revoked_at,
				(sessions.encrypted_credential_payload IS NOT NULL) AS has_credential_payload
		),
		inserted AS (
			INSERT INTO augment_official_sessions (
				user_id,
				mode,
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
				encrypted_credential_payload,
				credential_schema_version,
				key_version,
				fingerprint,
				updated_at
			)
			SELECT
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
			WHERE NOT EXISTS (SELECT 1 FROM updated)
			RETURNING
				user_id,
				mode,
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
				revoked_at,
				(encrypted_credential_payload IS NOT NULL) AS has_credential_payload
		)
		SELECT * FROM updated
		UNION ALL
		SELECT * FROM inserted
	`

	args := []any{
		input.UserID,
		mode,
		source,
		input.TenantOrigin,
		input.PortalOrigin,
		scopesJSON,
		normalizeTimePtr(input.ExpiresAt),
		normalizeTimePtr(input.LastRefreshAt),
		normalizeTimePtr(input.LastSuccessAt),
		normalizeTimePtr(input.LastErrorAt),
		input.LastErrorCode,
		status,
		input.EncryptedCredentialPayload,
		input.CredentialSchemaVersion,
		input.KeyVersion,
		input.Fingerprint,
		now,
	}
	return r.scanAdminViewRow(ctx, query, args)
}

func (r *augmentOfficialSessionRepository) GetActiveSessionPublicView(ctx context.Context, userID int64) (*AugmentOfficialSessionPublicView, error) {
	query := `
		SELECT user_id, mode, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at, last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint, created_at, updated_at, revoked_at
		FROM augment_official_sessions
		WHERE user_id = $1
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`
	view := &AugmentOfficialSessionPublicView{}
	var scopesJSON []byte
	var portalOrigin sql.NullString
	var expiresAt sql.NullTime
	var lastRefreshAt sql.NullTime
	var lastSuccessAt sql.NullTime
	var lastErrorAt sql.NullTime
	var lastErrorCode sql.NullString
	var revokedAt sql.NullTime
	err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{userID},
		&view.UserID,
		&view.Mode,
		&view.Source,
		&view.TenantOrigin,
		&portalOrigin,
		&scopesJSON,
		&expiresAt,
		&lastRefreshAt,
		&lastSuccessAt,
		&lastErrorAt,
		&lastErrorCode,
		&view.Status,
		&view.CredentialSchemaVersion,
		&view.KeyVersion,
		&view.Fingerprint,
		&view.CreatedAt,
		&view.UpdatedAt,
		&revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(scopesJSON, &view.Scopes); err != nil {
		return nil, err
	}
	applyNullString(&view.PortalOrigin, portalOrigin)
	applyNullTime(&view.ExpiresAt, expiresAt)
	applyNullTime(&view.LastRefreshAt, lastRefreshAt)
	applyNullTime(&view.LastSuccessAt, lastSuccessAt)
	applyNullTime(&view.LastErrorAt, lastErrorAt)
	applyNullString(&view.LastErrorCode, lastErrorCode)
	applyNullTime(&view.RevokedAt, revokedAt)
	return view, nil
}

func (r *augmentOfficialSessionRepository) GetActiveSessionAdminView(ctx context.Context, userID int64) (*AugmentOfficialSessionAdminView, error) {
	query := `
		SELECT user_id, mode, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at, last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint, created_at, updated_at, revoked_at, (encrypted_credential_payload IS NOT NULL) AS has_credential_payload
		FROM augment_official_sessions
		WHERE user_id = $1
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`
	view, err := r.scanAdminViewRow(ctx, query, []any{userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (r *augmentOfficialSessionRepository) GetActiveSessionCredentialRow(ctx context.Context, userID int64) (*AugmentOfficialSessionCredentialRow, error) {
	query := `
		SELECT user_id, mode, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at, last_error_at, last_error_code, status, encrypted_credential_payload, credential_schema_version, key_version, fingerprint, created_at, updated_at, revoked_at
		FROM augment_official_sessions
		WHERE user_id = $1
			AND status = '` + augmentOfficialSessionStatusActive + `'
			AND revoked_at IS NULL
			AND encrypted_credential_payload IS NOT NULL
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`
	row := &AugmentOfficialSessionCredentialRow{}
	var scopesJSON []byte
	var portalOrigin sql.NullString
	var expiresAt sql.NullTime
	var lastRefreshAt sql.NullTime
	var lastSuccessAt sql.NullTime
	var lastErrorAt sql.NullTime
	var lastErrorCode sql.NullString
	var revokedAt sql.NullTime
	err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{userID},
		&row.UserID,
		&row.Mode,
		&row.Source,
		&row.TenantOrigin,
		&portalOrigin,
		&scopesJSON,
		&expiresAt,
		&lastRefreshAt,
		&lastSuccessAt,
		&lastErrorAt,
		&lastErrorCode,
		&row.Status,
		&row.EncryptedCredentialPayload,
		&row.CredentialSchemaVersion,
		&row.KeyVersion,
		&row.Fingerprint,
		&row.CreatedAt,
		&row.UpdatedAt,
		&revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(scopesJSON, &row.Scopes); err != nil {
		return nil, err
	}
	applyNullString(&row.PortalOrigin, portalOrigin)
	applyNullTime(&row.ExpiresAt, expiresAt)
	applyNullTime(&row.LastRefreshAt, lastRefreshAt)
	applyNullTime(&row.LastSuccessAt, lastSuccessAt)
	applyNullTime(&row.LastErrorAt, lastErrorAt)
	applyNullString(&row.LastErrorCode, lastErrorCode)
	applyNullTime(&row.RevokedAt, revokedAt)
	return row, nil
}

func (r *augmentOfficialSessionRepository) RevokeActiveSession(ctx context.Context, userID int64) (*AugmentOfficialSessionAdminView, error) {
	now := r.now().UTC()
	query := `
		UPDATE augment_official_sessions
		SET encrypted_credential_payload = NULL, status = $2, revoked_at = $3, updated_at = $3
		WHERE id = (
			SELECT id
			FROM augment_official_sessions
			WHERE user_id = $1
				AND status <> $2
			ORDER BY updated_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		)
		RETURNING
			user_id,
			mode,
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
			revoked_at,
			(encrypted_credential_payload IS NOT NULL) AS has_credential_payload
	`
	view, err := r.scanAdminViewRow(ctx, query, []any{userID, augmentOfficialSessionStatusRevoked, now})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (r *augmentOfficialSessionRepository) scanAdminViewRow(ctx context.Context, query string, args []any) (*AugmentOfficialSessionAdminView, error) {
	view := &AugmentOfficialSessionAdminView{}
	var scopesJSON []byte
	var portalOrigin sql.NullString
	var expiresAt sql.NullTime
	var lastRefreshAt sql.NullTime
	var lastSuccessAt sql.NullTime
	var lastErrorAt sql.NullTime
	var lastErrorCode sql.NullString
	var revokedAt sql.NullTime
	err := scanSingleRow(
		ctx,
		r.sql,
		query,
		args,
		&view.UserID,
		&view.Mode,
		&view.Source,
		&view.TenantOrigin,
		&portalOrigin,
		&scopesJSON,
		&expiresAt,
		&lastRefreshAt,
		&lastSuccessAt,
		&lastErrorAt,
		&lastErrorCode,
		&view.Status,
		&view.CredentialSchemaVersion,
		&view.KeyVersion,
		&view.Fingerprint,
		&view.CreatedAt,
		&view.UpdatedAt,
		&revokedAt,
		&view.HasCredentialPayload,
	)
	if err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(scopesJSON, &view.Scopes); err != nil {
		return nil, err
	}
	applyNullString(&view.PortalOrigin, portalOrigin)
	applyNullTime(&view.ExpiresAt, expiresAt)
	applyNullTime(&view.LastRefreshAt, lastRefreshAt)
	applyNullTime(&view.LastSuccessAt, lastSuccessAt)
	applyNullTime(&view.LastErrorAt, lastErrorAt)
	applyNullString(&view.LastErrorCode, lastErrorCode)
	applyNullTime(&view.RevokedAt, revokedAt)
	return view, nil
}

func normalizeAugmentOfficialSessionMode(mode string) (string, error) {
	switch mode {
	case augmentOfficialSessionModeOfficialPassthrough:
		return mode, nil
	default:
		return "", ErrAugmentOfficialSessionModeInvalid
	}
}

func normalizeAugmentOfficialSessionSource(source string) (string, error) {
	switch source {
	case augmentOfficialSessionSourceOfficialQuickLogin, augmentOfficialSessionSourceWukongQuickLogin:
		return source, nil
	case augmentOfficialSessionSourceManualImport:
		return "", ErrAugmentOfficialSessionSourceInvalid
	default:
		return "", ErrAugmentOfficialSessionSourceInvalid
	}
}

func normalizeAugmentOfficialSessionStatus(status string) (string, error) {
	switch status {
	case augmentOfficialSessionStatusActive, augmentOfficialSessionStatusRevoked:
		return status, nil
	default:
		return "", ErrAugmentOfficialSessionStatusInvalid
	}
}

func marshalStringSlice(in []string) ([]byte, error) {
	if len(in) == 0 {
		return []byte("[]"), nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal augment official session string slice: %w", err)
	}
	return data, nil
}

func unmarshalStringSlice(data []byte, out *[]string) error {
	if len(data) == 0 {
		*out = []string{}
		return nil
	}
	var decoded []string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("unmarshal augment official session string slice: %w", err)
	}
	if decoded == nil {
		decoded = []string{}
	}
	*out = decoded
	return nil
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func normalizeTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	t := in.UTC()
	return &t
}

func applyNullTime(dst **time.Time, src sql.NullTime) {
	if src.Valid {
		t := src.Time.UTC()
		*dst = &t
		return
	}
	*dst = nil
}

func applyNullString(dst **string, src sql.NullString) {
	if src.Valid {
		value := src.String
		*dst = &value
		return
	}
	*dst = nil
}

func newAugmentOfficialSessionToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
