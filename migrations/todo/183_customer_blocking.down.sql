-- Annule 183_customer_blocking.up.sql. Les libellés sont supprimés AVANT les
-- motifs (ils sont retrouvés par les IDs des motifs 'customer').
DELETE FROM labels
WHERE label_type = 'deletion_reason'
  AND label_value IN (
      SELECT CAST(deletion_reason_id AS varchar(20))
      FROM deletion_reasons
      WHERE deletion_reason_object = 'customer'
  );

DELETE FROM deletion_reasons WHERE deletion_reason_object = 'customer';

DROP INDEX IF EXISTS idx_customer_blocked_tel;

ALTER TABLE customer
    DROP COLUMN IF EXISTS blocked_comment,
    DROP COLUMN IF EXISTS blocked_reason_id,
    DROP COLUMN IF EXISTS blocked_by,
    DROP COLUMN IF EXISTS blocked_at;
