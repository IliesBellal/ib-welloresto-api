-- Nouveau type d'obstacle du plan de salle : la caisse (cash_register).
--
-- Remplace la contrainte floor_obstacles_type_check posée par la migration 205
-- par une version à huit types. Rejouable.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (l'API refuserait sinon le
-- nouveau type à l'insertion).

DO $$
BEGIN
    IF to_regclass('public.floor_obstacles') IS NULL THEN
        RAISE NOTICE 'floor_obstacles absente : rien à faire';
        RETURN;
    END IF;

    ALTER TABLE public.floor_obstacles DROP CONSTRAINT IF EXISTS floor_obstacles_type_check;

    ALTER TABLE public.floor_obstacles
        ADD CONSTRAINT floor_obstacles_type_check CHECK (type IN (
            'wall', 'bar', 'stairs', 'door', 'window', 'plant', 'pillar', 'cash_register'
        ));
END
$$;
