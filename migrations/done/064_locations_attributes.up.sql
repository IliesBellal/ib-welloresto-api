ALTER TABLE locations
  ADD COLUMN attributes JSON DEFAULT NULL
  COMMENT 'Attributs booléens : pmr, terrace, vip, window';
