-- Reverts 099_merchant_default_role_admin.up.sql.
--
-- Best-effort, like 096's down: repoints default_role_id back at each
-- merchant's "staff" role. This is an exact revert only for merchants that
-- were provisioned before this lot (their default_role_id was "staff" prior
-- to the up migration, by construction of the old cmd/seed_system_roles /
-- POSService.CreateMerchant). A merchant created AFTER this lot ships never
-- had a "staff" default in the first place — its default_role_id was set
-- directly to "admin" at creation time — so running this down migration
-- against it repoints a value this migration never set. Acceptable for the
-- same reason 096's down accepts the same limitation: there is no stored
-- history of what default_role_id held before the up migration ran.
UPDATE merchant
SET default_role_id = r.id
FROM roles r
WHERE r.merchant_id = CAST(merchant.id AS TEXT)
  AND r.system_key = 'staff';
