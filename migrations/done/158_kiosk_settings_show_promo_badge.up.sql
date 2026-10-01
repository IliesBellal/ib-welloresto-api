-- Borne : badge « Promo » sur les cartes produit visées par une promotion en
-- cours (GET /kiosk/discounts, promo_product_ids), activable/désactivable
-- depuis la page Paramètres Kiosk du back-office. Actif par défaut, y compris
-- pour les merchants qui ont déjà une ligne kiosk_settings.
--
-- ORDRE DE DÉPLOIEMENT : appliquer cette migration AVANT de déployer le code
-- qui lit/écrit la colonne (Repository.GetSettingsByMerchant/UpsertSettings)
-- — dans l'ordre inverse, GET /kiosk/settings échouerait en erreur SQL.
ALTER TABLE kiosk_settings
    ADD COLUMN IF NOT EXISTS show_promo_badge boolean NOT NULL DEFAULT true;

COMMENT ON COLUMN kiosk_settings.show_promo_badge IS 'Badge « Promo » sur les produits visés par une promotion en cours (borne). Actif par défaut.';
