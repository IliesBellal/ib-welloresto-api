-- LOT B PRÉALABLE / B1d (docs/decisions.md) : chantier B1d needs a real
-- "WelloResto internal staff" permission, distinct from every merchant-scoped
-- RBAC right (settings.manage included — the brief is explicit that B1d's
-- endpoints must NOT be gated by it). No such concept existed anywhere in
-- this codebase (the only precedent, /admin/upsell, has a TODO literally
-- waiting on a "middleware admin dédié"). Chosen mechanism (checked with the
-- user rather than assumed): a plain boolean column on users, checked by
-- middleware.RequirePlatformAdmin — simplest thing that still gives
-- subscription_overrides.created_by a real, attributable user_id, unlike a
-- shared-secret header.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_platform_staff boolean NOT NULL DEFAULT false;

-- No rows are flipped true by this migration — seeding the first staff
-- account(s) is a deliberate follow-up (see docs/decisions.md), not something
-- to guess at here.
