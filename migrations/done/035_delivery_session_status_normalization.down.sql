-- Best-effort inverse of 035_delivery_session_status_normalization.up.sql.
-- Imperfect: the 'done' bucket merges what used to be distinguished as
-- '0' / 'DONE' / 'FINISHED' - they all revert to 'DONE'. Acceptable for test data.
UPDATE delivery_session SET status = 'PENDING'  WHERE status = 'active';
UPDATE delivery_session SET status = 'DONE'     WHERE status = 'done';
UPDATE delivery_session SET status = 'CANCELED' WHERE status = 'canceled';
