CREATE TABLE IF NOT EXISTS planning_week_templates (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  label VARCHAR(120) NOT NULL,
  notes TEXT NULL,
  active TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_planning_week_templates_merchant (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS planning_week_template_shifts (
  id VARCHAR(64) NOT NULL,
  week_template_id VARCHAR(64) NOT NULL,
  day_of_week TINYINT NOT NULL,
  employee_id VARCHAR(64) NULL,
  position_id VARCHAR(64) NULL,
  title VARCHAR(120) NULL,
  start_time TIME NOT NULL,
  end_time TIME NOT NULL,
  break_minutes INT NOT NULL DEFAULT 0,
  location VARCHAR(120) NULL,
  notes TEXT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_planning_week_template_shifts_template (week_template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;