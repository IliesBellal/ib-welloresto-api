-- ============================================================================
-- 056 — Bascule UTC des dates bookings (addendum §7.7, option A).
-- Conversion heure locale marchand -> UTC, reversible via 056 down.
-- ============================================================================

UPDATE bookings b
INNER JOIN merchant m ON m.id = b.merchant_id
SET b.booking_date_from = COALESCE(
        CONVERT_TZ(b.booking_date_from, COALESCE(NULLIF(m.timezone, ''), 'UTC'), '+00:00'),
        b.booking_date_from
    ),
    b.booking_date_to = CASE
        WHEN b.booking_date_to IS NULL THEN NULL
        ELSE COALESCE(
            CONVERT_TZ(b.booking_date_to, COALESCE(NULLIF(m.timezone, ''), 'UTC'), '+00:00'),
            b.booking_date_to
        )
    END
WHERE b.booking_date_from IS NOT NULL;
