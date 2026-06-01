ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS login_enabled TINYINT(1) NOT NULL DEFAULT 1 AFTER enabled;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS position_id VARCHAR(64) NULL AFTER login_enabled;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS position_note TEXT NULL AFTER position_id;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS job_title VARCHAR(150) NULL AFTER position_note;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS role VARCHAR(32) NOT NULL DEFAULT 'employee' AFTER job_title;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS contract_type_code VARCHAR(32) NULL AFTER role;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS contract_start_date DATE NULL AFTER contract_type_code;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS contract_end_date DATE NULL AFTER contract_start_date;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS probation_end_date DATE NULL AFTER contract_end_date;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS last_medical_checkup_date DATE NULL AFTER probation_end_date;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS contract_hours DECIMAL(5,2) NOT NULL DEFAULT 35.00 AFTER last_medical_checkup_date;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS max_weekly_hours DECIMAL(5,2) NOT NULL DEFAULT 35.00 AFTER contract_hours;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS required_rest_days INT NOT NULL DEFAULT 2 AFTER max_weekly_hours;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS sunday_premium TINYINT(1) NOT NULL DEFAULT 0 AFTER required_rest_days;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS night_premium TINYINT(1) NOT NULL DEFAULT 0 AFTER sunday_premium;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS hourly_rate BIGINT NOT NULL DEFAULT 0 AFTER night_premium;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS gross_monthly_salary BIGINT NOT NULL DEFAULT 0 AFTER hourly_rate;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS employer_charges_pct DECIMAL(5,2) NOT NULL DEFAULT 45.00 AFTER gross_monthly_salary;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS transport_cost BIGINT NOT NULL DEFAULT 0 AFTER employer_charges_pct;

ALTER TABLE users_rights
ADD COLUMN IF NOT EXISTS hr_comment TEXT NULL AFTER transport_cost;

ALTER TABLE employees
ADD COLUMN IF NOT EXISTS member_id BIGINT NULL AFTER user_id;

UPDATE employees e
INNER JOIN users_rights ur
	ON ur.merchant_id = e.merchant_id
 AND ur.user_id = e.user_id
 AND ur.enabled = 1
SET e.member_id = ur.id
WHERE e.enabled = 1
	AND e.user_id IS NOT NULL
	AND e.member_id IS NULL;

ALTER TABLE employees
ADD UNIQUE KEY uq_employees_merchant_member (merchant_id, member_id),
ADD KEY idx_employees_member_id (member_id);

UPDATE users_rights
SET login_enabled = 1
WHERE login_enabled IS NULL;