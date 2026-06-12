ALTER TABLE planning_weeks
    ADD COLUMN published_at DATETIME NULL AFTER status;

UPDATE planning_weeks
SET published_at = updated_at
WHERE status = 'published' AND published_at IS NULL;
