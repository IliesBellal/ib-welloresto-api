-- Reverts 097_permission_pos_status_manage.up.sql.
--
-- Run only after every role_permissions row granting this key has been
-- removed (e.g. by re-running an older roles.Repository against a role that
-- no longer includes it) — otherwise this DELETE fails on the
-- role_permissions.permission_key FK, exactly as documented for migration 095's
-- down.
DELETE FROM permissions WHERE key = 'pos.status.manage';
