CREATE TABLE IF NOT EXISTS labor_rules (
  country_code CHAR(2) NOT NULL,
  label VARCHAR(120) NOT NULL,
  min_daily_rest_hours DECIMAL(4,2) NOT NULL DEFAULT 11.00,
  min_break_minutes INT NOT NULL DEFAULT 45,
  night_shift_start TIME NOT NULL DEFAULT '22:00:00',
  night_shift_end TIME NOT NULL DEFAULT '06:00:00',
  night_shift_multiplier DECIMAL(4,2) NOT NULL DEFAULT 1.25,
  holiday_multiplier DECIMAL(4,2) NOT NULL DEFAULT 2.00,
  max_weekly_hours DECIMAL(5,2) NOT NULL DEFAULT 48.00,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (country_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO labor_rules (country_code, label, min_daily_rest_hours, min_break_minutes, night_shift_start, night_shift_end, night_shift_multiplier, holiday_multiplier, max_weekly_hours)
VALUES ('FR', 'France', 11.00, 45, '22:00:00', '06:00:00', 1.25, 2.00, 48.00)
ON DUPLICATE KEY UPDATE label = VALUES(label), min_daily_rest_hours = VALUES(min_daily_rest_hours), min_break_minutes = VALUES(min_break_minutes), night_shift_start = VALUES(night_shift_start), night_shift_end = VALUES(night_shift_end), night_shift_multiplier = VALUES(night_shift_multiplier), holiday_multiplier = VALUES(holiday_multiplier), max_weekly_hours = VALUES(max_weekly_hours);

CREATE TABLE IF NOT EXISTS holiday_calendar (
  id VARCHAR(64) NOT NULL,
  country_code CHAR(2) NOT NULL,
  holiday_date DATE NOT NULL,
  label VARCHAR(150) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_holiday_calendar_country_date (country_code, holiday_date),
  KEY idx_holiday_calendar_country (country_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS sys_contract_types (
  code VARCHAR(32) NOT NULL,
  label VARCHAR(80) NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  active TINYINT(1) NOT NULL DEFAULT 1,
  PRIMARY KEY (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO sys_contract_types (code, label, sort_order, active) VALUES
('CDI', 'CDI', 10, 1),
('CDD', 'CDD', 20, 1),
('Extra', 'Extra', 30, 1),
('Stage', 'Stage', 40, 1),
('Apprenti', 'Apprenti', 50, 1)
ON DUPLICATE KEY UPDATE label = VALUES(label), sort_order = VALUES(sort_order), active = VALUES(active);

CREATE TABLE IF NOT EXISTS sys_time_tracking_modes (
  code VARCHAR(32) NOT NULL,
  label VARCHAR(80) NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  active TINYINT(1) NOT NULL DEFAULT 1,
  PRIMARY KEY (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO sys_time_tracking_modes (code, label, sort_order, active) VALUES
('standard', 'Standard', 10, 1),
('badge', 'Badgeuse', 20, 1),
('mobile', 'Mobile', 30, 1),
('kiosk', 'Borne', 40, 1)
ON DUPLICATE KEY UPDATE label = VALUES(label), sort_order = VALUES(sort_order), active = VALUES(active);

CREATE TABLE IF NOT EXISTS sys_planning_event_types (
  code VARCHAR(32) NOT NULL,
  label VARCHAR(80) NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  active TINYINT(1) NOT NULL DEFAULT 1,
  PRIMARY KEY (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO sys_planning_event_types (code, label, sort_order, active) VALUES
('holiday', 'Jour férié', 10, 1),
('custom', 'Personnalisé', 20, 1),
('leave', 'Congé', 30, 1)
ON DUPLICATE KEY UPDATE label = VALUES(label), sort_order = VALUES(sort_order), active = VALUES(active);

CREATE TABLE IF NOT EXISTS planning_settings (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  labor_country_code CHAR(2) NOT NULL DEFAULT 'FR',
  min_daily_rest_hours DECIMAL(4,2) NOT NULL DEFAULT 11.00,
  min_break_minutes INT NOT NULL DEFAULT 45,
  night_shift_start TIME NOT NULL DEFAULT '22:00:00',
  night_shift_end TIME NOT NULL DEFAULT '06:00:00',
  night_shift_multiplier DECIMAL(4,2) NOT NULL DEFAULT 1.25,
  holiday_multiplier DECIMAL(4,2) NOT NULL DEFAULT 2.00,
  allow_override_warnings TINYINT(1) NOT NULL DEFAULT 1,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_planning_settings_merchant (merchant_id),
  KEY idx_planning_settings_merchant (merchant_id),
  KEY idx_planning_settings_labor_country_code (labor_country_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS employees (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NULL,
  first_name VARCHAR(150) NOT NULL,
  last_name VARCHAR(150) NOT NULL,
  position VARCHAR(150) NOT NULL,
  job_title VARCHAR(150) NULL,
  email VARCHAR(255) NULL,
  phone VARCHAR(64) NULL,
  role ENUM('employee','manager','admin') NOT NULL DEFAULT 'employee',
  contract_type_code VARCHAR(32) NOT NULL,
  contract_start_date DATE NULL,
  contract_end_date DATE NULL,
  probation_end_date DATE NULL,
  last_medical_checkup_date DATE NULL,
  contract_hours DECIMAL(5,2) NOT NULL DEFAULT 35.00,
  max_weekly_hours DECIMAL(5,2) NOT NULL DEFAULT 35.00,
  required_rest_days INT NOT NULL DEFAULT 2,
  sunday_premium TINYINT(1) NOT NULL DEFAULT 0,
  night_premium TINYINT(1) NOT NULL DEFAULT 0,
  time_tracking_mode_code VARCHAR(32) NOT NULL DEFAULT 'standard',
  hourly_rate BIGINT NOT NULL DEFAULT 0,
  gross_monthly_salary BIGINT NOT NULL DEFAULT 0,
  employer_charges_pct DECIMAL(5,2) NOT NULL DEFAULT 45.00,
  transport_cost BIGINT NOT NULL DEFAULT 0,
  birth_date DATE NULL,
  gender VARCHAR(32) NULL,
  nationality VARCHAR(80) NULL,
  address VARCHAR(255) NULL,
  hr_comment TEXT NULL,
  active TINYINT(1) NOT NULL DEFAULT 1,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_employees_merchant_user (merchant_id, user_id),
  KEY idx_employees_merchant_active (merchant_id, active),
  KEY idx_employees_merchant (merchant_id),
  KEY idx_employees_contract_type (contract_type_code),
  KEY idx_employees_time_tracking (time_tracking_mode_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS hours_amendments (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  employee_id VARCHAR(64) NOT NULL,
  type ENUM('permanent','temporary') NOT NULL DEFAULT 'permanent',
  start_date DATE NOT NULL,
  end_date DATE NULL,
  new_hours_volume DECIMAL(5,2) NOT NULL,
  created_by VARCHAR(255) NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_hours_amendments_merchant (merchant_id),
  KEY idx_hours_amendments_employee (employee_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;