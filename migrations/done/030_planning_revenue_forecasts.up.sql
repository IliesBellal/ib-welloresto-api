CREATE TABLE IF NOT EXISTS planning_revenue_forecasts (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  forecast_date DATE NOT NULL,
  amount_ht_cents BIGINT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_planning_revenue_forecasts_merchant_date (merchant_id, forecast_date),
  KEY idx_planning_revenue_forecasts_merchant (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;