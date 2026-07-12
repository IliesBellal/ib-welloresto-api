-- ============================================================================
-- 056 down — rollback UTC vers timezone marchand.
-- Conversion UTC -> heure locale marchand.
-- ============================================================================

UPDATE bookings b
INNER JOIN merchant m ON m.id = b.merchant_id
SET b.booking_date_from = COALESCE(
        CONVERT_TZ(b.booking_date_from, '+00:00', COALESCE(NULLIF(m.timezone, ''), 'UTC')),
        b.booking_date_from
    ),
    b.booking_date_to = CASE
        WHEN b.booking_date_to IS NULL THEN NULL
        ELSE COALESCE(
            CONVERT_TZ(b.booking_date_to, '+00:00', COALESCE(NULLIF(m.timezone, ''), 'UTC')),
            b.booking_date_to
        )
    END
WHERE b.booking_date_from IS NOT NULL;
