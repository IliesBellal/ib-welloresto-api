-- Correctif de 212 : order_courses.order_id doit être un entier comme
-- orders.order_id / orderitems.order_id. En texte, les jointures
-- (orders o ON o.order_id = oc.order_id) échouaient avec
-- « operator does not exist: integer = character varying ».
-- Sans effet si la colonne est déjà un entier (212 corrigée appliquée).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'order_courses' AND column_name = 'order_id'
          AND data_type <> 'integer'
    ) THEN
        ALTER TABLE order_courses
            ALTER COLUMN order_id TYPE integer USING order_id::integer;
    END IF;
END $$;
