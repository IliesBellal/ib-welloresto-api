DROP TABLE IF EXISTS average_delivery_time;

ALTER TABLE orders
  DROP COLUMN delivery_travel_seconds;
