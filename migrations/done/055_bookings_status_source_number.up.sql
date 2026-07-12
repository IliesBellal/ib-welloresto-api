-- ============================================================================
-- 055 — Bookings: statuts normalises + source + unicite booking_number.
-- Addendum: §7.1 (booking_number 6 chars + unique merchant) et fondations
-- Phase 1 session 1 (mapping legacy -> cible).
-- ============================================================================

-- 1) Colonne source (si absente) + backfill.
ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS source VARCHAR(16) NOT NULL DEFAULT 'staff' AFTER status;

UPDATE bookings
SET source = CASE
    WHEN created_by = 'WR_ONLINE_BOOKING' THEN 'web'
    ELSE 'staff'
END
WHERE source IS NULL OR source = '';

-- 2) Mapping statuts legacy -> cibles.
UPDATE bookings SET status = 'pending'   WHERE status = 'PENDING_APPROVAL';
UPDATE bookings SET status = 'confirmed' WHERE status = 'ACCEPTED';
UPDATE bookings SET status = 'seated'    WHERE status = 'ORDER_OPEN';
UPDATE bookings SET status = 'completed' WHERE status = '0';
UPDATE bookings SET status = 'cancelled' WHERE status = 'CANCELED';
UPDATE bookings SET status = 'denied'    WHERE status = 'DENIED';

-- 3) booking_number: remplir les vides puis dedoublonner par merchant.
-- Generation deterministe 6 chars alphanumeriques en base 36 depuis booking_id.
UPDATE bookings
SET booking_number = RIGHT(LPAD(UPPER(CONV(booking_id, 10, 36)), 6, '0'), 6)
WHERE booking_number IS NULL OR booking_number = '';

-- Duplicates intra-merchant: conserver la premiere occurrence, regenerer les autres.
UPDATE bookings b
INNER JOIN (
    SELECT booking_id,
           ROW_NUMBER() OVER (PARTITION BY merchant_id, booking_number ORDER BY booking_id) AS rn
    FROM bookings
    WHERE booking_number IS NOT NULL AND booking_number <> ''
) d ON d.booking_id = b.booking_id
SET b.booking_number = RIGHT(LPAD(UPPER(CONV(b.booking_id, 10, 36)), 6, '0'), 6)
WHERE d.rn > 1;

-- 4) Contrainte d'unicite cible.
ALTER TABLE bookings
    ADD CONSTRAINT uq_bookings_merchant_number UNIQUE (merchant_id, booking_number);
