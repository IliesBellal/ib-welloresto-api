CREATE TABLE IF NOT EXISTS planning_time_entries (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  employee_id VARCHAR(64) NOT NULL,
  shift_id VARCHAR(64) NULL,
  entry_mode_code VARCHAR(32) NOT NULL,
  clock_in_at DATETIME NOT NULL,
  clock_out_at DATETIME NULL,
  clock_in_note TEXT NULL,
  clock_out_note TEXT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_planning_time_entries_merchant_employee (merchant_id, employee_id),
  KEY idx_planning_time_entries_open (employee_id, clock_out_at),
  KEY idx_planning_time_entries_shift (shift_id),
  KEY idx_planning_time_entries_clock_in (clock_in_at),
  CONSTRAINT fk_planning_time_entries_employee FOREIGN KEY (employee_id)
    REFERENCES employees(id) ON DELETE CASCADE,
  CONSTRAINT fk_planning_time_entries_shift FOREIGN KEY (shift_id)
    REFERENCES planning_shifts(id) ON DELETE SET NULL,
  CONSTRAINT fk_planning_time_entries_mode FOREIGN KEY (entry_mode_code)
    REFERENCES sys_time_tracking_modes(code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;