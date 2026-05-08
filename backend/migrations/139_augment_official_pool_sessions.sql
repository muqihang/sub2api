CREATE TABLE IF NOT EXISTS augment_official_pool_bind_intents (
  id BIGSERIAL PRIMARY KEY,
  admin_user_id BIGINT NOT NULL,
  bind_intent_id TEXT NOT NULL UNIQUE,
  state_hash TEXT NOT NULL,
  mode TEXT NOT NULL,
  source TEXT NOT NULL,
  tenant_allowlist JSONB NOT NULL DEFAULT '[]'::jsonb,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS augment_official_pool_sessions (
  id BIGSERIAL PRIMARY KEY,
  source TEXT NOT NULL,
  tenant_origin TEXT NOT NULL,
  portal_origin TEXT,
  scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
  expires_at TIMESTAMPTZ,
  last_refresh_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  last_error_at TIMESTAMPTZ,
  last_error_code TEXT,
  status TEXT NOT NULL,
  encrypted_credential_payload BYTEA,
  credential_schema_version INTEGER NOT NULL DEFAULT 1,
  key_version TEXT NOT NULL,
  fingerprint TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at TIMESTAMPTZ,
  cooldown_until TIMESTAMPTZ,
  leased_at TIMESTAMPTZ,
  leased_until TIMESTAMPTZ,
  health_score INTEGER NOT NULL DEFAULT 100,
  created_by_admin_id BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_augment_official_pool_sessions_source_status
  ON augment_official_pool_sessions (source, status, health_score DESC, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_augment_official_pool_sessions_lease
  ON augment_official_pool_sessions (leased_until, cooldown_until);
