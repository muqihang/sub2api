CREATE TABLE IF NOT EXISTS entity_registry (
    id BIGSERIAL PRIMARY KEY,
    entity_key VARCHAR(128) NOT NULL UNIQUE,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    entity_type VARCHAR(64) NOT NULL DEFAULT 'workspace',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS entity_bindings (
    id BIGSERIAL PRIMARY KEY,
    entity_id BIGINT NOT NULL REFERENCES entity_registry(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT REFERENCES groups(id) ON DELETE CASCADE,
    account_id BIGINT REFERENCES accounts(id) ON DELETE CASCADE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT entity_bindings_scope_required CHECK (
        api_key_id IS NOT NULL
        OR user_id IS NOT NULL
        OR group_id IS NOT NULL
    ),
    CONSTRAINT entity_bindings_account_scope_unsupported CHECK (
        account_id IS NULL
    )
);

CREATE INDEX IF NOT EXISTS idx_entity_registry_status_type
    ON entity_registry(status, entity_type);

CREATE INDEX IF NOT EXISTS idx_entity_bindings_entity_id
    ON entity_bindings(entity_id);

CREATE INDEX IF NOT EXISTS idx_entity_bindings_api_key_id
    ON entity_bindings(api_key_id)
    WHERE api_key_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_entity_bindings_user_id
    ON entity_bindings(user_id)
    WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_entity_bindings_group_id
    ON entity_bindings(group_id)
    WHERE group_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_entity_bindings_default_api_key
    ON entity_bindings(api_key_id)
    WHERE is_default = TRUE AND status = 'active' AND api_key_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_entity_bindings_default_user
    ON entity_bindings(user_id)
    WHERE is_default = TRUE AND status = 'active' AND user_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_entity_bindings_default_group
    ON entity_bindings(group_id)
    WHERE is_default = TRUE AND status = 'active' AND group_id IS NOT NULL;
