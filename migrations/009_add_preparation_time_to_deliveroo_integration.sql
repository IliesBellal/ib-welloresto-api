-- Migration 009: add persisted preparation time for Deliveroo integration settings

ALTER TABLE integration_deliveroo
    ADD COLUMN preparation_time_minutes INT NOT NULL DEFAULT 0 COMMENT 'Default preparation time (minutes) sent to Deliveroo';
