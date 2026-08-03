CREATE TABLE IF NOT EXISTS entity_rate_limit_policies (
    id BIGSERIAL PRIMARY KEY,
    entity_id BIGINT NOT NULL REFERENCES entity_registry(id) ON DELETE CASCADE,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    rpm_limit INTEGER NOT NULL DEFAULT 0,
    tpm_limit INTEGER NOT NULL DEFAULT 0,
    concurrency_limit INTEGER NOT NULL DEFAULT 0,
    cost_limit_usd NUMERIC(18, 8) NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT entity_rate_limit_policies_nonnegative_limits CHECK (
        rpm_limit >= 0
        AND tpm_limit >= 0
        AND concurrency_limit >= 0
        AND cost_limit_usd >= 0
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_entity_rate_limit_policies_active_entity
    ON entity_rate_limit_policies(entity_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_entity_rate_limit_policies_entity_id
    ON entity_rate_limit_policies(entity_id);

CREATE INDEX IF NOT EXISTS idx_entity_rate_limit_policies_status
    ON entity_rate_limit_policies(status);
