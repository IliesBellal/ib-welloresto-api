-- Adds a structured deletion reason to delivery_session_order, alongside the existing
-- free-text fail_reason (032_delivery_module), for per-reason analytics on failed/
-- canceled delivery stops.
--
-- deletion_reason_id references deletion_reasons.deletion_reason_id - a legacy table
-- with no schema definition in this repo's migrations (same as orders.deletion_reason_id/
-- deletion_comment). deletion_reason_id is handled as a string throughout the Go
-- codebase (see internal/modules/pos/repository.go GetDeletionReasons). No FK
-- constraint added, consistent with the rest of this codebase (no FK to historical
-- tables, see 032_delivery_module.up.sql).
--
-- fail_reason is kept as-is for legacy display; deletion_reason_id/deletion_comment are
-- additional, optional columns written alongside it.
ALTER TABLE delivery_session_order ADD COLUMN deletion_reason_id VARCHAR(20) NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN deletion_comment VARCHAR(255) NULL DEFAULT NULL;
CREATE INDEX idx_delivery_session_order_deletion_reason ON delivery_session_order (deletion_reason_id);
