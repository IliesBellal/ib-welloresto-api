ALTER TABLE orders
  DROP COLUMN IF EXISTS expected_tender_amount,
  DROP COLUMN IF EXISTS expected_mop;
