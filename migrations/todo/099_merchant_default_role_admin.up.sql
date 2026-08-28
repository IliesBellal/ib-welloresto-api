-- RBAC lot 4: repoints merchant.default_role_id at each establishment's
-- "admin" role. Direct consequence of the product decision behind this lot —
-- every account becomes Administrateur while permissions are not yet
-- exploited from any screen, so a newly linked user must land on the same
-- footing as everyone else. Until lot 4, cmd/seed_system_roles and
-- POSService.CreateMerchant pointed default_role_id at "staff" instead; both
-- have been updated in the same change to point at "admin" going forward —
-- this migration is the one-time correction for merchants that were already
-- seeded under the old default.
--
-- Written for PostgreSQL (DB_DIALECT=postgres), same as the rest of the RBAC
-- series (094-098). UPDATE ... FROM is Postgres syntax; the MySQL equivalent
-- is UPDATE ... JOIN, which is why cmd/assign_admin_role (a different,
-- per-user backfill — see that command's doc comment) is a Go program
-- instead of raw SQL. This migration only touches one row per merchant, so a
-- single-dialect statement is enough here.
--
-- Idempotent: for a merchant already pointing at its admin role, this UPDATE
-- rewrites default_role_id to the same value it already holds — a no-op in
-- effect. An establishment with no "admin" role yet (cmd/seed_system_roles
-- has not run for it) matches no row in the FROM clause and is left
-- untouched, exactly like cmd/assign_admin_role's own "no admin role, skip
-- and report" rule — never created on the fly here.
UPDATE merchant
SET default_role_id = r.id
FROM roles r
WHERE r.merchant_id = CAST(merchant.id AS TEXT)
  AND r.system_key = 'admin';
