ALTER TABLE haccp_settings
  ADD COLUMN temp_failure_photo_required TINYINT(1) NOT NULL DEFAULT 0 AFTER temp_corrective_actions;
