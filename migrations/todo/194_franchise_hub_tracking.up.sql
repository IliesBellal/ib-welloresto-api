-- Statistiques du hub de franchise (onglet ScanNOrder de la page Analyse du
-- back-office). Voir docs/franchises.md.
--
-- 1. franchise_hub_events : affichages du hub et clics « Commander » /
--    « Réserver une table », envoyés par la page /franchise/{slug}. Aucune
--    donnée personnelle : ni identifiant de visiteur, ni IP, ni cookie. On
--    compte des événements, pas des personnes.
-- 2. franchise_hub_orders : commandes ScanNOrder passées après un clic
--    « Commander » sur le hub (attribution posée à la création de la
--    commande). Table à part plutôt qu'une colonne de orders : rien ne change
--    dans l'écriture ni la signature des commandes.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (le code écrit et lit ces
-- tables). Rejouable.

CREATE TABLE IF NOT EXISTS franchise_hub_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    franchise_id varchar(64) NOT NULL,
    -- NULL pour un affichage du hub (événement de la franchise entière).
    merchant_id  varchar(64),
    event_type   varchar(32) NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT franchise_hub_events_type_check
        CHECK (event_type IN ('view', 'order_click', 'reservation_click'))
);

CREATE INDEX IF NOT EXISTS franchise_hub_events_franchise_idx ON franchise_hub_events (franchise_id, created_at);
CREATE INDEX IF NOT EXISTS franchise_hub_events_merchant_idx ON franchise_hub_events (merchant_id, created_at) WHERE merchant_id IS NOT NULL;

COMMENT ON TABLE franchise_hub_events IS 'Affichages du hub de franchise et clics Commander / Réserver (anonymes : aucune donnée de visiteur).';

CREATE TABLE IF NOT EXISTS franchise_hub_orders (
    order_id     integer PRIMARY KEY,
    franchise_id varchar(64) NOT NULL,
    merchant_id  varchar(64) NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS franchise_hub_orders_merchant_idx ON franchise_hub_orders (merchant_id, created_at);

COMMENT ON TABLE franchise_hub_orders IS 'Commandes ScanNOrder passées depuis le hub de franchise (clic Commander puis commande dans la même visite).';
