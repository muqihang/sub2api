ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS client_product TEXT,
    ADD COLUMN IF NOT EXISTS request_scope TEXT,
    ADD COLUMN IF NOT EXISTS feature_scope TEXT,
    ADD COLUMN IF NOT EXISTS augment_session_id TEXT,
    ADD COLUMN IF NOT EXISTS route_policy_version TEXT,
    ADD COLUMN IF NOT EXISTS pricing_version TEXT,
    ADD COLUMN IF NOT EXISTS billable BOOLEAN,
    ADD COLUMN IF NOT EXISTS cost_source TEXT,
    ADD COLUMN IF NOT EXISTS currency TEXT,
    ADD COLUMN IF NOT EXISTS upstream_attempt_id TEXT,
    ADD COLUMN IF NOT EXISTS settlement_status TEXT,
    ADD COLUMN IF NOT EXISTS input_unit_price DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS output_unit_price DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS cache_read_unit_price DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS cache_creation_unit_price DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS reasoning_unit_price DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS estimated_cost DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS settled_cost DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS free_quota_applied DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS paid_balance_applied DOUBLE PRECISION;

CREATE INDEX IF NOT EXISTS idx_usage_logs_client_product_created_at
    ON usage_logs (client_product, created_at DESC);
