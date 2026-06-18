-- Per-merchant, per-calendar-month counter of Google Maps API calls (Directions API,
-- triggered by GET /external/routes). Mirrors merchant_sms_monthly (Messaggio): count
-- only, no monetary cost column, upserted via ON DUPLICATE KEY UPDATE.
CREATE TABLE merchant_google_maps_monthly (
    merchant_id BIGINT NOT NULL,
    month DATE NOT NULL,
    call_count INT NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, month)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
