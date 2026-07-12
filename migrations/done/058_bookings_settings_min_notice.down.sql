-- ============================================================================
-- 058 down — Remove min_booking_notice_minutes from bookings_settings.
-- ============================================================================

ALTER TABLE bookings_settings
    DROP COLUMN IF EXISTS min_booking_notice_minutes;
