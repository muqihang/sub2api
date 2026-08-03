package repository

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type bytesNotContainingMatcher struct {
	forbidden string
}

func (m bytesNotContainingMatcher) Match(v driver.Value) bool {
	switch typed := v.(type) {
	case []byte:
		return !bytes.Contains(typed, []byte(m.forbidden))
	case string:
		return !bytes.Contains([]byte(typed), []byte(m.forbidden))
	default:
		return false
	}
}

func TestAugmentOfficialSessionRepoCreatesIntentAndConsumesOnce(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	repo := newAugmentOfficialSessionRepositoryWithSQL(nil, db)
	repo.now = func() time.Time { return now }
	repo.newToken = func() (string, error) { return "bind-intent-1", nil }

	ctx := context.Background()
	tenantAllowlist := []string{
		"https://official.augment.local",
		"https://portal.augment.local",
	}
	tenantAllowlistJSON, err := json.Marshal(tenantAllowlist)
	require.NoError(t, err)

	expiresAt := now.Add(augmentOfficialSessionBindIntentTTL)

	mock.ExpectQuery("INSERT INTO augment_official_session_bind_intents").
		WithArgs(
			int64(41),
			"bind-intent-1",
			"state-hash-1",
			augmentOfficialSessionModeOfficialPassthrough,
			augmentOfficialSessionSourceWukongQuickLogin,
			tenantAllowlistJSON,
			expiresAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(7), now))

	intent, err := repo.CreateBindIntent(ctx, AugmentOfficialSessionBindIntentCreateInput{
		UserID:          41,
		StateHash:       "state-hash-1",
		Mode:            augmentOfficialSessionModeOfficialPassthrough,
		Source:          augmentOfficialSessionSourceWukongQuickLogin,
		TenantAllowlist: tenantAllowlist,
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), intent.ID)
	require.Equal(t, "bind-intent-1", intent.BindIntentID)
	require.Equal(t, "state-hash-1", intent.StateHash)
	require.Equal(t, augmentOfficialSessionModeOfficialPassthrough, intent.Mode)
	require.Equal(t, augmentOfficialSessionSourceWukongQuickLogin, intent.Source)
	require.Equal(t, tenantAllowlist, intent.TenantAllowlist)
	require.Equal(t, expiresAt, intent.ExpiresAt)

	mock.ExpectQuery("WITH candidate AS \\(").
		WithArgs("bind-intent-1", int64(41), now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "bind_intent_id", "state_hash", "mode", "source", "tenant_allowlist", "expires_at", "consumed_at", "created_at", "claimed",
		}).AddRow(
			int64(7),
			int64(41),
			"bind-intent-1",
			"state-hash-1",
			augmentOfficialSessionModeOfficialPassthrough,
			augmentOfficialSessionSourceWukongQuickLogin,
			tenantAllowlistJSON,
			expiresAt,
			nil,
			now,
			true,
		))

	consumed, err := repo.ConsumeBindIntent(ctx, "bind-intent-1", 41)
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)
	require.Equal(t, now, consumed.ConsumedAt.UTC())

	mock.ExpectQuery("WITH candidate AS \\(").
		WithArgs("bind-intent-1", int64(41), now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "bind_intent_id", "state_hash", "mode", "source", "tenant_allowlist", "expires_at", "consumed_at", "created_at", "claimed",
		}).AddRow(
			int64(7),
			int64(41),
			"bind-intent-1",
			"state-hash-1",
			augmentOfficialSessionModeOfficialPassthrough,
			augmentOfficialSessionSourceWukongQuickLogin,
			tenantAllowlistJSON,
			expiresAt,
			now,
			now,
			false,
		))

	_, err = repo.ConsumeBindIntent(ctx, "bind-intent-1", 41)
	require.ErrorIs(t, err, ErrAugmentOfficialSessionBindIntentConsumed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAugmentOfficialSessionRepoRejectsExpiredIntent(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	repo := newAugmentOfficialSessionRepositoryWithSQL(nil, db)
	repo.now = func() time.Time { return now }

	tenantAllowlistJSON, err := json.Marshal([]string{"https://official.augment.local"})
	require.NoError(t, err)

	mock.ExpectQuery("WITH candidate AS \\(").
		WithArgs("expired-bind-intent", int64(52), now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "bind_intent_id", "state_hash", "mode", "source", "tenant_allowlist", "expires_at", "consumed_at", "created_at", "claimed",
		}).AddRow(
			int64(9),
			int64(52),
			"expired-bind-intent",
			"state-hash-expired",
			augmentOfficialSessionModeOfficialPassthrough,
			augmentOfficialSessionSourceOfficialQuickLogin,
			tenantAllowlistJSON,
			now.Add(-time.Second),
			nil,
			now.Add(-2*time.Minute),
			false,
		))

	_, err = repo.ConsumeBindIntent(context.Background(), "expired-bind-intent", 52)
	require.ErrorIs(t, err, ErrAugmentOfficialSessionBindIntentExpired)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAugmentOfficialSessionRepoRejectsCrossUserIntent(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	repo := newAugmentOfficialSessionRepositoryWithSQL(nil, db)
	repo.now = func() time.Time { return now }

	tenantAllowlistJSON, err := json.Marshal([]string{"https://official.augment.local"})
	require.NoError(t, err)

	mock.ExpectQuery("WITH candidate AS \\(").
		WithArgs("foreign-bind-intent", int64(88), now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "bind_intent_id", "state_hash", "mode", "source", "tenant_allowlist", "expires_at", "consumed_at", "created_at", "claimed",
		}).AddRow(
			int64(10),
			int64(77),
			"foreign-bind-intent",
			"state-hash-foreign",
			augmentOfficialSessionModeOfficialPassthrough,
			augmentOfficialSessionSourceOfficialQuickLogin,
			tenantAllowlistJSON,
			now.Add(time.Minute),
			nil,
			now.Add(-time.Minute),
			false,
		))

	_, err = repo.ConsumeBindIntent(context.Background(), "foreign-bind-intent", 88)
	require.ErrorIs(t, err, ErrAugmentOfficialSessionBindIntentCrossUser)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAugmentOfficialSessionRepoStoresNoPlaintextCredentialPayload(t *testing.T) {
	t.Run("manual_import_is_rejected_for_v1", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := newAugmentOfficialSessionRepositoryWithSQL(nil, db)

		_, err := repo.UpsertActiveSession(context.Background(), AugmentOfficialSessionUpsertInput{
			UserID:                     12,
			Mode:                       augmentOfficialSessionModeOfficialPassthrough,
			Source:                     augmentOfficialSessionSourceManualImport,
			TenantOrigin:               "https://official.augment.local",
			Scopes:                     []string{"augment:session"},
			Status:                     augmentOfficialSessionStatusActive,
			EncryptedCredentialPayload: []byte("enc:v1:payload"),
			KeyVersion:                 "kv-1",
			CredentialSchemaVersion:    1,
			Fingerprint:                "fp-invalid-source",
		})
		require.ErrorIs(t, err, ErrAugmentOfficialSessionSourceInvalid)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("public_and_admin_views_do_not_expose_payload", func(t *testing.T) {
		db, mock := newSQLMock(t)
		now := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
		repo := newAugmentOfficialSessionRepositoryWithSQL(nil, db)
		repo.now = func() time.Time { return now }

		ctx := context.Background()
		scopes := []string{"augment:session", "augment:summary"}
		scopesJSON, err := json.Marshal(scopes)
		require.NoError(t, err)

		createdAt := now.Add(-time.Hour)
		updatedAt := now
		expiresAt := now.Add(45 * time.Minute)
		lastRefreshAt := now.Add(-10 * time.Minute)
		lastSuccessAt := now.Add(-2 * time.Minute)
		encryptedPayload := []byte("enc:v2:ciphertext")
		plaintextSecret := "refresh-token-plain"

		mock.ExpectQuery("INSERT INTO augment_official_sessions").
			WithArgs(
				int64(61),
				augmentOfficialSessionModeOfficialPassthrough,
				augmentOfficialSessionSourceOfficialQuickLogin,
				"https://official.augment.local",
				"https://portal.augment.local",
				scopesJSON,
				expiresAt,
				lastRefreshAt,
				lastSuccessAt,
				nil,
				nil,
				augmentOfficialSessionStatusActive,
				bytesNotContainingMatcher{forbidden: plaintextSecret},
				1,
				"kv-7",
				"fp-session-1",
				now,
			).
			WillReturnRows(sqlmock.NewRows([]string{
				"user_id", "mode", "source", "tenant_origin", "portal_origin", "scopes", "expires_at",
				"last_refresh_at", "last_success_at", "last_error_at", "last_error_code", "status",
				"credential_schema_version", "key_version", "fingerprint", "created_at", "updated_at", "revoked_at", "has_credential_payload",
			}).AddRow(
				int64(61),
				augmentOfficialSessionModeOfficialPassthrough,
				augmentOfficialSessionSourceOfficialQuickLogin,
				"https://official.augment.local",
				"https://portal.augment.local",
				scopesJSON,
				expiresAt,
				lastRefreshAt,
				lastSuccessAt,
				nil,
				nil,
				augmentOfficialSessionStatusActive,
				1,
				"kv-7",
				"fp-session-1",
				createdAt,
				updatedAt,
				nil,
				true,
			))

		adminView, err := repo.UpsertActiveSession(ctx, AugmentOfficialSessionUpsertInput{
			UserID:                     61,
			Mode:                       augmentOfficialSessionModeOfficialPassthrough,
			Source:                     augmentOfficialSessionSourceOfficialQuickLogin,
			TenantOrigin:               "https://official.augment.local",
			PortalOrigin:               stringPtr("https://portal.augment.local"),
			Scopes:                     scopes,
			ExpiresAt:                  &expiresAt,
			LastRefreshAt:              &lastRefreshAt,
			LastSuccessAt:              &lastSuccessAt,
			Status:                     augmentOfficialSessionStatusActive,
			EncryptedCredentialPayload: encryptedPayload,
			KeyVersion:                 "kv-7",
			CredentialSchemaVersion:    1,
			Fingerprint:                "fp-session-1",
		})
		require.NoError(t, err)
		require.NotNil(t, adminView)
		require.False(t, structHasField(reflect.TypeOf(*adminView), "EncryptedCredentialPayload"))

		mock.ExpectQuery("SELECT user_id, mode, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at, last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint, created_at, updated_at, revoked_at FROM augment_official_sessions").
			WithArgs(int64(61)).
			WillReturnRows(sqlmock.NewRows([]string{
				"user_id", "mode", "source", "tenant_origin", "portal_origin", "scopes", "expires_at",
				"last_refresh_at", "last_success_at", "last_error_at", "last_error_code", "status",
				"credential_schema_version", "key_version", "fingerprint", "created_at", "updated_at", "revoked_at",
			}).AddRow(
				int64(61),
				augmentOfficialSessionModeOfficialPassthrough,
				augmentOfficialSessionSourceOfficialQuickLogin,
				"https://official.augment.local",
				"https://portal.augment.local",
				scopesJSON,
				expiresAt,
				lastRefreshAt,
				lastSuccessAt,
				nil,
				nil,
				augmentOfficialSessionStatusActive,
				1,
				"kv-7",
				"fp-session-1",
				createdAt,
				updatedAt,
				nil,
			))

		publicView, err := repo.GetActiveSessionPublicView(ctx, 61)
		require.NoError(t, err)
		publicJSON, err := json.Marshal(publicView)
		require.NoError(t, err)
		require.NotContains(t, string(publicJSON), string(encryptedPayload))
		require.NotContains(t, string(publicJSON), plaintextSecret)
		require.False(t, structHasField(reflect.TypeOf(*publicView), "EncryptedCredentialPayload"))

		mock.ExpectQuery("SELECT user_id, mode, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at, last_error_at, last_error_code, status, credential_schema_version, key_version, fingerprint, created_at, updated_at, revoked_at, \\(encrypted_credential_payload IS NOT NULL\\) AS has_credential_payload FROM augment_official_sessions").
			WithArgs(int64(61)).
			WillReturnRows(sqlmock.NewRows([]string{
				"user_id", "mode", "source", "tenant_origin", "portal_origin", "scopes", "expires_at",
				"last_refresh_at", "last_success_at", "last_error_at", "last_error_code", "status",
				"credential_schema_version", "key_version", "fingerprint", "created_at", "updated_at", "revoked_at", "has_credential_payload",
			}).AddRow(
				int64(61),
				augmentOfficialSessionModeOfficialPassthrough,
				augmentOfficialSessionSourceOfficialQuickLogin,
				"https://official.augment.local",
				"https://portal.augment.local",
				scopesJSON,
				expiresAt,
				lastRefreshAt,
				lastSuccessAt,
				nil,
				nil,
				augmentOfficialSessionStatusActive,
				1,
				"kv-7",
				"fp-session-1",
				createdAt,
				updatedAt,
				nil,
				true,
			))

		adminView, err = repo.GetActiveSessionAdminView(ctx, 61)
		require.NoError(t, err)
		adminJSON, err := json.Marshal(adminView)
		require.NoError(t, err)
		require.NotContains(t, string(adminJSON), string(encryptedPayload))
		require.NotContains(t, string(adminJSON), plaintextSecret)

		mock.ExpectQuery("SELECT user_id, mode, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at, last_error_at, last_error_code, status, encrypted_credential_payload, credential_schema_version, key_version, fingerprint, created_at, updated_at, revoked_at FROM augment_official_sessions").
			WithArgs(int64(61)).
			WillReturnRows(sqlmock.NewRows([]string{
				"user_id", "mode", "source", "tenant_origin", "portal_origin", "scopes", "expires_at",
				"last_refresh_at", "last_success_at", "last_error_at", "last_error_code", "status",
				"encrypted_credential_payload", "credential_schema_version", "key_version", "fingerprint", "created_at", "updated_at", "revoked_at",
			}).AddRow(
				int64(61),
				augmentOfficialSessionModeOfficialPassthrough,
				augmentOfficialSessionSourceOfficialQuickLogin,
				"https://official.augment.local",
				"https://portal.augment.local",
				scopesJSON,
				expiresAt,
				lastRefreshAt,
				lastSuccessAt,
				nil,
				nil,
				augmentOfficialSessionStatusActive,
				encryptedPayload,
				1,
				"kv-7",
				"fp-session-1",
				createdAt,
				updatedAt,
				nil,
			))

		credentialRow, err := repo.GetActiveSessionCredentialRow(ctx, 61)
		require.NoError(t, err)
		require.NotNil(t, credentialRow)
		require.Equal(t, encryptedPayload, credentialRow.EncryptedCredentialPayload)
		require.Equal(t, "https://official.augment.local", credentialRow.TenantOrigin)
		require.Equal(t, augmentOfficialSessionStatusActive, credentialRow.Status)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAugmentOfficialSessionRepoRevokesAndClearsCredentialPayload(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	repo := newAugmentOfficialSessionRepositoryWithSQL(nil, db)
	repo.now = func() time.Time { return now }

	ctx := context.Background()
	scopesJSON, err := json.Marshal([]string{"augment:session"})
	require.NoError(t, err)
	createdAt := now.Add(-time.Hour)
	updatedAt := now

	mock.ExpectQuery("UPDATE augment_official_sessions SET encrypted_credential_payload = NULL, status = \\$2, revoked_at = \\$3, updated_at = \\$3").
		WithArgs(int64(91), augmentOfficialSessionStatusRevoked, now).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "mode", "source", "tenant_origin", "portal_origin", "scopes", "expires_at",
			"last_refresh_at", "last_success_at", "last_error_at", "last_error_code", "status",
			"credential_schema_version", "key_version", "fingerprint", "created_at", "updated_at", "revoked_at", "has_credential_payload",
		}).AddRow(
			int64(91),
			augmentOfficialSessionModeOfficialPassthrough,
			augmentOfficialSessionSourceOfficialQuickLogin,
			"https://official.augment.local",
			nil,
			scopesJSON,
			nil,
			nil,
			nil,
			nil,
			nil,
			augmentOfficialSessionStatusRevoked,
			1,
			"kv-11",
			"fp-revoke-1",
			createdAt,
			updatedAt,
			now,
			false,
		))

	revoked, err := repo.RevokeActiveSession(ctx, 91)
	require.NoError(t, err)
	require.NotNil(t, revoked)
	require.Equal(t, augmentOfficialSessionStatusRevoked, revoked.Status)
	require.NotNil(t, revoked.RevokedAt)
	require.False(t, revoked.HasCredentialPayload)

	mock.ExpectQuery("SELECT user_id, mode, source, tenant_origin, portal_origin, scopes, expires_at, last_refresh_at, last_success_at, last_error_at, last_error_code, status, encrypted_credential_payload, credential_schema_version, key_version, fingerprint, created_at, updated_at, revoked_at FROM augment_official_sessions").
		WithArgs(int64(91)).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "mode", "source", "tenant_origin", "portal_origin", "scopes", "expires_at",
			"last_refresh_at", "last_success_at", "last_error_at", "last_error_code", "status",
			"encrypted_credential_payload", "credential_schema_version", "key_version", "fingerprint", "created_at", "updated_at", "revoked_at",
		}))

	credentialRow, err := repo.GetActiveSessionCredentialRow(ctx, 91)
	require.NoError(t, err)
	require.Nil(t, credentialRow)
	require.NoError(t, mock.ExpectationsWereMet())
}

func structHasField(t reflect.Type, name string) bool {
	_, ok := t.FieldByName(name)
	return ok
}

func stringPtr(v string) *string {
	return &v
}
