-- Migration 008: add temporary closure support for ScanNOrder

ALTER TABLE scannorder_settings
    ADD COLUMN closed_until DATETIME NULL DEFAULT NULL COMMENT 'If set in the future (UTC), ScanNOrder is temporarily closed until this timestamp';
