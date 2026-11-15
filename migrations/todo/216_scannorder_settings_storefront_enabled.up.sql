-- Affichage de la carte ScanNOrder désactivable par l'établissement
-- (back-office : Intégrations > ScanNOrder). Indépendant de la commande en
-- ligne (scannorder_settings.activated) : une carte affichée peut ne pas
-- prendre de commande, voir docs/scannorder-carte-sans-stripe.md.
-- Vrai par défaut : toutes les cartes existantes restent affichées.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (l'API lit la colonne).
ALTER TABLE scannorder_settings
    ADD COLUMN IF NOT EXISTS storefront_enabled BOOLEAN NOT NULL DEFAULT TRUE;
