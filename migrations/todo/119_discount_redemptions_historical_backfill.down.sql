-- Reverts 119_discount_redemptions_historical_backfill.up.sql.
-- Removes only rows this migration could have inserted (is_reconstructed),
-- never a row written since by the live application code path.

DELETE FROM discount_redemptions WHERE is_reconstructed = true;
