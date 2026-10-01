-- Rollback du module CDS (147_cds_module.up.sql).
-- Ordre inverse des dépendances : les tables filles référencent cds_displays.

DROP TABLE IF EXISTS cds_media_items;
DROP TABLE IF EXISTS cds_settings;
DROP TABLE IF EXISTS cds_device_tokens;
DROP TABLE IF EXISTS cds_enrollment_codes;
DROP TABLE IF EXISTS cds_displays;
