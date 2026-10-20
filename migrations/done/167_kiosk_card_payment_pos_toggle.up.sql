-- Borne : ouverture/fermeture du paiement carte depuis le POS, au fil du
-- service (docs/KIOSK_DECISIONS.md, "Paiement carte ouvert/fermé depuis le
-- POS"). Fonctionnalité réservée à certains établissements :
--
--   card_payment_pos_toggle : droit d'accès, activé à la main par l'équipe
--     Wello (jamais modifiable via PUT /pos/settings/kiosk/settings). false =
--     fonctionnalité invisible, seul card_payment_enabled compte.
--   card_payment_closed_at  : dernière fermeture depuis le POS. Le paiement
--     carte est fermé si cette date tombe dans la journée locale courante du
--     merchant : il se rouvre donc tout seul à minuit, sans tâche planifiée.
--     NULL = jamais fermé (ou rouvert explicitement).
--
-- Activer pour un établissement :
--   UPDATE kiosk_settings SET card_payment_pos_toggle = true WHERE merchant_id = '<id>';
-- (la ligne kiosk_settings doit exister : elle est créée au premier
-- enregistrement des paramètres Kiosk depuis le back-office.)
--
-- ORDRE DE DÉPLOIEMENT : appliquer cette migration AVANT de déployer le code
-- qui lit les colonnes (Repository.GetSettingsByMerchant) — dans l'ordre
-- inverse, GET /kiosk/settings échouerait en erreur SQL.
ALTER TABLE kiosk_settings
    ADD COLUMN IF NOT EXISTS card_payment_pos_toggle boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS card_payment_closed_at timestamptz;

COMMENT ON COLUMN kiosk_settings.card_payment_pos_toggle IS 'Droit d''ouvrir/fermer le paiement carte borne depuis le POS. Activé à la main par l''équipe Wello, jamais par le merchant.';
COMMENT ON COLUMN kiosk_settings.card_payment_closed_at IS 'Dernière fermeture du paiement carte depuis le POS. Fermé si dans la journée locale courante, rouvert automatiquement à minuit.';
