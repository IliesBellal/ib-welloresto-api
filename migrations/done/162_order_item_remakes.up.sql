-- Retour en production d'unités déjà servies (plat tombé, erreur,
-- réclamation) depuis la caisse ou l'écran de production. Voir
-- docs/order-lifecycle.md, section « Retour en production ».
--
-- orderitems.remake_quantity : unités renvoyées en production et pas encore
-- re-servies. Incrémentée par PATCH /orders/{id}/back-to-production, remise à
-- 0 par la distribution (SetDistributedProducts). Sert uniquement au badge
-- « REFAIRE » de l'écran de production : les unités à produire restent
-- quantity - distributed_quantity, comme pour toute ligne.
--
-- order_item_remakes : historique de chaque renvoi (quantité, motif
-- optionnel, auteur) pour la traçabilité et les futures stats de pertes. Une
-- ligne n'est jamais modifiée ni supprimée.
--
-- ORDRE DE DÉPLOIEMENT : appliquer cette migration AVANT de déployer le code.
-- Le chargement des commandes (orders_fetcher_builder) lit
-- oi.remake_quantity et SetDistributedProducts l'écrit (erreur ignorée par
-- ce chemin) : sans la colonne, plus aucune commande ne se charge et la
-- distribution échoue silencieusement.
--
-- ADD COLUMN avec défaut constant : sans réécriture de table en Postgres 11+,
-- verrou ACCESS EXCLUSIVE très bref sur orderitems.
ALTER TABLE orderitems
    ADD COLUMN IF NOT EXISTS remake_quantity integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN orderitems.remake_quantity IS 'Unités renvoyées en production (retour en production) et pas encore re-servies ; remis à 0 à la distribution. Badge « REFAIRE » côté production.';

CREATE TABLE IF NOT EXISTS order_item_remakes (
    id            bigserial    PRIMARY KEY,
    merchant_id   varchar(64)  NOT NULL,
    order_id      varchar(64)  NOT NULL,
    order_item_id varchar(64)  NOT NULL,
    product_id    varchar(64)  NOT NULL,
    quantity      integer      NOT NULL CHECK (quantity > 0),
    reason        varchar(32)  NULL,
    created_by    varchar(64)  NOT NULL,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT chk_order_item_remakes_reason
        CHECK (reason IS NULL OR reason IN ('PREPARATION_ERROR', 'DROPPED', 'CUSTOMER_COMPLAINT', 'OTHER'))
);

CREATE INDEX IF NOT EXISTS idx_order_item_remakes_merchant
    ON order_item_remakes (merchant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_order_item_remakes_order
    ON order_item_remakes (order_id);

COMMENT ON TABLE order_item_remakes IS 'Historique des retours en production d''unités déjà servies (quantité, motif optionnel, auteur).';
COMMENT ON COLUMN order_item_remakes.reason IS 'Motif optionnel : PREPARATION_ERROR, DROPPED, CUSTOMER_COMPLAINT, OTHER (models.RemakeReasons).';
