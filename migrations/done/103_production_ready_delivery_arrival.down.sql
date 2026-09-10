ALTER TABLE orders
  ADD COLUMN dateCall DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE orders
  DROP COLUMN production_ready_at,
  DROP COLUMN delivery_arrival_at;
