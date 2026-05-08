WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY user_id
            ORDER BY updated_at DESC, id DESC
        ) AS row_num
    FROM augment_official_sessions
)
DELETE FROM augment_official_sessions AS sessions
USING ranked
WHERE sessions.id = ranked.id
  AND ranked.row_num > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_augment_official_sessions_user_id_unique
    ON augment_official_sessions(user_id);
