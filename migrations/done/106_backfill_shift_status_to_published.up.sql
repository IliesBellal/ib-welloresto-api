-- Backfills existing shifts to the new draft/published model.
--
-- Must run strictly after 105_add_published_shift_status.up.sql, in its own
-- migration/transaction (see that file's header for why — using a
-- brand-new enum value in the same transaction that added it fails).
--
-- Every shift that isn't already 'draft' becomes 'published'. Rationale:
-- before this change, ALL shifts in a published week were visible to the
-- self-service team-week view regardless of their individual status
-- (ListPlanningShiftsTeamWeekView had no status filter) — so this preserves
-- existing visibility for existing data. Shifts already sitting at 'draft'
-- (the pre-existing default was 'planned', so this is expected to affect
-- few or no rows) stay 'draft' and become newly hidden from that view,
-- consistent with what "draft" is now meant to do.
UPDATE planning_shifts
SET status = 'published'
WHERE status IN ('planned', 'confirmed', 'done', 'cancelled');
