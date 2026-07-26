-- Reverts 071_users_enabled_boolean.up.sql.

ALTER TABLE users
  MODIFY enabled int(11) NOT NULL DEFAULT 1;
