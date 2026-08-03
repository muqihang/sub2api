ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS entity_id BIGINT,
    ADD COLUMN IF NOT EXISTS entity_type VARCHAR(64),
    ADD COLUMN IF NOT EXISTS claimed_entity_id VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_usage_logs_entity_id
    ON usage_logs(entity_id)
    WHERE entity_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_usage_logs_entity_type
    ON usage_logs(entity_type)
    WHERE entity_type IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_usage_logs_claimed_entity_id
    ON usage_logs(claimed_entity_id)
    WHERE claimed_entity_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_usage_logs_entity_id_created_at
    ON usage_logs(entity_id, created_at)
    WHERE entity_id IS NOT NULL;
