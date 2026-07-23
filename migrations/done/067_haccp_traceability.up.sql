CREATE TABLE haccp_traceability_records (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    comment TEXT NULL,
    created_by VARCHAR(64) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    deleted_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    updated_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP() ON UPDATE UTC_TIMESTAMP(),
    INDEX idx_haccp_traceability_records_merchant (merchant_id),
    INDEX idx_haccp_traceability_records_merchant_created (merchant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE haccp_traceability_photos (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    record_id VARCHAR(64) NOT NULL,
    photo_key VARCHAR(512) NOT NULL,
    position TINYINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    CONSTRAINT fk_haccp_traceability_photos_record FOREIGN KEY (record_id) REFERENCES haccp_traceability_records(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
