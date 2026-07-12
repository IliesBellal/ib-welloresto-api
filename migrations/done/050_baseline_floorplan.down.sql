-- ============================================================================
-- 050 down — ⚠️ À N'EXÉCUTER QUE sur une base où la 050 a réellement créé les
-- tables (base vierge / environnement de dev). En production ces tables
-- préexistent au repo (héritage PHP) avec des données vivantes : le up y était
-- un no-op, ce down DÉTRUIRAIT les données. Seul le retrait de l'index ajouté
-- est sûr partout.
-- ============================================================================

ALTER TABLE booked_location
    DROP INDEX IF EXISTS idx_booked_location_location;

DROP TABLE IF EXISTS booked_location;
DROP TABLE IF EXISTS order_location;
DROP TABLE IF EXISTS floor_areas;
DROP TABLE IF EXISTS locations;
DROP TABLE IF EXISTS floors;
