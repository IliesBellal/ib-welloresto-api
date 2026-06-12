-- Migration: Add display_order column to tags table
-- Adds support for custom tag display ordering

ALTER TABLE tags ADD COLUMN display_order INT DEFAULT 0 AFTER merchant_id;
CREATE INDEX idx_tags_merchant_display_order ON tags(merchant_id, display_order);
