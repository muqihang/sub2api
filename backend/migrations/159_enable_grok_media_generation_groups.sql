-- Existing Grok groups may have been created before Grok media routes reused
-- the image-generation permission gate. Backfill them safely.
UPDATE groups
SET allow_image_generation = true
WHERE platform = 'grok'
  AND allow_image_generation = false;
