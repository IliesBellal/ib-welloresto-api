DROP TABLE IF EXISTS review_request_optouts;
DROP TABLE IF EXISTS review_requests;
ALTER TABLE scannorder_settings
  DROP COLUMN IF EXISTS review_request_enabled,
  DROP COLUMN IF EXISTS google_review_url,
  DROP COLUMN IF EXISTS google_maps_url,
  DROP COLUMN IF EXISTS google_place_id;
