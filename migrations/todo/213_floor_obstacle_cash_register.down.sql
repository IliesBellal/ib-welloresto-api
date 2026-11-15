-- Retour au jeu de sept types de la migration 205. Les caisses posées sur les
-- plans sont supprimées avant de reposer la contrainte.
DO $$
BEGIN
    IF to_regclass('public.floor_obstacles') IS NULL THEN
        RETURN;
    END IF;

    ALTER TABLE public.floor_obstacles DROP CONSTRAINT IF EXISTS floor_obstacles_type_check;

    DELETE FROM public.floor_obstacles WHERE type = 'cash_register';

    ALTER TABLE public.floor_obstacles
        ADD CONSTRAINT floor_obstacles_type_check CHECK (type IN (
            'wall', 'bar', 'stairs', 'door', 'window', 'plant', 'pillar'
        ));
END
$$;
