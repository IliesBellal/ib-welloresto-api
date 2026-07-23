-- Reverts 066_widen_varchar_columns.up.sql.
-- WARNING: only safe to run if no stored value exceeds the narrower width
-- again (see the verification query in docs/migration-postgres/28-varchar-widening.md) —
-- otherwise this MODIFY will either truncate data (non-strict mode) or fail
-- outright (strict mode).

ALTER TABLE customer_loyalty_progress_order
  MODIFY progress_id varchar(30) NOT NULL;

ALTER TABLE customer_loyalty_progress
  MODIFY id varchar(50) NOT NULL;

ALTER TABLE users
  MODIFY token varchar(30) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL;
