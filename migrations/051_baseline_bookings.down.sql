-- ============================================================================
-- 051 down — ⚠️ À N'EXÉCUTER QUE sur une base où la 051 a réellement créé les
-- tables (base vierge / environnement de dev). En production ces tables
-- préexistent au repo (héritage PHP) avec des données vivantes : le up y était
-- un no-op, ce down DÉTRUIRAIT les données. Seul le retrait des index ajoutés
-- est sûr partout.
-- ============================================================================

ALTER TABLE bookings
    DROP INDEX IF EXISTS idx_bookings_merchant_date,
    DROP INDEX IF EXISTS idx_bookings_merchant_status;

ALTER TABLE hours_of_operation
    DROP INDEX IF EXISTS idx_hoo_merchant_day;

DROP TABLE IF EXISTS hours_of_operation;
DROP TABLE IF EXISTS bookings_settings;
DROP TABLE IF EXISTS bookings;
