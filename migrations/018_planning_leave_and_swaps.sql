CREATE TABLE IF NOT EXISTS planning_leave_requests (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  employee_id VARCHAR(64) NOT NULL,
  leave_type ENUM('paid','unpaid','sick','other') NOT NULL DEFAULT 'paid',
  start_date DATE NOT NULL,
  end_date DATE NOT NULL,
  status ENUM('pending','approved','rejected','cancelled') NOT NULL DEFAULT 'pending',
  reason TEXT NULL,
  manager_note TEXT NULL,
  requested_by_user_id VARCHAR(64) NULL,
  processed_by_user_id VARCHAR(64) NULL,
  processed_at DATETIME NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_planning_leave_requests_merchant_employee (merchant_id, employee_id),
  KEY idx_planning_leave_requests_status (status),
  KEY idx_planning_leave_requests_range (start_date, end_date),
  CONSTRAINT fk_planning_leave_requests_employee FOREIGN KEY (employee_id)
    REFERENCES employees(id) ON DELETE CASCADE,
  CONSTRAINT fk_planning_leave_requests_requested_by FOREIGN KEY (requested_by_user_id)
    REFERENCES users(user_id) ON DELETE SET NULL,
  CONSTRAINT fk_planning_leave_requests_processed_by FOREIGN KEY (processed_by_user_id)
    REFERENCES users(user_id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS planning_shift_swap_requests (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  requester_employee_id VARCHAR(64) NOT NULL,
  requester_shift_id VARCHAR(64) NOT NULL,
  target_employee_id VARCHAR(64) NOT NULL,
  target_shift_id VARCHAR(64) NOT NULL,
  status ENUM('pending','approved','rejected','cancelled') NOT NULL DEFAULT 'pending',
  reason TEXT NULL,
  manager_note TEXT NULL,
  requested_by_user_id VARCHAR(64) NULL,
  processed_by_user_id VARCHAR(64) NULL,
  processed_at DATETIME NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_planning_shift_swap_requests_merchant (merchant_id),
  KEY idx_planning_shift_swap_requests_status (status),
  KEY idx_planning_shift_swap_requests_requester (requester_employee_id),
  KEY idx_planning_shift_swap_requests_target (target_employee_id),
  CONSTRAINT fk_planning_shift_swap_requests_requester_employee FOREIGN KEY (requester_employee_id)
    REFERENCES employees(id) ON DELETE CASCADE,
  CONSTRAINT fk_planning_shift_swap_requests_requester_shift FOREIGN KEY (requester_shift_id)
    REFERENCES planning_shifts(id) ON DELETE CASCADE,
  CONSTRAINT fk_planning_shift_swap_requests_target_employee FOREIGN KEY (target_employee_id)
    REFERENCES employees(id) ON DELETE CASCADE,
  CONSTRAINT fk_planning_shift_swap_requests_target_shift FOREIGN KEY (target_shift_id)
    REFERENCES planning_shifts(id) ON DELETE CASCADE,
  CONSTRAINT fk_planning_shift_swap_requests_requested_by FOREIGN KEY (requested_by_user_id)
    REFERENCES users(user_id) ON DELETE SET NULL,
  CONSTRAINT fk_planning_shift_swap_requests_processed_by FOREIGN KEY (processed_by_user_id)
    REFERENCES users(user_id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;