-- 156_content_moderation_matched_keyword.sql
-- Record which configured keyword triggered keyword-block risk-control logs.

ALTER TABLE content_moderation_logs
  ADD COLUMN IF NOT EXISTS matched_keyword VARCHAR(255) NOT NULL DEFAULT '';
