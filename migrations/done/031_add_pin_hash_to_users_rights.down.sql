DROP INDEX idx_users_rights_merchant_pin ON users_rights;
ALTER TABLE users_rights DROP COLUMN pin_hash;
