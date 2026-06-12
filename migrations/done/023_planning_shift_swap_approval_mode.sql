ALTER TABLE planning_settings
ADD COLUMN shift_swap_approval_mode VARCHAR(64) NOT NULL DEFAULT 'manager_required' AFTER attendance_source;

UPDATE planning_settings
SET shift_swap_approval_mode = 'manager_required'
WHERE shift_swap_approval_mode IS NULL OR shift_swap_approval_mode = '';