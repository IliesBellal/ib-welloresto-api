CREATE TABLE IF NOT EXISTS employee_documents (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  employee_id VARCHAR(64) NOT NULL,
  document_type VARCHAR(32) NOT NULL,
  name VARCHAR(255) NOT NULL,
  file_key VARCHAR(512) NOT NULL,
  content_type VARCHAR(120) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_empdocs_merchant (merchant_id),
  KEY idx_empdocs_employee (employee_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;