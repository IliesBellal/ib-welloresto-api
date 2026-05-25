DROP TABLE IF EXISTS cleaning_executions;
DROP TABLE IF EXISTS cleaning_sessions;
DROP TABLE IF EXISTS cleaning_surfaces;
DROP TABLE IF EXISTS cleaning_zones;
DROP TABLE IF EXISTS cleaning_tasks;

CREATE TABLE cleaning_zones (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    deleted_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    updated_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP() ON UPDATE UTC_TIMESTAMP(),
    INDEX idx_cleaning_zones_merchant_enabled (merchant_id, enabled),
    INDEX idx_cleaning_zones_merchant_name (merchant_id, name)
);

CREATE TABLE cleaning_surfaces (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    zone_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    frequency_unit ENUM('day', 'week', 'month') NOT NULL,
    frequency_count INT NOT NULL DEFAULT 1,
    active TINYINT(1) NOT NULL DEFAULT 1,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    deleted_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    updated_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP() ON UPDATE UTC_TIMESTAMP(),
    INDEX idx_cleaning_surfaces_merchant_enabled (merchant_id, enabled),
    INDEX idx_cleaning_surfaces_zone_enabled (zone_id, enabled),
    INDEX idx_cleaning_surfaces_merchant_zone (merchant_id, zone_id)
);

CREATE TABLE cleaning_sessions (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'done',
    created_by VARCHAR(64) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    deleted_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    updated_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP() ON UPDATE UTC_TIMESTAMP(),
    INDEX idx_cleaning_sessions_merchant_enabled (merchant_id, enabled),
    INDEX idx_cleaning_sessions_merchant_created (merchant_id, created_at)
);

CREATE TABLE cleaning_executions (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    session_id VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    surface_id VARCHAR(64) NOT NULL,
    comment TEXT NULL,
    photo_url TEXT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'done',
    created_by VARCHAR(64) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    deleted_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    updated_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP() ON UPDATE UTC_TIMESTAMP(),
    INDEX idx_cleaning_executions_session_enabled (session_id, enabled),
    INDEX idx_cleaning_executions_surface_enabled (surface_id, enabled),
    INDEX idx_cleaning_executions_merchant_created (merchant_id, created_at)
);