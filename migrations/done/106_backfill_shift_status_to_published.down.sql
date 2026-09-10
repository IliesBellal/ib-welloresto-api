-- Reverts 106_backfill_shift_status_to_published.up.sql.
--
-- Cannot restore the original sub-status (planned vs confirmed vs done vs
-- cancelled) — that information is gone the moment the up migration runs,
-- same irrecoverable-data caveat as other UPDATE/DELETE migrations in this
-- set (e.g. migration 100's down). Rows land on 'planned', the pre-existing
-- default, rather than staying 'published'.
UPDATE planning_shifts
SET status = 'planned'
WHERE status = 'published';
