-- Distingue le canal d'origine d'une suggestion upsell (POS/SNO/Kiosk) pour
-- les statistiques, désormais que SNO et Kiosk persistent aussi leurs
-- suggestions (Phase C). DEFAULT 'POS' couvre les lignes existantes,
-- toutes créées par le POS jusqu'ici.
ALTER TABLE upsell_suggestions
    ADD COLUMN IF NOT EXISTS channel ENUM('POS','SNO','KIOSK') NOT NULL DEFAULT 'POS';
