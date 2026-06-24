ALTER TABLE configurable_attribute_options
ADD COLUMN IF NOT EXISTS image_url VARCHAR(500) NULL DEFAULT NULL AFTER extra_price;
