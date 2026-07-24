-- Widens audit_logs.id, which is silently truncated by MySQL's non-strict
-- mode: helpers.GeneratePrefixedID(helpers.AuditLogIDPrefix) produces
-- "audit-log-" + uuid.New().String() = 10 + 36 = 46 characters, but the
-- column was only varchar(36) (same width as the un-prefixed UUID it was
-- presumably sized for). Same failure family as
-- docs/migration-postgres/27-tier3-conversion-log.md (ecart transverse #7)
-- and docs/migration-postgres/28-varchar-widening.md: Postgres has no
-- equivalent silent coercion (hard error 22001 instead of truncation), which
-- is what surfaced this in internal/modules/audit/repository.go
-- InsertLogWithChain. See docs/migration-postgres/53-audit-logs-column-width.md.
--
-- audit_logs.id is this table's PRIMARY KEY; widening a varchar(36) PK to
-- varchar(64) keeps the same 1-byte length-prefix storage class, so MySQL
-- performs this in place without rebuilding the index (same reasoning as
-- customer_loyalty_progress.id in migration 066).

ALTER TABLE audit_logs
  MODIFY id varchar(64) NOT NULL;
