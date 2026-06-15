-- Normalizes delivery_session.status to the snake_case vocabulary already used by
-- delivery_session_order.status: 'active' / 'done' / 'canceled'.
--
-- Legacy values being replaced:
--   '1', 'PENDING'          -> 'active'
--   '0', 'DONE', 'FINISHED' -> 'done'
--   'CANCELED'              -> 'canceled'
-- '-1' and 'CLOSED' were never written and have no rows to migrate.
UPDATE delivery_session SET status = 'active'   WHERE status IN ('1','PENDING');
UPDATE delivery_session SET status = 'done'     WHERE status IN ('0','DONE','FINISHED');
UPDATE delivery_session SET status = 'canceled' WHERE status = 'CANCELED';
