-- Reverts 094_roles_schema.up.sql, in reverse order.
--
-- WARNING (audit_logs width revert): only safe if no stored resource_id or
-- user_id exceeds 36 characters. Once migration 095/096 or any role-change
-- audit entry has run, resource_id will contain "role-<uuid>" (41 chars) —
-- reverting after that point truncates data (non-strict MySQL) or fails
-- outright (Postgres 22001), exactly as documented for audit_logs.id in
-- migrations/done/069_widen_audit_logs_id.down.sql.

ALTER TABLE audit_logs ALTER COLUMN user_id TYPE varchar(36);
ALTER TABLE audit_logs ALTER COLUMN resource_id TYPE varchar(36);

ALTER TABLE merchant DROP COLUMN default_role_id;

DROP INDEX IF EXISTS idx_users_rights_role_id;
ALTER TABLE users_rights DROP COLUMN role_id;

DROP TABLE role_permissions;

DROP INDEX IF EXISTS idx_roles_merchant_id;
DROP INDEX IF EXISTS idx_roles_merchant_system_key;
DROP INDEX IF EXISTS idx_roles_merchant_name_active;
DROP TABLE roles;

DROP TABLE permissions;
