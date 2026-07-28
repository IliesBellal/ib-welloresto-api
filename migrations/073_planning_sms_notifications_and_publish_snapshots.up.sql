ALTER TABLE planning_settings
  ADD COLUMN planning_sms_notifications_enabled tinyint(1) NOT NULL DEFAULT 0 AFTER shift_swap_approval_mode;

CREATE TABLE planning_published_shift_snapshots (
  id varchar(64) NOT NULL,
  merchant_id varchar(64) NOT NULL,
  week_id varchar(64) NOT NULL,
  employee_id varchar(64) NOT NULL,
  shift_date date NOT NULL,
  start_time time NOT NULL,
  end_time time NOT NULL,
  title varchar(255) NOT NULL,
  position_label varchar(150) NULL,
  published_at datetime NOT NULL DEFAULT UTC_TIMESTAMP(),
  created_at datetime NOT NULL DEFAULT UTC_TIMESTAMP(),
  PRIMARY KEY (id),
  KEY idx_planning_published_shift_snapshots_week (merchant_id, week_id),
  KEY idx_planning_published_shift_snapshots_employee (merchant_id, week_id, employee_id)
);
