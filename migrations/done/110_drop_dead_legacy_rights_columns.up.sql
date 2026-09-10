-- Cleanup: drops 5 columns on users_rights identified as dead during a
-- back-office rights-catalog review session (2026-09-01) — see
-- docs/decisions.md for the full audit.
--
-- - `access_wrdelivery` / `access_wrwaiter` : fed only a legacy, RBAC-
--   independent "app access" flag (LoginAccessResponse.Apps.Delivery/Waiter
--   in the Go API). Neither ever gated an authorization check via
--   permission.Key (access_wrwaiter's RBAC fallback for pos.access was
--   itself removed in migration 100), and neither is read by any
--   wello-back-office UI decision — grepped and confirmed dead in that repo.
--   The corresponding Flutter getter (`capabilities.apps.waiter` in
--   wello_resto_flutter) is itself `@Deprecated` and has no live caller.
--   `access_wrreception` is NOT touched here: it still backs
--   pos.status.manage's legacy fallback (see
--   internal/modules/auth/permissions.go) and gates PATCH /pos/status for
--   accounts with no role_id yet.
-- - `export_reports` / `export_financials` / `export_customers` : never had
--   an RBAC catalog key (permission.go documents them as "deliberately
--   absent" from legacyPermissionFallback since RBAC lot 1/2 — reading a
--   report and exporting it were never split into separate grantable
--   rights). Round-tripped through the legacy login response
--   (LoginCapabilityActionsResponse.ExportReports/Financials/Customers) and
--   the back-office's own MerchantUserPermissions/RightsTab-era admin CRUD,
--   but never consumed by any gating decision — the back-office's rights
--   editor (RightsTab) was already replaced by a role-selector UI
--   (AccessTab.tsx) before this cleanup, and the parsed login-response
--   mirror of these flags (AuthCapabilities.actions) was confirmed unread
--   anywhere in that repo.
--
-- Postgres syntax + defensive to_regclass/IF EXISTS guard, same convention
-- as migration 104 (this schema went through a MySQL -> Postgres
-- conversion; the guard lets this file replay safely regardless of
-- environment state).

DO $$
BEGIN
  IF to_regclass('public.users_rights') IS NOT NULL THEN
    ALTER TABLE users_rights
      DROP COLUMN IF EXISTS access_wrdelivery,
      DROP COLUMN IF EXISTS access_wrwaiter,
      DROP COLUMN IF EXISTS export_reports,
      DROP COLUMN IF EXISTS export_financials,
      DROP COLUMN IF EXISTS export_customers;
  END IF;
END
$$;
