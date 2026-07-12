-- ============================================================================
-- 060 down — Retrait de reminder_sent_at sur bookings.
-- ============================================================================

ALTER TABLE bookings
    DROP COLUMN IF EXISTS reminder_sent_at;
