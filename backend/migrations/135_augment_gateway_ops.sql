CREATE TABLE IF NOT EXISTS augment_official_session_bind_intents (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    bind_intent_id TEXT NOT NULL UNIQUE,
    state_hash TEXT NOT NULL,
    mode TEXT NOT NULL,
    source TEXT NOT NULL,
    tenant_allowlist JSONB NOT NULL DEFAULT '[]'::jsonb,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS augment_official_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    mode TEXT NOT NULL,
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
    fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_augment_official_sessions_user_status
    ON augment_official_sessions(user_id, status);
