-- LOT B B1d (docs/decisions.md) : subscription_overrides — commercial
-- dérogations, internal-only (never exposed to the client, see
-- middleware.RequirePlatformAdmin / users.is_platform_staff, migration 138).
--
-- id prefix "sbov" via helpers.GeneratePrefixedID (same "prefix-<uuid>"
-- convention as every other id in this repo, see 137_subscription_items.up.sql's
-- comment on the sbit_ vs sbit- discrepancy — not repeated here).
--
-- kind ('module' | 'price' | 'kiosk_quota') and reason ('commercial' | 'test'
-- | 'partenaire' | 'migration' | 'geste') : no CHECK constraint, same
-- convention as subscription_items.kind/code — validated applicatively by
-- subscriptions.ValidOverrideKinds/ValidOverrideReasons.
--
-- `target` is deliberately untyped (text) because its meaning depends on
-- `kind` — the brief's schema has no separate "value" column, so target
-- carries the payload for all three:
--   kind='module'      -> target is a subscriptions_items-style module code
--                          (reservation|haccp|planning|marketplaces|delivery|
--                          kiosk) naming which subscriptions.*_enabled column
--                          to flip true. See subscriptions.moduleOverrideColumn
--                          for the exact code->column mapping — it does NOT
--                          cover every code in subscriptions.ValidCodes (no
--                          subscriptions column exists yet for "marketplaces";
--                          "kiosk"/"sms" are additionally blocked by the P3
--                          guard below regardless). Unmapped/blocked targets
--                          are rejected at write time, not silently ignored.
--   kind='price'        -> target is the override_price_cents value to write
--                          on subscriptions (an integer, as text).
--   kind='kiosk_quota'  -> target is the new subscriptions.max_kiosks value
--                          (an integer, as text).
-- Documented here as an explicit interpretation of an ambiguous brief detail
-- (see docs/decisions.md) — flag if wrong rather than assume it's settled.
CREATE TABLE subscription_overrides (
    id          text PRIMARY KEY,
    merchant_id text NOT NULL,
    kind        text NOT NULL,
    target      text NOT NULL,
    reason      text NOT NULL,
    note        text,
    created_by  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    revoked_at  timestamptz
);
COMMENT ON COLUMN subscription_overrides.kind IS 'module | price | kiosk_quota — non contraint en base, voir subscriptions.ValidOverrideKinds côté Go.';
COMMENT ON COLUMN subscription_overrides.target IS 'Payload whose shape depends on kind — see the table-level comment in this migration for the full mapping.';
COMMENT ON COLUMN subscription_overrides.reason IS 'commercial | test | partenaire | migration | geste — non contraint en base, voir subscriptions.ValidOverrideReasons côté Go.';
COMMENT ON COLUMN subscription_overrides.created_by IS 'users.user_id of the platform staff member who created this dérogation — see users.is_platform_staff (migration 138).';
COMMENT ON COLUMN subscription_overrides.revoked_at IS 'NULL = active. Non-NULL = revoked via DELETE /v1/admin/overrides/{id} — the row is kept, never deleted, for audit history.';
COMMENT ON COLUMN subscription_overrides.expires_at IS 'NULL = no automatic expiry. Non-NULL and in the past means the override is no longer in effect even if revoked_at is still NULL — GET /v1/admin/overrides and the enforcement checks both.';

CREATE INDEX idx_subscription_overrides_merchant_id ON subscription_overrides (merchant_id);

-- "toutes les actives, triées par ancienneté" (GET /v1/admin/overrides) reads
-- every non-revoked, non-expired row ordered by created_at ASC — this index
-- serves both that filter and the ordering.
CREATE INDEX idx_subscription_overrides_active_created_at ON subscription_overrides (created_at) WHERE revoked_at IS NULL;
