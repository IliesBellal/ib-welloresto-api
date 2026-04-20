-- Migration: Create Availabilities Module Tables
-- Purpose: Implement schedule-based product availability management
-- Created: 2026-04-20
-- Description: Allows restricting product purchases to specific days and time slots
--              (e.g., "Breakfast" available 8am-11am, "Lunch" 12pm-2pm)

-- Main availabilities table: Metadata container
CREATE TABLE IF NOT EXISTS availabilities (
    availability_id CHAR(36) PRIMARY KEY,
    merchant_id CHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL COMMENT 'Availability name (e.g., "Breakfast", "Lunch")',
    description TEXT COMMENT 'Optional description of the availability',
    enabled INT DEFAULT 1 COMMENT 'Soft delete flag (0 = disabled/deleted)',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    INDEX idx_merchant_id (merchant_id),
    INDEX idx_enabled (enabled),
    INDEX idx_created_at (created_at),
    
    CONSTRAINT fk_availabilities_merchant 
        FOREIGN KEY (merchant_id) REFERENCES merchants(merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Time-based product availability definitions';

-- Junction table: Links products to availabilities (many-to-many)
CREATE TABLE IF NOT EXISTS availabilities_products (
    availability_product_id CHAR(36) PRIMARY KEY,
    availability_id CHAR(36) NOT NULL,
    product_id CHAR(36) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    UNIQUE KEY uq_availability_product (availability_id, product_id),
    INDEX idx_product_id (product_id),
    INDEX idx_availability_id (availability_id),
    
    CONSTRAINT fk_availabilities_products_availability
        FOREIGN KEY (availability_id) REFERENCES availabilities(availability_id) ON DELETE CASCADE,
    CONSTRAINT fk_availabilities_products_product
        FOREIGN KEY (product_id) REFERENCES products(product_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Maps products to their availabilities (many-to-many relationship)';

-- Time schedules table: Defines specific time slots for each availability
CREATE TABLE IF NOT EXISTS availabilities_schedules (
    schedule_id CHAR(36) PRIMARY KEY,
    availability_id CHAR(36) NOT NULL,
    day_of_week INT NOT NULL COMMENT '1=Sunday, 2=Monday, ..., 7=Saturday',
    start_time TIME NOT NULL COMMENT 'Start time (HH:MM:SS)',
    end_time TIME NOT NULL COMMENT 'End time (HH:MM:SS)',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    INDEX idx_availability_id (availability_id),
    INDEX idx_day_of_week (day_of_week),
    INDEX idx_time_range (start_time, end_time),
    
    CONSTRAINT fk_availabilities_schedules_availability
        FOREIGN KEY (availability_id) REFERENCES availabilities(availability_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Time schedules for each availability (specific days and time ranges)';
