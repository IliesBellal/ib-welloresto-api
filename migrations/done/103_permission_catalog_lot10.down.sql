-- Reverts 103_permission_catalog_lot10.up.sql.
--
-- Only the 5 new keys are removed (role_permissions rows referencing them
-- purged first, same FK reasoning as migration 100's down). The
-- `description` backfill on the 13 pre-existing keys is NOT reverted —
-- restoring them to an empty string on rollback would be a pure
-- regression, not a meaningful "undo".

DELETE FROM role_permissions WHERE permission_key IN ('pos.analytics', 'bookings.manage', 'platforms.manage', 'kiosk.manage', 'seating_plan.manage');

DELETE FROM permissions WHERE key IN ('pos.analytics', 'bookings.manage', 'platforms.manage', 'kiosk.manage', 'seating_plan.manage');
