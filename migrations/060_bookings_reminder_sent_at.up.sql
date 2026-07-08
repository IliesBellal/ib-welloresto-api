-- ============================================================================
-- 060 — Phase 5 Lot 1 : marqueur d'envoi du rappel avant service.
--
-- Colonne unique : reminder_sent_at, posée par la tâche SendBookingReminders
-- pour éviter les doublons d'envoi. Rien d'autre dans cette migration.
-- ============================================================================

ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS reminder_sent_at TIMESTAMP NULL AFTER deletion_date;
