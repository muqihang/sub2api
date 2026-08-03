ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS augment_gateway_entitled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS restricted_client_product TEXT NULL;
