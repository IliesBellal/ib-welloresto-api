-- Mesure d'audience ScanNOrder (hub de franchise et pages restaurant) : onglet
-- ScanNOrder de la page Analyse du back-office. Voir docs/franchises.md
-- (D17, D19 à D23).
--
-- Mesure ANONYME, dans le cadre de l'exemption de consentement de la CNIL
-- pour la mesure d'audience (pas de bandeau) :
--   - aucun identifiant de personne : ni IP, ni cookie, ni identifiant
--     d'appareil persistant ;
--   - visit_id : tiré au hasard par onglet, oublié à la fermeture ou après
--     30 min d'inactivité ; sert seulement à savoir jusqu'où va une visite ;
--   - conservation 25 mois (tâche CleanupOldAudienceEvents), données jamais
--     croisées avec les fiches clients.
-- Les mesures qui demanderont un consentement (visiteurs uniques sur la
-- durée, pixels publicitaires…) passeront par le module de consentement du
-- front, pas par ces tables.
--
-- 1. sno_events : événements d'audience.
-- 2. sno_order_attributions : d'où vient une commande ScanNOrder (source de
--    la visite, hub de franchise), posé à la création de la commande. Table à
--    part : rien ne change dans l'écriture ni la signature des commandes.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

CREATE TABLE IF NOT EXISTS sno_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type   varchar(32) NOT NULL,
    merchant_id  varchar(64),
    franchise_id varchar(64),
    visit_id     uuid,
    -- Source de la visite (menu_view, hub_view) : qr_table, direct, search,
    -- social, referral, hub, ou une valeur utm_source.
    source       varchar(32),
    product_id   varchar(64),
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sno_events_type_check CHECK (event_type IN (
        'hub_view', 'hub_order_click', 'hub_reservation_click',
        'menu_view', 'product_view', 'add_to_cart', 'checkout_start', 'payment_redirect', 'order_created'
    ))
);

CREATE INDEX IF NOT EXISTS sno_events_merchant_idx ON sno_events (merchant_id, created_at) WHERE merchant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS sno_events_franchise_idx ON sno_events (franchise_id, created_at) WHERE franchise_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS sno_events_created_at_idx ON sno_events (created_at);

COMMENT ON TABLE sno_events IS 'Mesure d''audience anonyme de ScanNOrder (hub et pages restaurant). visit_id aléatoire par onglet, non persistant. Conservation 25 mois.';

CREATE TABLE IF NOT EXISTS sno_order_attributions (
    order_id     integer PRIMARY KEY,
    merchant_id  varchar(64) NOT NULL,
    visit_id     uuid,
    source       varchar(32),
    -- Franchise dont le hub a mené à la commande (clic Commander dans les 2 h).
    franchise_id varchar(64),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sno_order_attributions_merchant_idx ON sno_order_attributions (merchant_id, created_at);

COMMENT ON TABLE sno_order_attributions IS 'Origine d''une commande ScanNOrder (source de la visite, hub de franchise). Conservation 25 mois.';
