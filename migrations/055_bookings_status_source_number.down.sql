-- ============================================================================
-- 055 down — rollback statuts/source/contrainte booking_number.
-- Note: la regeneration de booking_number est non bijective (irreversible a
-- l'identique), mais le rollback restaure un schema/jeu de valeurs legacy.
-- ============================================================================

ALTER TABLE bookings
    DROP INDEX IF EXISTS uq_bookings_merchant_number;

-- Mapping inverse cible -> legacy.
UPDATE bookings SET status = 'PENDING_APPROVAL' WHERE status = 'pending';
UPDATE bookings SET status = 'ACCEPTED'         WHERE status = 'confirmed';
UPDATE bookings SET status = 'ORDER_OPEN'       WHERE status = 'seated';
UPDATE bookings SET status = '0'                WHERE status = 'completed';
UPDATE bookings SET status = 'CANCELED'         WHERE status = 'cancelled';
UPDATE bookings SET status = 'DENIED'           WHERE status = 'denied';
UPDATE bookings SET status = 'CANCELED'         WHERE status = 'no_show';

ALTER TABLE bookings
    DROP COLUMN IF EXISTS source;
