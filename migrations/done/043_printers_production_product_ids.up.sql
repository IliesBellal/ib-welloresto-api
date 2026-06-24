-- Per-printer production filter: list of product_id values (JSON array)
-- that this production printer should print. NULL or empty array means
-- no filter (print everything), preserving current behavior for existing
-- printers. Lets a restaurant route e.g. kitchen products to one printer
-- and bar products to another.
ALTER TABLE printers ADD COLUMN production_product_ids TEXT NULL DEFAULT NULL
    AFTER enabled;
