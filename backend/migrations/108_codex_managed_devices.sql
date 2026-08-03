SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS codex_setup_grants (
    id BIGSERIAL PRIMARY KEY,
    code_hash VARCHAR(128) NOT NULL,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    mode VARCHAR(32) NOT NULL,
    server_origin VARCHAR(255) NOT NULL,
    gateway_origin VARCHAR(255) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT codex_setup_grants_code_hash_key UNIQUE (code_hash),
    CONSTRAINT codex_setup_grants_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT codex_setup_grants_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_codex_setup_grants_user_id ON codex_setup_grants(user_id);
CREATE INDEX IF NOT EXISTS idx_codex_setup_grants_api_key_id ON codex_setup_grants(api_key_id);
CREATE INDEX IF NOT EXISTS idx_codex_setup_grants_expires_at ON codex_setup_grants(expires_at);

CREATE TABLE IF NOT EXISTS codex_managed_devices (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    name VARCHAR(128) NOT NULL,
    platform VARCHAR(32) NOT NULL,
    arch VARCHAR(32) NOT NULL,
    manager_version VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    last_seen_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT codex_managed_devices_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT codex_managed_devices_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE,
    CONSTRAINT codex_managed_devices_status_check CHECK (status IN ('active', 'revoked', 'reauthorization_required'))
);

CREATE INDEX IF NOT EXISTS idx_codex_managed_devices_user_id ON codex_managed_devices(user_id);
CREATE INDEX IF NOT EXISTS idx_codex_managed_devices_api_key_id ON codex_managed_devices(api_key_id);
CREATE INDEX IF NOT EXISTS idx_codex_managed_devices_status ON codex_managed_devices(status);

CREATE TABLE IF NOT EXISTS codex_device_tokens (
    id BIGSERIAL PRIMARY KEY,
    device_id BIGINT NOT NULL,
    refresh_token_hash VARCHAR(128) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT codex_device_tokens_device_id_fkey FOREIGN KEY (device_id) REFERENCES codex_managed_devices(id) ON DELETE CASCADE,
    CONSTRAINT codex_device_tokens_refresh_token_hash_key UNIQUE (refresh_token_hash)
);

CREATE INDEX IF NOT EXISTS idx_codex_device_tokens_device_id ON codex_device_tokens(device_id);
CREATE INDEX IF NOT EXISTS idx_codex_device_tokens_expires_at ON codex_device_tokens(expires_at);

CREATE TABLE IF NOT EXISTS codex_device_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    device_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    event VARCHAR(64) NOT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT codex_device_audit_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES codex_managed_devices(id) ON DELETE CASCADE,
    CONSTRAINT codex_device_audit_logs_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_codex_device_audit_logs_device_id ON codex_device_audit_logs(device_id);
CREATE INDEX IF NOT EXISTS idx_codex_device_audit_logs_user_id ON codex_device_audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_codex_device_audit_logs_event ON codex_device_audit_logs(event);
CREATE INDEX IF NOT EXISTS idx_codex_device_audit_logs_created_at ON codex_device_audit_logs(created_at);
