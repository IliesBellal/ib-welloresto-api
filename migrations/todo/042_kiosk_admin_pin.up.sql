-- Kiosk admin PIN: per-device PIN used to unlock the admin/settings screen on
-- the kiosk itself, and consultable from the POS (back-office). Stored
-- encrypted (AES-256-GCM, internal/helpers/encryption.go), not hashed —
-- unlike auth.pin_hash, this PIN must be decryptable to be redisplayed to
-- staff. See docs/KIOSK_DECISIONS.md for the hash-vs-encrypt rationale.
ALTER TABLE kiosks ADD COLUMN admin_pin_encrypted VARBINARY(255) NULL DEFAULT NULL
    AFTER hardware_model;
