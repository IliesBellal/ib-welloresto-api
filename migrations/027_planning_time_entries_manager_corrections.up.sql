ALTER TABLE planning_time_entries
  ADD COLUMN modified_by VARCHAR(255) NULL AFTER clock_out_note,
  ADD COLUMN modified_at DATETIME NULL AFTER modified_by,
  ADD COLUMN modification_reason VARCHAR(255) NULL AFTER modified_at;