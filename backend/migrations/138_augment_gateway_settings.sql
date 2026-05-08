CREATE TABLE IF NOT EXISTS augment_gateway_settings_versions (
  id BIGSERIAL PRIMARY KEY,
  namespace TEXT NOT NULL,
  settings_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  version BIGINT NOT NULL,
  previous_version BIGINT,
  rollback_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  actor_admin_id BIGINT,
  request_id TEXT,
  before_json JSONB,
  after_json JSONB,
  action TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (namespace, version)
);

CREATE INDEX IF NOT EXISTS idx_augment_gateway_settings_versions_namespace_created
  ON augment_gateway_settings_versions (namespace, created_at DESC);

CREATE TABLE IF NOT EXISTS augment_gateway_settings_audit_logs (
  id BIGSERIAL PRIMARY KEY,
  namespace TEXT NOT NULL,
  version BIGINT NOT NULL,
  previous_version BIGINT,
  actor_admin_id BIGINT,
  request_id TEXT,
  before_json JSONB,
  after_json JSONB,
  rollback_snapshot_json JSONB,
  action TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_augment_gateway_settings_audit_logs_namespace_created
  ON augment_gateway_settings_audit_logs (namespace, created_at DESC);
