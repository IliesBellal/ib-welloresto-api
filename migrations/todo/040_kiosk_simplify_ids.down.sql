-- Reverse of 040_kiosk_simplify_ids.up.sql: split kiosks.id back into a
-- BIGINT AUTO_INCREMENT id + VARCHAR(64) public_id, and restore the BIGINT
-- kiosk_id FK columns on kiosk_device_tokens / kiosk_enrollment_codes.

ALTER TABLE kiosk_device_tokens DROP FOREIGN KEY fk_device_token_kiosk;
ALTER TABLE kiosk_enrollment_codes DROP FOREIGN KEY fk_enrollment_kiosk;

-- 1. kiosks: split id back into BIGINT id + public_id.
ALTER TABLE kiosks DROP PRIMARY KEY;
ALTER TABLE kiosks CHANGE COLUMN id public_id VARCHAR(64) NOT NULL;
ALTER TABLE kiosks
    ADD COLUMN id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT FIRST,
    ADD PRIMARY KEY (id);
ALTER TABLE kiosks ADD UNIQUE KEY idx_kiosks_public_id (public_id);

-- 2. kiosk_device_tokens: restore BIGINT id + BIGINT kiosk_id.
ALTER TABLE kiosk_device_tokens
    ADD COLUMN old_kiosk_id BIGINT UNSIGNED NULL DEFAULT NULL AFTER kiosk_id;
UPDATE kiosk_device_tokens t
    JOIN kiosks k ON k.public_id = t.kiosk_id
    SET t.old_kiosk_id = k.id;

ALTER TABLE kiosk_device_tokens
    DROP PRIMARY KEY,
    DROP COLUMN id,
    DROP COLUMN kiosk_id;
ALTER TABLE kiosk_device_tokens
    ADD COLUMN id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT FIRST,
    ADD PRIMARY KEY (id);
ALTER TABLE kiosk_device_tokens CHANGE COLUMN old_kiosk_id kiosk_id BIGINT UNSIGNED NOT NULL;
ALTER TABLE kiosk_device_tokens ADD KEY idx_device_kiosk (kiosk_id);

-- 3. kiosk_enrollment_codes: restore BIGINT id + BIGINT kiosk_id (nullable).
ALTER TABLE kiosk_enrollment_codes
    ADD COLUMN old_kiosk_id BIGINT UNSIGNED NULL DEFAULT NULL AFTER kiosk_id;
UPDATE kiosk_enrollment_codes c
    JOIN kiosks k ON k.public_id = c.kiosk_id
    SET c.old_kiosk_id = k.id;

ALTER TABLE kiosk_enrollment_codes
    DROP PRIMARY KEY,
    DROP COLUMN id,
    DROP COLUMN kiosk_id;
ALTER TABLE kiosk_enrollment_codes
    ADD COLUMN id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT FIRST,
    ADD PRIMARY KEY (id);
ALTER TABLE kiosk_enrollment_codes CHANGE COLUMN old_kiosk_id kiosk_id BIGINT UNSIGNED NULL DEFAULT NULL;

-- 4. Recreate FKs against the restored BIGINT kiosks.id.
ALTER TABLE kiosk_device_tokens ADD CONSTRAINT fk_device_token_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id);
ALTER TABLE kiosk_enrollment_codes ADD CONSTRAINT fk_enrollment_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id);
