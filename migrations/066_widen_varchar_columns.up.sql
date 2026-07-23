-- Widens the varchar columns identified in the Tier 3 Postgres conversion
-- (docs/migration-postgres/27-tier3-conversion-log.md, ecart transverse #7)
-- as being silently truncated by MySQL's non-strict mode when Go-generated
-- prefixed IDs/tokens exceed the column width. Postgres has no equivalent
-- silent coercion (hard error 22001), so the columns must be wide enough for
-- the real generated values before the truncation workaround added in Go can
-- be removed. See docs/migration-postgres/28-varchar-widening.md for the
-- length audit behind the varchar(64) target.
--
-- users.token: paired with a companion change reducing generateToken()'s
-- output from 128 to 64 hex chars (internal/modules/users/repository.go),
-- so 64 is the exact fit, not just headroom.
--
-- customer_loyalty_progress.id is this table's PRIMARY KEY; widening a
-- varchar(50) PK to varchar(64) keeps the same 1-byte length-prefix storage
-- class, so MySQL performs this in place without rebuilding the index.

ALTER TABLE users
  MODIFY token varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL;

ALTER TABLE customer_loyalty_progress
  MODIFY id varchar(64) NOT NULL;

ALTER TABLE customer_loyalty_progress_order
  MODIFY progress_id varchar(64) NOT NULL;
