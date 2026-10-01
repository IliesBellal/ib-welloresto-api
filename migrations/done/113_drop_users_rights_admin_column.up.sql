-- RBAC lot 11, phase 4 — drops the legacy users_rights.admin boolean column.
--
-- PREPARED, NOT MEANT TO BE APPLIED YET. This column is a different thing
-- from the "admin" ROLE (roles.system_key = 'admin') despite sharing the
-- word — see docs/decisions.md, RBAC lot 11 phase 4. Dropping it is safe
-- only once no deployed code reads users_rights.admin at all. As of this
-- migration's authoring date, that is NOT yet true:
--   - internal/modules/auth/permissions.go's Has(), historical branch
--     (RoleID == nil): `if u.Rights.Admin { return true }` — kept
--     deliberately in this chantier (RBAC lot 11 phase 5), it is what
--     production runs on today since the RBAC migrations (094+) have never
--     been applied there (see docs/RBAC_BASCULE.md).
--   - UserLoginRow.HasAdminRole()'s RoleID == nil branch (display: login's
--     `admin` flag, GET /me/permissions's `is_admin`).
--   - LoginLegacyFields.Admin (deprecated compatibility payload).
-- RBAC lot 12 removed the other two readers that used to be listed here,
-- UserLoginRow.HasAccessReception() and CanPrintCashReport() (both read
-- Rights.Admin directly, display-only) — see docs/decisions.md. They are no
-- longer a blocker for this migration; the two above still are.
-- This migration can only run after a version of the API that drops every
-- one of those readers has been deployed everywhere that matters —
-- production included. See docs/RBAC_DEPLOIEMENT_PROD.md for the ordered
-- rollout this depends on, and docs/decisions.md's RBAC lot 11 phase 5 entry
-- for the exact condition ("tous les liens portant un role_id, en
-- production comprise") that retires the historical fallback branch this
-- column still feeds.
--
-- Postgres syntax + defensive to_regclass/IF EXISTS guard, same convention
-- as migrations 104/110.
--
-- ============================================================================
-- NEUTRALIZED (2026-09-10, LOT A Semaine 2 prerequisite check, docs/decisions.md).
-- This file was accidentally swept from migrations/todo/ into migrations/done/
-- by commit c113a46 ("onboarding LOT A") alongside migrations that genuinely
-- were applied — despite every blocking condition above still holding
-- (verified directly against staging: users_rights.admin still exists, the
-- three readers above are still live in deployed code). Moved back to
-- migrations/todo/, and the DROP COLUMN below replaced with a no-op: even a
-- blind "run every file in todo/ in order" can no longer break production.
-- Restore the commented-out body verbatim once every reader above is
-- confirmed gone from deployed code, production included.
-- ============================================================================

DO $$
BEGIN
  RAISE NOTICE '113_drop_users_rights_admin_column is neutralized (blocked) — see this file''s header. No-op, users_rights.admin left untouched.';
END
$$;

-- Original body, preserved for restoration once actually safe:
--
-- DO $$
-- BEGIN
--   IF to_regclass('public.users_rights') IS NOT NULL THEN
--     ALTER TABLE users_rights
--       DROP COLUMN IF EXISTS admin;
--   END IF;
-- END
-- $$;
