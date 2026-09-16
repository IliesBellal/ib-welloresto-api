-- Reverts 146_kiosks_terminal_reader.up.sql.
DROP INDEX IF EXISTS uq_kiosks_stripe_reader_id;
ALTER TABLE kiosks
    DROP COLUMN IF EXISTS stripe_reader_id,
    DROP COLUMN IF EXISTS stripe_reader_label,
    DROP COLUMN IF EXISTS stripe_reader_serial;
ALTER TABLE stripe_payments
    DROP COLUMN IF EXISTS kiosk_id,
    DROP COLUMN IF EXISTS card_brand,
    DROP COLUMN IF EXISTS card_last4,
    DROP COLUMN IF EXISTS card_application_preferred_name,
    DROP COLUMN IF EXISTS card_dedicated_file_name,
    DROP COLUMN IF EXISTS card_authorization_code;
