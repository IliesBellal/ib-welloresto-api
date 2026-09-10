-- PostgreSQL migration : instrumentation du chemin d'écriture, lot 2
-- (PROMPT 11). Migration additive, une seule vague. Complète le lot 1
-- (114_write_path_instrumentation) sur deux points restés ouverts :
--
--   1. le coût de revient d'une option de configuration et d'un supplément
--      n'était figé nulle part — order_item_configuration/extra restaient
--      joints à components.purchase_price (prix COURANT), exactement le
--      défaut déjà corrigé pour orderitems.cost_price_unit par le lot 1 ;
--   2. orders.deletion_reason_id (varchar(11)) est trop étroit pour les
--      propres constantes applicatives (KIOSK_CUSTOMER_CANCELLED = 24
--      caractères, SNO_CUSTOMER_CANCELLED = 23) — élargi avec marge.
--
-- Même règle d'or que le lot 1 : centimes entiers, jamais de flottant ;
-- NULL quand le coût n'est pas calculable, jamais 0 ; aucun rétro-remplissage
-- du coût (décision explicite — l'historique reste vide, seule la vente à
-- partir du déploiement du code de ce lot est instrumentée). Voir
-- docs/decisions.md (entrée PROMPT 11) pour le détail des choix.

-- ============================================================================
-- 1. order_item_configuration : coût de revient figé d'une option choisie
-- ============================================================================
-- Coût de LA LIGNE de configuration (déjà multiplié par order_item_configuration
-- .quantity, à la différence de orderitems.cost_price_unit qui est unitaire) :
-- une ligne order_item_configuration n'est jamais réécrite en place pour un
-- changement de quantité (elle est supprimée puis réinsérée, comme le reste
-- de la configuration d'une ligne de commande — voir repository.go, nettoyage
-- systématique extra/without/order_item_configuration avant réinsertion), donc
-- il n'y a pas la même contrainte de "recalcul au moment de la lecture" que
-- pour orderitems.
--
-- cost_price_reason distingue :
--   - NO_RECIPE : l'option choisie n'a aucun ingrédient lié
--     (configurable_attribute_options.component_id NULL) — cas normal et,
--     vérifié sur staging au moment d'écrire cette migration, le cas de
--     100% des options existantes : la fonctionnalité de liaison
--     option->ingrédient (migration 079) n'a encore jamais été utilisée
--     depuis l'interface d'administration marchand. Rien d'anormal ici :
--     ce lot ne fait qu'arrêter de perdre l'information le jour où un
--     marchand commencera à lier des options.
--   - INCOMPLETE_RECIPE : un ingrédient est lié mais son coût n'est pas
--     calculable (prix d'achat manquant/nul, conversion d'unité introuvable).
ALTER TABLE order_item_configuration
    ADD COLUMN IF NOT EXISTS cost_price_unit integer,
    ADD COLUMN IF NOT EXISTS cost_price_reason varchar(20);

ALTER TABLE order_item_configuration
    ADD CONSTRAINT chk_oic_cost_price_reason
    CHECK (cost_price_reason IS NULL OR cost_price_reason IN ('NO_RECIPE', 'INCOMPLETE_RECIPE'));

COMMENT ON COLUMN order_item_configuration.cost_price_unit IS 'Coût de revient de cette ligne de configuration (quantity déjà incluse), en centimes entiers, snapshoté à l''écriture. Ne change plus jamais après coup, y compris si components.purchase_price est modifié ultérieurement. NULL si non calculable (voir cost_price_reason) ou si la ligne a été écrite avant ce lot.';
COMMENT ON COLUMN order_item_configuration.cost_price_reason IS 'Raison de cost_price_unit NULL : NO_RECIPE (option sans ingrédient lié — cas normal, 100% des options aujourd''hui) ou INCOMPLETE_RECIPE (ingrédient lié mais prix d''achat/conversion d''unité manquants). NULL si cost_price_unit est renseigné, ou si la ligne est antérieure à ce lot.';

-- ============================================================================
-- 2. extra : coût de revient figé d'un supplément
-- ============================================================================
-- extra.component_id est NOT NULL (un supplément référence toujours un
-- ingrédient réel) : pas de cas NO_RECIPE possible ici, seulement
-- INCOMPLETE_RECIPE (prix d'achat non calculable — 92,7% des lignes extra
-- existantes sur staging au moment d'écrire cette migration, cohérent avec
-- les 84% de components sans prix d'achat utilisable cités par le brief) ou
-- une valeur réelle.
ALTER TABLE extra
    ADD COLUMN IF NOT EXISTS cost_price_unit integer,
    ADD COLUMN IF NOT EXISTS cost_price_reason varchar(20);

ALTER TABLE extra
    ADD CONSTRAINT chk_extra_cost_price_reason
    CHECK (cost_price_reason IS NULL OR cost_price_reason IN ('NO_RECIPE', 'INCOMPLETE_RECIPE'));

COMMENT ON COLUMN extra.cost_price_unit IS 'Coût de revient de ce supplément (extra.quantity déjà incluse — extra n''a pas de unit_of_measure propre : quantity compte directement des unités de purchase_price_quantity du composant lié), en centimes entiers, snapshoté à l''écriture. Ne change plus jamais après coup. NULL si non calculable (voir cost_price_reason) ou si la ligne a été écrite avant ce lot.';
COMMENT ON COLUMN extra.cost_price_reason IS 'Raison de cost_price_unit NULL : INCOMPLETE_RECIPE (prix d''achat du composant manquant/nul) — NO_RECIPE n''est structurellement pas possible ici, extra.component_id est NOT NULL. NULL si cost_price_unit est renseigné, ou si la ligne est antérieure à ce lot.';

-- ============================================================================
-- 3. orders.deletion_reason_id : élargissement (bug de troncature en cours)
-- ============================================================================
-- varchar(11) est trop étroit pour les propres constantes de ce dépôt
-- (KIOSK_CUSTOMER_CANCELLED = 24 caractères, SNO_CUSTOMER_CANCELLED = 23) :
-- confirmé sur staging que ces valeurs y arrivent tronquées à 11 caractères
-- (KIOSK_CUSTO, SNO_CUSTOME) alors même qu'un test direct contre Postgres
-- montre qu'une écriture paramétrée normale échoue (erreur "value too long")
-- plutôt que de tronquer silencieusement sur ce type — signe qu'un chemin
-- d'écriture hors de ce dépôt Go écrit encore une valeur pré-tronquée. Élargi
-- à 32 caractères : large marge au-dessus de la plus longue valeur légitime
-- actuelle (24) pour absorber une future sentinelle sans nouvelle migration,
-- sans viser un type illimité qui masquerait un futur bug de génération de
-- valeur. Élargir un varchar est un changement de type sans perte de données
-- (aucune valeur existante n'est concernée), donc sans risque sur l'historique.
ALTER TABLE orders ALTER COLUMN deletion_reason_id TYPE varchar(32);
