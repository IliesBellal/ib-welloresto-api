-- Renumbered from 089_roles_schema.up.sql (RBAC lot 4, 2026-08-27): this
-- migration was originally filed as 089, colliding with an unrelated
-- 089_delivery_module_flag that already existed in migrations/done/. Both
-- were applied on staging under the old number before the collision was
-- noticed. Renumbered 089->094 (and 090->095, 091->096, 092->097, 093->098)
-- purely as a filesystem/bookkeeping fix for humans — renaming a file replays
-- nothing, there is no migration-tracking table in this project. Staging
-- already has this migration's DDL applied (it ran as 089); production has
-- never received it and will receive it under this new number, 094. If you
-- are looking for what "the other 089" was, see
-- migrations/done/089_delivery_module_flag.up.sql.
--
-- RBAC lot 1 (schema only): permission catalog, roles, role<->permission
-- links, and the two nullable pointers that will let a future lot attach a
-- user or a merchant default to a role. Strictly additive: no existing column
-- is dropped or repurposed, and internal/middleware/permissions.go keeps
-- deciding authorization exactly as before this migration — the
-- users_rights.manage_* / admin boolean columns remain the only thing the API
-- actually reads to authorize a request. See the RBAC audit report for the
-- full picture of what currently gates access.
--
-- Written for PostgreSQL (DB_DIALECT=postgres), per the ongoing MySQL ->
-- PostgreSQL migration. MySQL-specific notes, where this diverges:
--   * `text ... NOT NULL DEFAULT ''` on `description` is portable as-is.
--   * The partial unique indexes (`WHERE archived_at IS NULL` / `WHERE
--     system_key IS NOT NULL`) are Postgres partial indexes. MySQL (< 8.0.13,
--     and even 8.0+ without a generated-column workaround) has no equivalent
--     syntax — a MySQL version of this migration would need a computed/
--     generated column standing in for the partial predicate, or would have
--     to enforce the same uniqueness in application code instead.
--   * `ALTER TABLE ... ALTER COLUMN ... TYPE varchar(64)` (used below for
--     audit_logs) is Postgres syntax; MySQL's equivalent is `MODIFY ...
--     varchar(64)` (see migrations/done/069_widen_audit_logs_id.up.sql for
--     the precedent in this repo).
--
-- Naming note: `users_rights.role varchar(32)` and `employees.role` (a
-- Postgres enum) already exist and are NOT read by any authorization path
-- today (see the RBAC audit report, section 4.5) — they are left untouched.
-- The new column introduced here is deliberately named `role_id`, never
-- `role`, so it cannot be confused with either.

-- ---------------------------------------------------------------------------
-- 1. permissions — the fixed catalog of grantable actions.
-- ---------------------------------------------------------------------------
CREATE TABLE permissions (
    key           varchar(64) PRIMARY KEY,
    domain        varchar(32) NOT NULL,
    label         varchar(150) NOT NULL,
    description   text NOT NULL DEFAULT '',
    is_sensitive  boolean NOT NULL DEFAULT false,
    sort_order    integer NOT NULL DEFAULT 0,
    deprecated_at timestamptz
);

-- ---------------------------------------------------------------------------
-- 2. roles — per-merchant named bundles of permissions.
-- ---------------------------------------------------------------------------
CREATE TABLE roles (
    id          varchar(64) PRIMARY KEY,          -- role-<uuid>, app-generated (helpers.GeneratePrefixedID)
    merchant_id varchar(64) NOT NULL,              -- bare column, no FK to merchant(id): type mismatch
                                                    -- (merchant.id is integer, merchant_id is varchar
                                                    -- everywhere in application tables) and this repo does
                                                    -- not add FKs to historical tables — see migration 032.
    name        varchar(150) NOT NULL,
    description text NOT NULL DEFAULT '',
    system_key  varchar(16),                       -- 'admin' | 'staff' | NULL (custom role)
    version     integer NOT NULL DEFAULT 1,        -- optimistic locking for a future update endpoint
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT roles_system_key_check CHECK (system_key IS NULL OR system_key IN ('admin', 'staff'))
);

-- One active role per (merchant, name), case-insensitive. Archived roles are
-- excluded so a name can be reused after archiving.
CREATE UNIQUE INDEX idx_roles_merchant_name_active
    ON roles (merchant_id, lower(name))
    WHERE archived_at IS NULL;

-- At most one role per system_key per merchant (one "admin" role, one
-- "staff" role) — custom roles (system_key NULL) are unrestricted by this index.
CREATE UNIQUE INDEX idx_roles_merchant_system_key
    ON roles (merchant_id, system_key)
    WHERE system_key IS NOT NULL;

CREATE INDEX idx_roles_merchant_id ON roles (merchant_id);

-- ---------------------------------------------------------------------------
-- 3. role_permissions — many-to-many role <-> permission.
-- ---------------------------------------------------------------------------
CREATE TABLE role_permissions (
    role_id        varchar(64) NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_key varchar(64) NOT NULL REFERENCES permissions(key),
    PRIMARY KEY (role_id, permission_key)
);

-- ---------------------------------------------------------------------------
-- 4. users_rights.role_id — nullable pointer, not populated by this lot.
-- ---------------------------------------------------------------------------
-- No ON DELETE clause on purpose (defaults to RESTRICT/NO ACTION): a role
-- still worn by a users_rights row must not be deletable until every wearer
-- has been moved off it first.
ALTER TABLE users_rights ADD COLUMN role_id varchar(64) REFERENCES roles(id);
CREATE INDEX idx_users_rights_role_id ON users_rights (role_id);

-- ---------------------------------------------------------------------------
-- 5. merchant.default_role_id — role assigned to a newly linked user absent
--    any other choice. Not populated for existing merchants by this
--    migration (see migration 096).
-- ---------------------------------------------------------------------------
ALTER TABLE merchant ADD COLUMN default_role_id varchar(64) REFERENCES roles(id);

-- ---------------------------------------------------------------------------
-- 6. audit_logs column widths — fixed while touching adjacent ground.
-- ---------------------------------------------------------------------------
-- resource_id was varchar(36) (sized for a bare UUID). A role id
-- ("role-<uuid>") is 4 + 1 + 36 = 41 characters and would be silently
-- truncated by MySQL or hard-rejected by Postgres (22001) the first time a
-- role change is audited — same failure family as
-- migrations/done/069_widen_audit_logs_id.up.sql, which hit the identical
-- problem on audit_logs.id itself.
ALTER TABLE audit_logs ALTER COLUMN resource_id TYPE varchar(64);

-- user_id was varchar(36); users.user_id is varchar(64) (see
-- staging_schema_dump.sql). Widening it here so it can hold every real
-- user_id, not just the 36-character ones seen in fixtures/tests so far.
ALTER TABLE audit_logs ALTER COLUMN user_id TYPE varchar(64);
