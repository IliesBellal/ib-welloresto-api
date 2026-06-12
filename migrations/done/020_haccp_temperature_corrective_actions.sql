CREATE TABLE haccp_corrective_actions (
    id VARCHAR(64) NOT NULL,
    code VARCHAR(64) NOT NULL,
    label VARCHAR(120) NOT NULL,
    description TEXT NULL,
    severity_scope VARCHAR(32) NULL,
    active TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_haccp_corrective_actions_code (code),
    KEY idx_haccp_corrective_actions_active (active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE temperature_reading_corrective_actions (
    id VARCHAR(64) NOT NULL,
    reading_id VARCHAR(64) NOT NULL,
    action_id VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    note TEXT NULL,
    photo_url VARCHAR(512) NULL,
    follow_up_value DECIMAL(5,2) NULL,
    created_by VARCHAR(255) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_trca_reading (reading_id),
    KEY idx_trca_action (action_id),
    KEY idx_trca_merchant (merchant_id),
    CONSTRAINT fk_trca_reading FOREIGN KEY (reading_id) REFERENCES temperature_readings(id) ON DELETE CASCADE,
    CONSTRAINT fk_trca_action FOREIGN KEY (action_id) REFERENCES haccp_corrective_actions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO haccp_corrective_actions (id, code, label, description, severity_scope)
VALUES
    ('haccp-ca-adjust-thermostat', 'adjust_thermostat', 'Adjust thermostat', 'Adjust the equipment thermostat and verify the next reading.', 'both'),
    ('haccp-ca-move-product', 'move_product', 'Move product', 'Move products to compliant storage immediately.', 'both'),
    ('haccp-ca-discard-product', 'discard_product', 'Discard product', 'Discard non-compliant product according to HACCP rules.', 'critical'),
    ('haccp-ca-clean-sensor', 'clean_or_replace_sensor', 'Clean or replace sensor', 'Clean or replace the probe/sensor and retake measurement.', 'both'),
    ('haccp-ca-other', 'other', 'Other', 'Use this action when none of the predefined actions applies.', 'both');