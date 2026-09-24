-- Site vitrine, formulaire "demande de démo" (DemoForm.astro) — le visiteur
-- réserve désormais un créneau d'appel précis plutôt qu'une simple préférence
-- textuelle, pour engager le restaurateur à répondre à l'heure choisie. Voir
-- wello-resto-vitrine/docs/decisions-log.md pour le contexte produit.
--
-- Index unique partiel (slot_start WHERE cancelled_at IS NULL), même pattern
-- que subscription_items (migration 137) : garantit qu'un seul rendez-vous
-- actif existe par créneau exact (protection native Postgres contre la
-- double réservation concurrente, sans transaction applicative — l'égalité
-- stricte sur un instant fixe ne nécessite pas la logique de chevauchement
-- de plages qu'utilise le module bookings pour les réservations de table)
-- tout en gardant l'historique des annulations (cancelled_at renseigné,
-- ligne jamais supprimée, libère le créneau pour une nouvelle réservation).
CREATE TABLE demo_bookings (
    id                     integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slot_start             timestamptz NOT NULL,
    slot_end               timestamptz NOT NULL,
    establishment          text NOT NULL,
    establishment_address  text,
    restaurant_type        text NOT NULL,
    phone                  text NOT NULL,
    email                  text NOT NULL,
    situation              text NOT NULL,
    cancelled_at           timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_demo_bookings_slot_start_active
    ON demo_bookings (slot_start) WHERE cancelled_at IS NULL;

CREATE INDEX idx_demo_bookings_slot_start ON demo_bookings (slot_start);

COMMENT ON TABLE demo_bookings IS 'Rendez-vous téléphoniques commerciaux réservés depuis DemoForm.astro (site vitrine) — remplace le champ texte libre "créneau souhaité". Un rendez-vous par créneau exact (index unique partiel ci-dessus), jamais synchronisé avec l''agenda réel du fondateur : voir decisions-log.md.';
COMMENT ON COLUMN demo_bookings.cancelled_at IS 'NULL = rendez-vous actif. Non-NULL = annulé — jamais supprimé (historique), libère le créneau pour une nouvelle réservation via l''index unique partiel.';
