-- Mesure d'audience des bornes de commande : onglet Borne de la page Analyse
-- du back-office. Voir docs/mesure-audience.md (D27 à D33).
--
-- Mesure ANONYME : la borne appartient au restaurant, aucun traceur n'est
-- déposé sur l'appareil d'un client, et rien ne permet d'identifier une
-- personne :
--   - session_id : tiré au hasard par la borne au toucher de l'écran de
--     veille, oublié au retour en veille ; jamais relié à une commande ;
--   - conservation 25 mois (tâche CleanupOldAudienceEvents), comme sno_events.
--
-- Une ligne = un événement d'une session de commande sur une borne.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

CREATE TABLE IF NOT EXISTS kiosk_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type   varchar(32) NOT NULL,
    merchant_id  varchar(64) NOT NULL,
    kiosk_id     varchar(64) NOT NULL,
    session_id   uuid NOT NULL,
    -- Millisecondes écoulées depuis le début de la session, mesurées par la
    -- borne : la durée d'une commande ne dépend pas de l'heure de la borne.
    elapsed_ms   integer,
    product_id   varchar(64),
    -- Montant du panier en centimes (abandon de session).
    amount_cents integer,
    -- Précision selon le type : mode de commande (IN, TAKE_AWAY), moyen de
    -- paiement (card, counter), raison d'un échec de paiement (declined,
    -- canceled, terminal_unavailable, error) ou d'un abandon (inactivity,
    -- cancel).
    detail       varchar(32),
    -- Étape où la session a été abandonnée : menu, cart, upsell, summary,
    -- payment.
    step         varchar(32),
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT kiosk_events_type_check CHECK (event_type IN (
        'session_start', 'fulfillment_chosen', 'product_view', 'add_to_cart',
        'summary_view', 'payment_start', 'payment_failed', 'order_created',
        'session_abandoned'
    ))
);

CREATE INDEX IF NOT EXISTS kiosk_events_merchant_idx ON kiosk_events (merchant_id, created_at);
CREATE INDEX IF NOT EXISTS kiosk_events_created_at_idx ON kiosk_events (created_at);

COMMENT ON TABLE kiosk_events IS 'Mesure d''audience anonyme des bornes de commande. session_id aléatoire, jamais relié à une commande. Conservation 25 mois.';
