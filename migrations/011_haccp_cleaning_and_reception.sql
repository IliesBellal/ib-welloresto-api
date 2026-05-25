-- Tranche HACCP: C.3 / C.4 / C.5
-- Cleaning tasks + cleaning executions + goods receipts

CREATE TABLE IF NOT EXISTS cleaning_tasks (
  id              VARCHAR(64) NOT NULL,
  merchant_id     VARCHAR(64) NOT NULL,
  zone            VARCHAR(150) NOT NULL,
  name            VARCHAR(255) NOT NULL,
  frequency_unit  ENUM('day','week','month') NOT NULL DEFAULT 'day',
  frequency_count INT NOT NULL DEFAULT 1,
  active          TINYINT(1) NOT NULL DEFAULT 1,
  enabled         TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at      DATETIME NULL,
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_cleaning_tasks_merchant (merchant_id),
  KEY idx_cleaning_tasks_merchant_enabled (merchant_id, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS cleaning_executions (
  id          VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  task_id     VARCHAR(64) NOT NULL,
  comment     TEXT NULL,
  photo_url   VARCHAR(512) NULL,
  status      ENUM('done') NOT NULL DEFAULT 'done',
  created_by  VARCHAR(255) NOT NULL,
  enabled     TINYINT(1) NOT NULL DEFAULT 1,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_cleaning_exec_merchant_created (merchant_id, created_at),
  KEY idx_cleaning_exec_task (task_id),
  CONSTRAINT fk_cleaning_exec_task FOREIGN KEY (task_id)
    REFERENCES cleaning_tasks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS goods_receipts (
  id                  VARCHAR(64) NOT NULL,
  merchant_id         VARCHAR(64) NOT NULL,
  supplier            VARCHAR(255) NOT NULL,
  product_type        VARCHAR(50) NOT NULL,
  batch_number        VARCHAR(120) NOT NULL,
  product_temp        DECIMAL(5,2) NOT NULL,
  control_sample      VARCHAR(255) NULL,
  quantities_verified TINYINT(1) NOT NULL DEFAULT 0,
  non_conformities    JSON NULL,
  comment             TEXT NULL,
  invoice_url         VARCHAR(512) NULL,
  created_by          VARCHAR(255) NOT NULL,
  enabled             TINYINT(1) NOT NULL DEFAULT 1,
  created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_goods_receipts_merchant_created (merchant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Seed minimal cleaning task examples (safe no-op if IDs already exist only if manually kept unique)
-- Keep commented by default to avoid hardcoding merchant IDs.
-- INSERT INTO cleaning_tasks (id, merchant_id, zone, name, frequency_unit, frequency_count)
-- VALUES ('haccp-ct-example', 'merchant-id', 'Cuisine', 'Désinfection plan de travail', 'day', 1);
