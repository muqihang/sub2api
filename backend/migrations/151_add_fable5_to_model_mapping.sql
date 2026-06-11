-- Add claude-fable-5 passthrough for persisted Antigravity model mappings.
-- Accounts without a persisted model_mapping already use DefaultAntigravityModelMapping.
UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{model_mapping,claude-fable-5}',
    '"claude-fable-5"'::jsonb,
    true
)
WHERE platform = 'antigravity'
  AND credentials IS NOT NULL
  AND credentials ? 'model_mapping'
  AND credentials->'model_mapping' IS NOT NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND credentials->'model_mapping'->>'claude-fable-5' IS NULL;
