-- Kiosk module (increment 1): physical terminal registry, device auth
-- (enrollment codes + rotating refresh tokens), and per-merchant kiosk
-- settings. No menu/order endpoints in this increment — see
-- docs/KIOSK_DECISIONS.md for the full design rationale.
--
-- public_id (kiosks) is the identifier exposed to clients (KIOSK-<uuid>),
-- generated in Go via helpers.GeneratePrefixedID("KIOSK") — never the
-- internal auto-increment id.
--
-- No FK to historical tables (merchant, products, orders, subscriptions) —
-- consistent with the rest of this codebase. FK allowed between kiosk
-- tables themselves.

CREATE TABLE kiosks (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       VARCHAR(64) NOT NULL,
    merchant_id     VARCHAR(64) NOT NULL,
    name            VARCHAR(100) NOT NULL,
    location_id     VARCHAR(64) NULL DEFAULT NULL,
    status          ENUM('pending','active','inactive','revoked') NOT NULL DEFAULT 'pending',
    app_version     VARCHAR(20) NULL DEFAULT NULL,
    hardware_model  VARCHAR(100) NULL DEFAULT NULL,
    os_version      VARCHAR(50) NULL DEFAULT NULL,
    last_heartbeat_at DATETIME NULL DEFAULT NULL,
    last_ip         VARCHAR(45) NULL DEFAULT NULL,
    last_error      TEXT NULL DEFAULT NULL,
    last_error_at   DATETIME NULL DEFAULT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY idx_kiosks_public_id (public_id),
    KEY idx_kiosks_merchant (merchant_id),
    KEY idx_kiosks_status (merchant_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- One-time enrollment codes. code_hash uses the same HMAC-SHA256 + pepper
-- pattern as auth.pin (internal/utils/security/pin.go) for deterministic
-- lookup without storing the plaintext code. TTL (15 min) is enforced
-- applicatively, not in DB.
CREATE TABLE kiosk_enrollment_codes (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    merchant_id     VARCHAR(64) NOT NULL,
    code_hash       VARCHAR(64) NOT NULL,
    kiosk_id        BIGINT UNSIGNED NULL DEFAULT NULL,
    expires_at      DATETIME NOT NULL,
    used_at         DATETIME NULL DEFAULT NULL,
    created_by_user_id VARCHAR(64) NULL DEFAULT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY idx_enrollment_code_hash (code_hash),
    KEY idx_enrollment_merchant (merchant_id),
    CONSTRAINT fk_enrollment_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Kiosk device refresh tokens. Rotation pattern (not the opaque permanent
-- token used for human users in users_rights.token) — a device is a
-- different risk profile than a human (public physical access, must be
-- revocable instantly). expires_at is set 30 days out at issuance.
CREATE TABLE kiosk_device_tokens (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    kiosk_id        BIGINT UNSIGNED NOT NULL,
    token_hash      VARCHAR(64) NOT NULL,
    expires_at      DATETIME NOT NULL,
    revoked_at      DATETIME NULL DEFAULT NULL,
    last_used_at    DATETIME NULL DEFAULT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY idx_device_token_hash (token_hash),
    KEY idx_device_kiosk (kiosk_id),
    CONSTRAINT fk_device_token_kiosk FOREIGN KEY (kiosk_id) REFERENCES kiosks(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Per-merchant kiosk configuration, one row per merchant (like
-- scannorder_settings / merchant_parameters).
CREATE TABLE kiosk_settings (
    merchant_id                 VARCHAR(64) NOT NULL,
    fulfillment_dine_in         BOOLEAN NOT NULL DEFAULT TRUE,
    fulfillment_take_away       BOOLEAN NOT NULL DEFAULT TRUE,
    force_fulfillment_type      VARCHAR(20) NULL DEFAULT NULL,
    pager_number_required       BOOLEAN NOT NULL DEFAULT FALSE,
    show_allergens               BOOLEAN NOT NULL DEFAULT TRUE,
    inactivity_timeout_sec       INT NOT NULL DEFAULT 90,
    upsell_enabled               BOOLEAN NOT NULL DEFAULT TRUE,
    pay_at_counter_enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    card_payment_enabled        BOOLEAN NOT NULL DEFAULT FALSE,
    logo_url                     VARCHAR(500) NULL DEFAULT NULL,
    idle_image_url               VARCHAR(500) NULL DEFAULT NULL,
    primary_color                VARCHAR(7) NULL DEFAULT NULL,
    created_at                   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                   DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
