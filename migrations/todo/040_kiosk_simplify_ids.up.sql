-- Kiosk module (increment 5): collapse the double identifier (BIGINT
-- AUTO_INCREMENT internal id + VARCHAR(64) public_id) into a single
-- VARCHAR(64) id, generated backend-side via
-- helpers.GeneratePrefixedID(helpers.KioskIDPrefix /
-- KioskEnrollmentCodeIDPrefix / KioskDeviceTokenIDPrefix). See
-- docs/KIOSK_DECISIONS.md for the rationale (every kiosk route is already
-- protected — back-office authMiddleware or device KioskAuth — so there is
-- no public/internal distinction to preserve).
--
-- kiosk_settings is NOT touched: it never had an id column, its primary key
-- is merchant_id (one row per merchant, same pattern as scannorder_settings).

-- 1. Detach FKs that point at kiosks.id (old BIGINT) before changing types.
ALTER TABLE kiosk_device_tokens DROP FOREIGN KEY fk_device_token_kiosk;
ALTER TABLE kiosk_enrollment_codes DROP FOREIGN KEY fk_enrollment_kiosk;

-- 2. kiosk_device_tokens: collapse its own BIGINT id into a generated
--    VARCHAR(64), and repoint kiosk_id at kiosks.public_id (the value that
--    becomes kiosks.id in step 4) instead of the old BIGINT kiosks.id.
ALTER TABLE kiosk_device_tokens
    ADD COLUMN new_id VARCHAR(64) NULL DEFAULT NULL AFTER id,
    ADD COLUMN new_kiosk_id VARCHAR(64) NULL DEFAULT NULL AFTER kiosk_id;

UPDATE kiosk_device_tokens t
    JOIN kiosks k ON k.id = t.kiosk_id
    SET t.new_id = CONCAT('kiosk-dev-tkn-', UUID()),
        t.new_kiosk_id = k.public_id;

ALTER TABLE kiosk_device_tokens
    DROP PRIMARY KEY,
    DROP COLUMN id,
    DROP COLUMN kiosk_id;
ALTER TABLE kiosk_device_tokens CHANGE COLUMN new_id id VARCHAR(64) NOT NULL;
ALTER TABLE kiosk_device_tokens CHANGE COLUMN new_kiosk_id kiosk_id VARCHAR(64) NOT NULL;
ALTER TABLE kiosk_device_tokens
    ADD PRIMARY KEY (id),
    ADD KEY idx_device_kiosk (kiosk_id);

-- 3. kiosk_enrollment_codes: same pattern, kiosk_id stays nullable
--    (unlinked until a code is actually used to enroll a device).
ALTER TABLE kiosk_enrollment_codes
    ADD COLUMN new_id VARCHAR(64) NULL DEFAULT NULL AFTER id,
    ADD COLUMN new_kiosk_id VARCHAR(64) NULL DEFAULT NULL AFTER kiosk_id;

UPDATE kiosk_enrollment_codes
    SET new_id = CONCAT('kiosk-enrl-cd-', UUID());
UPDATE kiosk_enrollment_codes c
    JOIN kiosks k ON k.id = c.kiosk_id
    SET c.new_kiosk_id = k.public_id
    WHERE c.kiosk_id IS NOT NULL;

ALTER TABLE kiosk_enrollment_codes
    DROP PRIMARY KEY,
    DROP COLUMN id,
    DROP COLUMN kiosk_id;
ALTER TABLE kiosk_enrollment_codes CHANGE COLUMN new_id id VARCHAR(64) NOT NULL;
ALTER TABLE kiosk_enrollment_codes CHANGE COLUMN new_kiosk_id kiosk_id VARCHAR(64) NULL DEFAULT NULL;
ALTER TABLE kiosk_enrollment_codes ADD PRIMARY KEY (id);

-- 4. kiosks: drop the old BIGINT id, promote public_id to be the (only) id.
ALTER TABLE kiosks DROP PRIMARY KEY;
ALTER TABLE kiosks DROP INDEX idx_kiosks_public_id;
ALTER TABLE kiosks DROP COLUMN id;
ALTER TABLE kiosks CHANGE COLUMN public_id id VARCHAR(64) NOT NULL;
ALTER TABLE kiosks ADD PRIMARY KEY (id);

-- 5. Recreate the FKs, now VARCHAR(64) on both sides.
ALTER TABLE kiosk_device_tokens ADD CONSTRAINT fk_device_token_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id);
ALTER TABLE kiosk_enrollment_codes ADD CONSTRAINT fk_enrollment_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id);

-- orders.kiosk_id (VARCHAR(64), no FK, migration 038) already stores
-- kiosks.public_id values, which are exactly the new kiosks.id values after
-- step 4 — no change needed, idx_orders_kiosk stays valid as-is.
