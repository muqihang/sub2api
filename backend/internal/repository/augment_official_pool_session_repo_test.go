package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAugmentOfficialPoolSessionRepoReleaseLeaseSuccessCastsNullableFields(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewAugmentOfficialPoolSessionRepository(db)
	now := time.Date(2026, 5, 9, 10, 55, 0, 0, time.UTC)

	query := regexp.QuoteMeta(`
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
	`)

	scopesJSON := `["email"]`
	mock.ExpectQuery(query).
		WithArgs(now, true, (*string)(nil), (*time.Time)(nil), int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source", "tenant_origin", "portal_origin", "scopes", "expires_at", "last_refresh_at", "last_success_at",
			"last_error_at", "last_error_code", "status", "credential_schema_version", "key_version", "fingerprint",
			"created_at", "updated_at", "last_used_at", "cooldown_until", "leased_at", "leased_until",
			"health_score", "created_by_admin_id", "has_credential_payload",
		}).AddRow(
			int64(1),
			"official_quick_login",
			"https://d12.api.augmentcode.com",
			nil,
			[]byte(scopesJSON),
			nil,
			nil,
			now,
			nil,
			nil,
			service.AugmentOfficialPoolSessionStatusActive,
			int64(1),
			"local",
			"fp-1",
			now.Add(-time.Hour),
			now,
			now,
			nil,
			nil,
			nil,
			100,
			int64(1),
			true,
		))

	record, err := repo.ReleaseLease(context.Background(), 1, service.AugmentOfficialPoolLeaseReleaseInput{
		Now:     now,
		Success: true,
	})
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, int64(1), record.ID)
	require.Nil(t, record.LeasedAt)
	require.Nil(t, record.LeasedUntil)
	require.NoError(t, mock.ExpectationsWereMet())
}
