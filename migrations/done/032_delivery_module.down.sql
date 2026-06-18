ALTER TABLE customer DROP COLUMN delivery_notes;

DROP TABLE delivery_position;

ALTER TABLE users DROP COLUMN last_position_at;

DROP INDEX idx_delivery_session_current_order ON delivery_session;
ALTER TABLE delivery_session DROP COLUMN current_order_id;

ALTER TABLE delivery_session_order DROP COLUMN status;
ALTER TABLE delivery_session_order DROP COLUMN arrived_at;
ALTER TABLE delivery_session_order DROP COLUMN delivered_at;
ALTER TABLE delivery_session_order DROP COLUMN failed_at;
ALTER TABLE delivery_session_order DROP COLUMN canceled_at;
ALTER TABLE delivery_session_order DROP COLUMN fail_reason;
