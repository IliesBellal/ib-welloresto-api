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
--   - UserLoginRow.HasAccessReception() / CanPrintCashReport() (display,
--     login capabilities).
--   - LoginLegacyFields.Admin (deprecated compatibility payload).
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

DO $$
BEGIN
  IF to_regclass('public.users_rights') IS NOT NULL THEN
    ALTER TABLE users_rights
      DROP COLUMN IF EXISTS admin;
  END IF;
END
$$;
