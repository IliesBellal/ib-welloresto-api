-- Reverts 157_demo_bookings_utm.up.sql. À n'appliquer qu'après avoir retiré
-- du code déployé l'écriture de ces colonnes (Repository.CreateBooking).
ALTER TABLE demo_bookings
    DROP COLUMN IF EXISTS utm_campaign,
    DROP COLUMN IF EXISTS utm_medium,
    DROP COLUMN IF EXISTS utm_source;
