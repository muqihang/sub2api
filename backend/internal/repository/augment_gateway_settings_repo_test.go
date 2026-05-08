package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAugmentGatewaySettingsVersionConflict(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)
	repo := newAugmentGatewaySettingsRepositoryWithSQL(db)
	repo.now = func() time.Time { return now }

	namespace := "gateway.augment.provider_groups.openai"
	currentJSON := mustMarshalJSON(t, AugmentGatewayProviderGroupRecord{GroupID: 1001})
	nextJSON := mustMarshalJSON(t, AugmentGatewayProviderGroupRecord{GroupID: 1002})

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT namespace, settings_json, version, previous_version, rollback_snapshot_json, actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at FROM augment_gateway_settings_versions").
		WithArgs(namespace).
		WillReturnRows(sqlmock.NewRows([]string{
			"namespace", "settings_json", "version", "previous_version", "rollback_snapshot_json",
			"actor_admin_id", "request_id", "before_json", "after_json", "action", "result",
			"created_at", "updated_at",
		}).AddRow(
			namespace, currentJSON, int64(2), nil, currentJSON, int64(7), "req-prev", currentJSON, currentJSON, "update", "success", now.Add(-time.Minute), now.Add(-time.Minute),
		))
	mock.ExpectRollback()

	_, err := repo.Put(context.Background(), service.AugmentGatewaySettingsWriteInput{
		Namespace:       namespace,
		SettingsJSON:    nextJSON,
		ExpectedVersion: 1,
		ActorAdminID:    9,
		RequestID:       "req-conflict",
		Action:          service.AugmentGatewaySettingsActionUpdate,
	})
	require.ErrorIs(t, err, service.ErrAugmentGatewaySettingsVersionConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAugmentGatewaySettingsRollback(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Date(2026, 5, 8, 13, 30, 0, 0, time.UTC)
	repo := newAugmentGatewaySettingsRepositoryWithSQL(db)
	repo.now = func() time.Time { return now }

	namespace := "gateway.augment.enabled_models"
	currentJSON := mustMarshalJSON(t, map[string]AugmentGatewayModelRecord{
		"gpt-5.4": {Enabled: true, SmokeStatus: service.AugmentGatewaySmokeStatusPassed},
	})
	targetJSON := mustMarshalJSON(t, map[string]AugmentGatewayModelRecord{
		"gpt-5.4": {Enabled: false, SmokeStatus: service.AugmentGatewaySmokeStatusPending},
	})

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT namespace, settings_json, version, previous_version, rollback_snapshot_json, actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at FROM augment_gateway_settings_versions").
		WithArgs(namespace).
		WillReturnRows(sqlmock.NewRows([]string{
			"namespace", "settings_json", "version", "previous_version", "rollback_snapshot_json",
			"actor_admin_id", "request_id", "before_json", "after_json", "action", "result",
			"created_at", "updated_at",
		}).AddRow(
			namespace, currentJSON, int64(3), int64(2), currentJSON, int64(7), "req-current", currentJSON, currentJSON, "update", "success", now.Add(-2*time.Minute), now.Add(-time.Minute),
		))
	mock.ExpectQuery("SELECT namespace, settings_json, version, previous_version, rollback_snapshot_json, actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at FROM augment_gateway_settings_versions WHERE namespace = \\$1 AND version = \\$2").
		WithArgs(namespace, int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"namespace", "settings_json", "version", "previous_version", "rollback_snapshot_json",
			"actor_admin_id", "request_id", "before_json", "after_json", "action", "result",
			"created_at", "updated_at",
		}).AddRow(
			namespace, targetJSON, int64(1), nil, targetJSON, int64(6), "req-target", targetJSON, targetJSON, "create", "success", now.Add(-5*time.Minute), now.Add(-5*time.Minute),
		))
	mock.ExpectQuery("INSERT INTO augment_gateway_settings_versions").
		WithArgs(
			namespace,
			targetJSON,
			int64(4),
			int64(3),
			currentJSON,
			int64(42),
			"req-rollback",
			currentJSON,
			targetJSON,
			service.AugmentGatewaySettingsActionRollback,
			service.AugmentGatewaySettingsResultSuccess,
			now,
			now,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"namespace", "settings_json", "version", "previous_version", "rollback_snapshot_json",
			"actor_admin_id", "request_id", "before_json", "after_json", "action", "result",
			"created_at", "updated_at",
		}).AddRow(
			namespace, targetJSON, int64(4), int64(3), currentJSON, int64(42), "req-rollback", currentJSON, targetJSON, "rollback", "success", now, now,
		))
	mock.ExpectExec("INSERT INTO augment_gateway_settings_audit_logs").
		WithArgs(
			namespace,
			int64(4),
			int64(3),
			int64(42),
			"req-rollback",
			currentJSON,
			targetJSON,
			currentJSON,
			service.AugmentGatewaySettingsActionRollback,
			service.AugmentGatewaySettingsResultSuccess,
			now,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	record, err := repo.Rollback(context.Background(), service.AugmentGatewaySettingsRollbackInput{
		Namespace:       namespace,
		TargetVersion:   1,
		ExpectedVersion: 3,
		ActorAdminID:    42,
		RequestID:       "req-rollback",
	})
	require.NoError(t, err)
	require.Equal(t, int64(4), record.Version)
	require.JSONEq(t, string(targetJSON), string(record.SettingsJSON))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAugmentGatewaySettingsAuditStoresBeforeAfterDiff(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	repo := newAugmentGatewaySettingsRepositoryWithSQL(db)
	repo.now = func() time.Time { return now }

	namespace := "gateway.augment.provider_groups.deepseek"
	beforeJSON := mustMarshalJSON(t, AugmentGatewayProviderGroupRecord{GroupID: 2001})
	afterJSON := mustMarshalJSON(t, AugmentGatewayProviderGroupRecord{GroupID: 2002})

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT namespace, settings_json, version, previous_version, rollback_snapshot_json, actor_admin_id, request_id, before_json, after_json, action, result, created_at, updated_at FROM augment_gateway_settings_versions").
		WithArgs(namespace).
		WillReturnRows(sqlmock.NewRows([]string{
			"namespace", "settings_json", "version", "previous_version", "rollback_snapshot_json",
			"actor_admin_id", "request_id", "before_json", "after_json", "action", "result",
			"created_at", "updated_at",
		}).AddRow(
			namespace, beforeJSON, int64(1), nil, beforeJSON, int64(3), "req-before", beforeJSON, beforeJSON, "create", "success", now.Add(-time.Hour), now.Add(-time.Hour),
		))
	mock.ExpectQuery("INSERT INTO augment_gateway_settings_versions").
		WithArgs(
			namespace,
			afterJSON,
			int64(2),
			int64(1),
			beforeJSON,
			int64(99),
			"req-audit",
			beforeJSON,
			afterJSON,
			service.AugmentGatewaySettingsActionUpdate,
			service.AugmentGatewaySettingsResultSuccess,
			now,
			now,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"namespace", "settings_json", "version", "previous_version", "rollback_snapshot_json",
			"actor_admin_id", "request_id", "before_json", "after_json", "action", "result",
			"created_at", "updated_at",
		}).AddRow(
			namespace, afterJSON, int64(2), int64(1), beforeJSON, int64(99), "req-audit", beforeJSON, afterJSON, "update", "success", now, now,
		))
	mock.ExpectExec("INSERT INTO augment_gateway_settings_audit_logs").
		WithArgs(
			namespace,
			int64(2),
			int64(1),
			int64(99),
			"req-audit",
			beforeJSON,
			afterJSON,
			beforeJSON,
			service.AugmentGatewaySettingsActionUpdate,
			service.AugmentGatewaySettingsResultSuccess,
			now,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	record, err := repo.Put(context.Background(), service.AugmentGatewaySettingsWriteInput{
		Namespace:       namespace,
		SettingsJSON:    afterJSON,
		ExpectedVersion: 1,
		ActorAdminID:    99,
		RequestID:       "req-audit",
		Action:          service.AugmentGatewaySettingsActionUpdate,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), record.Version)
	require.JSONEq(t, string(afterJSON), string(record.SettingsJSON))
	require.NoError(t, mock.ExpectationsWereMet())
}

func mustMarshalJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}
