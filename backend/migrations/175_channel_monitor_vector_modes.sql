-- Extend channel monitor protocol modes for vector and ranking endpoints.
-- Existing rows keep their current values; only the check constraints change.

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_api_mode_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'embeddings', 'rerank'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_api_mode_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'embeddings', 'rerank'));
