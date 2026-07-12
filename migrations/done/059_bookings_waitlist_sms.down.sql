-- ============================================================================
-- 059 down — Réversion : journal d'events, liste d'attente, colonnes settings.
-- ============================================================================

DROP TABLE IF EXISTS booking_events;
DROP TABLE IF EXISTS booking_waitlist;

ALTER TABLE bookings_settings
    DROP COLUMN IF EXISTS waitlist_slot_expiry_minutes,
    DROP COLUMN IF EXISTS waitlist_max_size,
    DROP COLUMN IF EXISTS waitlist_enabled,
    DROP COLUMN IF EXISTS sms_enabled;
