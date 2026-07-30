ALTER TABLE planning_settings
ADD COLUMN premium_cumulation_mode varchar(16) NOT NULL DEFAULT 'highest',
ADD COLUMN night_sunday_combined_multiplier numeric(4,2) NULL;
