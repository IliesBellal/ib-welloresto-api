CREATE TABLE IF NOT EXISTS planning_weeks (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  label VARCHAR(150) NULL,
  start_date DATE NOT NULL,
  end_date DATE NOT NULL,
  status ENUM('draft','published','locked') NOT NULL DEFAULT 'draft',
  notes TEXT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_planning_weeks_merchant_start (merchant_id, start_date, enabled),
  KEY idx_planning_weeks_merchant (merchant_id),
  KEY idx_planning_weeks_range (start_date, end_date),
  CONSTRAINT fk_planning_weeks_merchant FOREIGN KEY (merchant_id)
    REFERENCES merchants(merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS planning_shifts (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  week_id VARCHAR(64) NOT NULL,
  employee_id VARCHAR(64) NULL,
  title VARCHAR(150) NOT NULL,
  shift_date DATE NOT NULL,
  start_time TIME NOT NULL,
  end_time TIME NOT NULL,
  break_minutes INT NOT NULL DEFAULT 0,
  position VARCHAR(150) NULL,
  location VARCHAR(150) NULL,
  notes TEXT NULL,
  status ENUM('planned','confirmed','done','cancelled') NOT NULL DEFAULT 'planned',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_planning_shifts_merchant (merchant_id),
  KEY idx_planning_shifts_week (week_id),
  KEY idx_planning_shifts_employee_date (employee_id, shift_date),
  KEY idx_planning_shifts_date (shift_date),
  CONSTRAINT fk_planning_shifts_week FOREIGN KEY (week_id)
    REFERENCES planning_weeks(id) ON DELETE CASCADE,
  CONSTRAINT fk_planning_shifts_employee FOREIGN KEY (employee_id)
    REFERENCES employees(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;