-- PROMPT 11 §4 — data cleanup for orders.deletion_reason_id.
--
-- NOT APPLIED as part of this lot (explicit instruction — prepared only).
-- The live write path is already clean: verified against staging that zero
-- orders written after 2026-02-18 carry a quoted deletion_reason_id, including
-- recent cancellations from the same staff account that produced most of the
-- 212 historical offenders (Sep 2025-Feb 2026). This migration only strips the
-- stray literal quotes left on those 212 historical rows — same trim pattern
-- already used by 114_write_path_instrumentation's own cancelled_by_type
-- backfill, applied here directly to the source column instead of only at
-- read time. Idempotent (WHERE clause only matches still-quoted values).
--
-- Does not attempt to recover the historical truncation (KIOSK_CUSTO,
-- SNO_CUSTOME, varchar(11)-truncated before this lot's 116 migration widened
-- the column): the original untruncated value was never stored, so there is
-- nothing to restore — those rows stay as-is, same as lot 1's own decision.

UPDATE orders
SET deletion_reason_id = trim(both '''' from deletion_reason_id)
WHERE deletion_reason_id LIKE '''%''';
