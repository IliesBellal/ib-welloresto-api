-- Migration: Add base_price column to orderitems table
-- Purpose: Track original price separately from discounted price to monitor promotions impact
-- Created: 2026-04-08

-- Add base_price column to store the original price before discounts
ALTER TABLE orderitems
ADD COLUMN base_price INT DEFAULT 0 NOT NULL AFTER price,
MODIFY COLUMN price INT DEFAULT 0 COMMENT 'Final price after discounts (discounted_price if provided, otherwise base_price)';

-- Add comment to the new column
ALTER TABLE orderitems MODIFY COLUMN base_price INT DEFAULT 0 NOT NULL COMMENT 'Original price before any discounts (used for promotion tracking)';

-- If you need to populate existing rows with their current price as base_price:
-- UPDATE orderitems SET base_price = price WHERE base_price = 0;
