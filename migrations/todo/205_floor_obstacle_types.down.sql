-- Retour au jeu de types d'origine. Les obstacles des types ajoutés par la
-- migration montante (fenêtre, plante, pilier) ne peuvent plus exister : ils
-- sont supprimés avant de reposer la contrainte à quatre types.
DO $$
BEGIN
    IF to_regclass('public.floor_obstacles') IS NULL THEN
        RETURN;
    END IF;

    ALTER TABLE public.floor_obstacles DROP CONSTRAINT IF EXISTS floor_obstacles_type_check;

    DELETE FROM public.floor_obstacles WHERE type IN ('window', 'plant', 'pillar');

    ALTER TABLE public.floor_obstacles
        ADD CONSTRAINT floor_obstacles_type_check CHECK (type IN ('wall', 'bar', 'stairs', 'door'));
END
$$;
