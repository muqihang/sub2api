CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS batchimagejob_user_id_api_key_id_idempotency_key
    ON batch_image_jobs (user_id, api_key_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
