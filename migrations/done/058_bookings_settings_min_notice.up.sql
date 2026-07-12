-- ============================================================================
-- 058 — Add missing min_booking_notice_minutes on bookings_settings.
--
-- Scope intentionally limited to a single nullable column required by
-- bookings settings API and availability rules.
-- ============================================================================

ALTER TABLE bookings_settings
    ADD COLUMN IF NOT EXISTS min_booking_notice_minutes INT NULL AFTER max_booking_horizon_days;
