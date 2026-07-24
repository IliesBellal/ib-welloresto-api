-- Reverts 069_widen_audit_logs_id.up.sql.
-- WARNING: only safe to run if no stored id exceeds 36 characters (see the
-- verification query in docs/migration-postgres/53-audit-logs-column-width.md)
-- — otherwise this MODIFY will either truncate data (non-strict mode) or
-- fail outright (strict mode). Since every id is generated with the fixed
-- "audit-log-" + uuid format (46 chars), reverting will truncate every
-- existing row's id back down and is very likely to produce PRIMARY KEY
-- collisions (1062) across rows sharing the same first-36-character prefix.

ALTER TABLE audit_logs
  MODIFY id varchar(36) NOT NULL;
