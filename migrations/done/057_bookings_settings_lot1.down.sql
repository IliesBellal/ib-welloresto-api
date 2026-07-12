-- ============================================================================
-- 057 down — rollback booking_duration_rules + colonnes Lot 1 ajoutees sur
-- bookings_settings.
-- ============================================================================

DROP TABLE IF EXISTS booking_duration_rules;

ALTER TABLE bookings_settings
    DROP COLUMN IF EXISTS pending_expiration_hours,
    DROP COLUMN IF EXISTS reserve_minimum_party_size,
    DROP COLUMN IF EXISTS max_booking_horizon_days,
    DROP COLUMN IF EXISTS overbooking_percent;