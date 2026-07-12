//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexAgentSchema(t *testing.T) {
	tx := testTx(t)

	require.NoError(t, ApplyMigrations(context.Background(), integrationDB))

	var grantTable sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.codex_setup_grants')").Scan(&grantTable))
	require.True(t, grantTable.Valid, "expected codex_setup_grants table to exist")
	requireColumn(t, tx, "codex_setup_grants", "code_hash", "character varying", 128, false)
	requireColumn(t, tx, "codex_setup_grants", "expires_at", "timestamp with time zone", 0, false)
	requireColumn(t, tx, "codex_setup_grants", "consumed_at", "timestamp with time zone", 0, true)
	requireIndex(t, tx, "codex_setup_grants", "codex_setup_grants_code_hash_key")
	requireIndex(t, tx, "codex_setup_grants", "idx_codex_setup_grants_user_id")
	requireIndex(t, tx, "codex_setup_grants", "idx_codex_setup_grants_api_key_id")
	requireIndex(t, tx, "codex_setup_grants", "idx_codex_setup_grants_expires_at")
	requireForeignKey(t, tx, "codex_setup_grants", "codex_setup_grants_user_id_fkey", "user_id", "users")
	requireForeignKey(t, tx, "codex_setup_grants", "codex_setup_grants_api_key_id_fkey", "api_key_id", "api_keys")

	var deviceTable sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.codex_managed_devices')").Scan(&deviceTable))
	require.True(t, deviceTable.Valid, "expected codex_managed_devices table to exist")
	requireColumn(t, tx, "codex_managed_devices", "manager_version", "character varying", 64, false)
	requireColumn(t, tx, "codex_managed_devices", "status", "character varying", 32, false)
	requireColumn(t, tx, "codex_managed_devices", "last_seen_at", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "codex_managed_devices", "revoked_at", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "codex_managed_devices", "created_at", "timestamp with time zone", 0, false)
	requireColumn(t, tx, "codex_managed_devices", "updated_at", "timestamp with time zone", 0, false)
	requireIndex(t, tx, "codex_managed_devices", "idx_codex_managed_devices_user_id")
	requireIndex(t, tx, "codex_managed_devices", "idx_codex_managed_devices_api_key_id")
	requireIndex(t, tx, "codex_managed_devices", "idx_codex_managed_devices_status")
	requireForeignKey(t, tx, "codex_managed_devices", "codex_managed_devices_user_id_fkey", "user_id", "users")
	requireForeignKey(t, tx, "codex_managed_devices", "codex_managed_devices_api_key_id_fkey", "api_key_id", "api_keys")
	requireCodexColumnDefaultContains(t, tx, "codex_managed_devices", "status", "'active'")
	requireCheckConstraintContains(t, tx, "codex_managed_devices", "codex_managed_devices_status_check", "reauthorization_required")
	requireCheckConstraintContains(t, tx, "codex_managed_devices", "codex_managed_devices_status_check", "revoked")

	var tokenTable sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.codex_device_tokens')").Scan(&tokenTable))
	require.True(t, tokenTable.Valid, "expected codex_device_tokens table to exist")
	requireColumn(t, tx, "codex_device_tokens", "refresh_token_hash", "character varying", 128, false)
	requireColumn(t, tx, "codex_device_tokens", "rotated_at", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "codex_device_tokens", "revoked_at", "timestamp with time zone", 0, true)
	requireIndex(t, tx, "codex_device_tokens", "codex_device_tokens_refresh_token_hash_key")
	requireIndex(t, tx, "codex_device_tokens", "idx_codex_device_tokens_device_id")
	requireIndex(t, tx, "codex_device_tokens", "idx_codex_device_tokens_expires_at")
	requireForeignKey(t, tx, "codex_device_tokens", "codex_device_tokens_device_id_fkey", "device_id", "codex_managed_devices")

	var auditTable sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.codex_device_audit_logs')").Scan(&auditTable))
	require.True(t, auditTable.Valid, "expected codex_device_audit_logs table to exist")
	requireColumn(t, tx, "codex_device_audit_logs", "metadata", "jsonb", 0, false)
	requireColumn(t, tx, "codex_device_audit_logs", "created_at", "timestamp with time zone", 0, false)
	requireIndex(t, tx, "codex_device_audit_logs", "idx_codex_device_audit_logs_device_id")
	requireIndex(t, tx, "codex_device_audit_logs", "idx_codex_device_audit_logs_user_id")
	requireIndex(t, tx, "codex_device_audit_logs", "idx_codex_device_audit_logs_event")
	requireIndex(t, tx, "codex_device_audit_logs", "idx_codex_device_audit_logs_created_at")
	requireForeignKey(t, tx, "codex_device_audit_logs", "codex_device_audit_logs_device_id_fkey", "device_id", "codex_managed_devices")
	requireForeignKey(t, tx, "codex_device_audit_logs", "codex_device_audit_logs_user_id_fkey", "user_id", "users")
	requireCodexColumnDefaultContains(t, tx, "codex_device_audit_logs", "metadata", "'{}'::jsonb")
}

func requireForeignKey(t *testing.T, tx *sql.Tx, table, constraint, column, refTable string) {
	t.Helper()

	var gotColumn string
	var gotRefTable string
	err := tx.QueryRowContext(context.Background(), `
SELECT
  kcu.column_name,
  ccu.table_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON tc.constraint_name = ccu.constraint_name
 AND tc.table_schema = ccu.table_schema
WHERE tc.table_schema = 'public'
  AND tc.table_name = $1
  AND tc.constraint_name = $2
  AND tc.constraint_type = 'FOREIGN KEY'
`, table, constraint).Scan(&gotColumn, &gotRefTable)
	require.NoError(t, err, "query foreign key %s on %s", constraint, table)
	require.Equal(t, column, gotColumn, "foreign key column mismatch for %s", constraint)
	require.Equal(t, refTable, gotRefTable, "foreign key ref table mismatch for %s", constraint)
}

func requireCheckConstraintContains(t *testing.T, tx *sql.Tx, table, constraint, fragment string) {
	t.Helper()

	var definition string
	err := tx.QueryRowContext(context.Background(), `
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
JOIN pg_class r ON r.oid = c.conrelid
JOIN pg_namespace n ON n.oid = r.relnamespace
WHERE n.nspname = 'public'
  AND r.relname = $1
  AND c.conname = $2
  AND c.contype = 'c'
`, table, constraint).Scan(&definition)
	require.NoError(t, err, "query check constraint %s on %s", constraint, table)
	require.Contains(t, definition, fragment, "check constraint mismatch for %s", constraint)
}

func requireCodexColumnDefaultContains(t *testing.T, tx *sql.Tx, table, column, fragment string) {
	t.Helper()

	var defaultValue sql.NullString
	err := tx.QueryRowContext(context.Background(), `
SELECT column_default
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = $1
  AND column_name = $2
`, table, column).Scan(&defaultValue)
	require.NoError(t, err, "query default for %s.%s", table, column)
	require.True(t, defaultValue.Valid, "expected default for %s.%s", table, column)
	require.Contains(t, defaultValue.String, fragment, "default mismatch for %s.%s", table, column)
}
