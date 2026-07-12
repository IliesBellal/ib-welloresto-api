-- 053 — payments.net_amount : montant net encaissé par le merchant (amount - fee).
--
-- Initialisé à `amount` à la création du paiement (valeur provisoire, avant
-- réception des frais réels Stripe), puis recalculé à `amount - fee` par le
-- webhook charge.captured qui renseigne déjà `payments.fee` aujourd'hui
-- (internal/webhook/stripe : HandleRetrieveFees -> UpdateFees). Voir
-- docs/KIOSK_DECISIONS.md, section « Terminal — payments.net_amount ».
--
-- `payments` est une table historique (héritage PHP) sans CREATE TABLE dans
-- migrations/ : seule une ALTER TABLE peut la faire évoluer (même convention
-- que 031_add_pin_hash_to_users_rights). Pas de renommage de colonne existante.

ALTER TABLE payments
    ADD COLUMN net_amount INT NOT NULL DEFAULT 0 AFTER fee;
