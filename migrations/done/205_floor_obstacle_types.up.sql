-- Nouveaux types d'obstacles du plan de salle : fenêtre, plante, pilier
-- (en plus de mur, bar, escalier, porte).
--
-- floor_obstacles.type a été créé en ENUM('wall','bar','stairs','door') dans
-- la migration 063 (MySQL). Le schéma Postgres réel n'étant pas versionné,
-- cette migration est volontairement agnostique : elle supprime toute
-- contrainte CHECK portant sur la colonne, convertit la colonne en
-- varchar(32) (sans effet si elle l'est déjà, correct si c'est un enum
-- natif), puis pose une contrainte CHECK nommée avec les sept types.
--
-- Le jeu de types autorisé reste donc contrôlé en base : ajouter un type
-- demandera une nouvelle migration, comme avant.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (l'API refuserait sinon les
-- nouveaux types à l'insertion). Rejouable.

DO $$
DECLARE
    c record;
BEGIN
    IF to_regclass('public.floor_obstacles') IS NULL THEN
        RAISE NOTICE 'floor_obstacles absente : rien à faire';
        RETURN;
    END IF;

    -- 1. Les CHECK qui comparent la colonne à des littéraux d'enum doivent
    --    partir avant le changement de type, sinon Postgres ne sait pas les
    --    réévaluer.
    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'public.floor_obstacles'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) ILIKE '%type%'
    LOOP
        EXECUTE format('ALTER TABLE public.floor_obstacles DROP CONSTRAINT %I', c.conname);
    END LOOP;

    -- 2. Colonne en texte libre contraint (enum natif → varchar).
    ALTER TABLE public.floor_obstacles
        ALTER COLUMN type TYPE varchar(32) USING type::text;

    -- 3. Contrainte nommée avec le jeu complet de types.
    ALTER TABLE public.floor_obstacles
        ADD CONSTRAINT floor_obstacles_type_check CHECK (type IN (
            'wall', 'bar', 'stairs', 'door', 'window', 'plant', 'pillar'
        ));
END
$$;
