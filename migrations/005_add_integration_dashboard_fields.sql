-- Migration 005: Add dashboard/settings fields to integration tables
-- Run this before deploying the /integrations/* GET endpoints

-- ─── Uber Eats ────────────────────────────────────────────────────────────────
ALTER TABLE integration_uber_eats
    ADD COLUMN commission_rate INT         NOT NULL DEFAULT 0     COMMENT 'Commission rate in percent',
    ADD COLUMN last_sync       DATETIME    NULL     DEFAULT NULL   COMMENT 'Last successful menu/product sync timestamp (UTC)',
    ADD COLUMN synced_items    INT         NOT NULL DEFAULT 0      COMMENT 'Number of products currently mapped to Uber Eats';

-- ─── Deliveroo ────────────────────────────────────────────────────────────────
ALTER TABLE integration_deliveroo
    ADD COLUMN commission_rate INT         NOT NULL DEFAULT 0     COMMENT 'Commission rate in percent',
    ADD COLUMN last_sync       DATETIME    NULL     DEFAULT NULL   COMMENT 'Last successful menu upload timestamp (UTC)',
    ADD COLUMN synced_items    INT         NOT NULL DEFAULT 0      COMMENT 'Number of products in last published menu';

-- ─── ScanNOrder ───────────────────────────────────────────────────────────────
ALTER TABLE scannorder_settings
    ADD COLUMN commission_rate      INT          NOT NULL DEFAULT 0    COMMENT 'Commission rate in percent (0 for internal tool)',
    ADD COLUMN last_sync            DATETIME     NULL     DEFAULT NULL  COMMENT 'Last settings sync timestamp (UTC)',
    ADD COLUMN synced_items         INT          NOT NULL DEFAULT 0     COMMENT 'Number of products currently available on ScanNOrder',
    ADD COLUMN logo_url             VARCHAR(512) NULL     DEFAULT NULL  COMMENT 'Merchant logo URL',
    ADD COLUMN banner_url           VARCHAR(512) NULL     DEFAULT NULL  COMMENT 'Merchant banner/cover URL',
    ADD COLUMN header_title         VARCHAR(255) NULL     DEFAULT NULL  COMMENT 'Hero section title shown on the ordering page',
    ADD COLUMN header_text          VARCHAR(512) NULL     DEFAULT NULL  COMMENT 'Hero section subtitle/body text',
    ADD COLUMN cgv_link             VARCHAR(512) NULL     DEFAULT NULL  COMMENT 'URL to general terms and conditions',
    ADD COLUMN return_policy_link   VARCHAR(512) NULL     DEFAULT NULL  COMMENT 'URL to return / refund policy',
    ADD COLUMN legal_notices_link   VARCHAR(512) NULL     DEFAULT NULL  COMMENT 'URL to legal notices',
    ADD COLUMN takeaway_auto_accept TINYINT(1)   NOT NULL DEFAULT 0     COMMENT '1 = takeaway orders are auto-accepted',
    ADD COLUMN delivery_auto_accept TINYINT(1)   NOT NULL DEFAULT 0     COMMENT '1 = delivery orders are auto-accepted';
