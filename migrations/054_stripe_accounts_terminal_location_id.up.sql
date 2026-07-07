-- 054 — stripe_accounts.terminal_location_id : identifiant Stripe Terminal
-- Location (tml_...) du merchant, nécessaire côté Flutter pour connecter le
-- lecteur de carte physique à la bonne « location » Stripe.
--
-- Aucun endpoint admin ne renseigne cette valeur : elle est insérée
-- manuellement en base par le développeur au moment de l'activation Terminal
-- d'un merchant. NULL par défaut = Terminal non activé. Exposée en lecture
-- seule dans GET /kiosk/settings (nullable). Voir docs/KIOSK_DECISIONS.md,
-- section « Terminal — terminal_location_id ».
--
-- `stripe_accounts` est une table historique (héritage PHP) sans CREATE TABLE
-- dans migrations/ : seule une ALTER TABLE peut la faire évoluer.

ALTER TABLE stripe_accounts
    ADD COLUMN terminal_location_id VARCHAR(255) NULL DEFAULT NULL;
