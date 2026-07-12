-- ============================================================================
-- 057 — Bookings settings Lot 1: colonnes manquantes + booking_duration_rules.
--
-- Cette migration n'ajoute QUE les colonnes absentes confirmees en base :
--   - overbooking_percent
--   - max_booking_horizon_days
--   - reserve_minimum_party_size
--   - pending_expiration_hours
--
-- Les colonnes deja presentes (ex: auto_accept_reserve_bookings,
-- reserve_maximum_party_size, last_booking_offset_minutes, etc.) ne sont pas
-- recreees ni renommees.
-- ============================================================================

ALTER TABLE bookings_settings
    ADD COLUMN IF NOT EXISTS overbooking_percent INT NULL AFTER last_booking_offset_minutes,
    ADD COLUMN IF NOT EXISTS max_booking_horizon_days INT NULL AFTER overbooking_percent,
    ADD COLUMN IF NOT EXISTS reserve_minimum_party_size INT NULL AFTER reserve_maximum_party_size,
    ADD COLUMN IF NOT EXISTS pending_expiration_hours INT NOT NULL DEFAULT 24 AFTER cancel_booking_limit_offset_hours;

CREATE TABLE IF NOT EXISTS booking_duration_rules (
    rule_id           VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id       VARCHAR(64) NOT NULL,
    min_party_size    INT NOT NULL,
    max_party_size    INT NOT NULL,
    duration_minutes  INT NOT NULL,
    enabled           TINYINT(1) NOT NULL DEFAULT 1,
    created_at        DATETIME NOT NULL DEFAULT UTC_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT UTC_TIMESTAMP ON UPDATE UTC_TIMESTAMP,
    INDEX idx_bdr_merchant_enabled (merchant_id, enabled),
    INDEX idx_bdr_merchant_party_range (merchant_id, min_party_size, max_party_size)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;