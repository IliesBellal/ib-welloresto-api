-- Ordre d'affichage des options d'un attribut configurable ("Options &
-- Suppléments" : sauces, tailles, cuissons...).
--
-- Jusqu'ici aucune colonne ne portait cet ordre : le glisser-déposer du
-- back-office envoyait bien les options dans le nouvel ordre, mais UpdateAttribute
-- ne l'écrivait nulle part et toutes les lectures se faisaient sans ORDER BY
-- (ordre physique de Postgres, non garanti). Les caisses, bornes et ScanNOrder
-- affichaient donc un ordre arbitraire.
--
-- display_order : rang de l'option DANS son attribut (0, 1, 2...). Il est
-- réécrit à chaque sauvegarde de l'attribut à partir de la position de l'option
-- dans le tableau reçu ; une option créée seule (webhooks Uber Eats / Deliveroo)
-- se place en fin de liste. Les lectures trient par (display_order, id) : à
-- égalité l'ordre de création (id identity) départage.
--
-- Reprise des données : les options existantes sont numérotées par ordre
-- alphabétique de leur titre (insensible à la casse, id en départage) dans
-- chaque attribut. Les options désactivées sont numérotées aussi : elles ne
-- s'affichent pas, mais une réactivation ne doit pas les ranger à 0.
-- L'ordre alphabétique suit la collation de la base (LOWER(title)).
--
-- Rejouable sans écraser un ordre saisi depuis : la reprise ne s'exécute que
-- si la colonne n'existe pas encore.
--
-- Les menus de la borne et de ScanNOrder sont cachés dans Redis (purgés à chaque
-- modification du menu, TTL en filet) : après la migration ils gardent l'ancien
-- ordre jusqu'à leur expiration ou à la prochaine modification du menu.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Les lectures du menu
-- sélectionnent display_order ; sans la colonne, le menu (caisse, borne,
-- ScanNOrder, back-office) ne se charge plus.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'configurable_attribute_options'
          AND column_name = 'display_order'
    ) THEN
        ALTER TABLE configurable_attribute_options
            ADD COLUMN display_order integer NOT NULL DEFAULT 0;

        UPDATE configurable_attribute_options cao
        SET display_order = ranked.rn
        FROM (
            SELECT id,
                   ROW_NUMBER() OVER (
                       PARTITION BY configurable_attribute_id
                       ORDER BY LOWER(title), id
                   ) - 1 AS rn
            FROM configurable_attribute_options
        ) ranked
        WHERE ranked.id = cao.id;

        -- La caisse ne recharge son menu que si last_menu_update a changé
        -- (GetMenu répond "no_update_required" sinon) : sans ce marquage, elle
        -- garderait l'ancien ordre jusqu'à la prochaine modification du menu.
        UPDATE merchant_parameters
        SET last_menu_update = now()
        WHERE merchant_id IN (SELECT DISTINCT merchant_id FROM configurable_attributes);
    END IF;
END
$$;

COMMENT ON COLUMN configurable_attribute_options.display_order IS 'Rang d''affichage de l''option dans son attribut (0, 1, 2...). Réécrit à chaque sauvegarde de l''attribut selon l''ordre du tableau reçu ; lectures triées par (display_order, id).';
