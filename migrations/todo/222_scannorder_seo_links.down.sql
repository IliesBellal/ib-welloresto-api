ALTER TABLE scannorder_settings
  DROP COLUMN IF EXISTS seo_updated_at,
  DROP COLUMN IF EXISTS seo_links;
