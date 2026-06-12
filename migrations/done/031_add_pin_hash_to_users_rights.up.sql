-- PIN authentication: store HMAC-SHA256 hash of the PIN on the user↔merchant link.
-- NULL means the link has no PIN set; the unique index allows multiple NULLs (MySQL semantics).
ALTER TABLE users_rights ADD COLUMN pin_hash VARCHAR(64) NULL DEFAULT NULL;

-- Unique per (merchant, pin_hash) so two employees of the same merchant cannot share a PIN.
-- MySQL treats NULL as distinct in unique indexes, so multiple links without a PIN are allowed.
CREATE UNIQUE INDEX idx_users_rights_merchant_pin ON users_rights (merchant_id, pin_hash);
