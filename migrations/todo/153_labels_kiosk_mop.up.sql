-- Libellé FR du MOP 'KIOSK' (encaissements Stripe Terminal borne de commande,
-- internal/webhook/stripe/service.go: recordTerminalPayment) dans la table
-- `labels` (label_type='mop'), au même titre que 'CB'/'ES'/'TR'/... déjà
-- présents. Consommé par les rapports comptables/caisse
-- (internal/modules/pos/accounting, internal/modules/pos/reports,
-- internal/modules/cash_registers, internal/modules/orders) via
-- COALESCE(l.label, p.mop) : sans cette ligne, ces rapports affichaient déjà
-- le code brut 'KIOSK' sans casser (fallback existant), juste sans libellé
-- lisible. Voir docs/KIOSK_DECISIONS.md, "Distinction MOP borne (KIOSK) vs
-- carte bancaire (CB)".
INSERT INTO labels (label_value, label_type, lang, label)
SELECT 'KIOSK', 'mop', 'FR', 'Borne de commande'
WHERE NOT EXISTS (
    SELECT 1 FROM labels WHERE label_value = 'KIOSK' AND label_type = 'mop' AND lang = 'FR'
);
