ALTER TABLE merchant_parameters DROP COLUMN IF EXISTS sno_course_delay_minutes;
DROP TABLE IF EXISTS order_courses;
ALTER TABLE orderitems DROP COLUMN IF EXISTS course;
ALTER TABLE productcateg DROP CONSTRAINT IF EXISTS chk_productcateg_course_step;
ALTER TABLE productcateg DROP COLUMN IF EXISTS course_step;
