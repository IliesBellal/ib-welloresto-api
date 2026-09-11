-- LOT A Semaine 3, Chantier 11a (docs/decisions.md) : "Pro" et "Complet"
-- n'existaient dans `packages` sous aucun nom — décidé avec l'utilisateur de
-- créer deux nouvelles lignes plutôt que de réutiliser Standard/Premium
-- (noms différents, contenu jamais confirmé équivalent).
--
-- stripe_price_id = '' — même convention que les lignes 6 ('Deis'), 102
-- ('Premium Waiter') et 103 ('Pointage') déjà en production : un package
-- sans Price Stripe réel n'est pas une anomalie dans ce schéma. ATTENTION :
-- un vrai Stripe Price doit être créé côté dashboard Stripe et cette colonne
-- mise à jour avant que "Pro" ou "Complet" puisse réellement être facturé —
-- hors de portée de ce chantier (accès Stripe dashboard).
--
-- Indicateurs fonctionnels : posés par analogie avec Standard (Pro) et
-- Premium (Complet), qui sont les lignes existantes les plus proches en
-- ambition — à vérifier/ajuster une fois que le lot B (sélection de modules
-- à la carte) précise comment ces indicateurs doivent réagir au choix de
-- modules du client, plutôt que de rester figés par package.
-- No unique constraint on package_name to key an ON CONFLICT off, so
-- idempotency is a plain existence check instead — safe to run this file
-- twice, unlike a bare "ON CONFLICT DO NOTHING" would be here.
INSERT INTO packages (
    package_name, stripe_price_id, trial_period_days,
    allow_waiter_account, allow_delivery_account, scannorder_ready,
    stock_management, hr_management, planning_enabled, haccp_enabled,
    stock_enabled, scannorder_enabled, bookings_enabled, kiosks_enabled,
    delivery_enabled
)
SELECT v.package_name, v.stripe_price_id, v.trial_period_days,
       v.allow_waiter_account, v.allow_delivery_account, v.scannorder_ready,
       v.stock_management, v.hr_management, v.planning_enabled, v.haccp_enabled,
       v.stock_enabled, v.scannorder_enabled, v.bookings_enabled, v.kiosks_enabled,
       v.delivery_enabled
FROM (VALUES
    ('Pro',     '', 30, true, true, true, 0, false, false, false, false, true, false, false, true),
    ('Complet', '', 30, true, true, true, 0, true,  true,  true,  true,  true, true,  true,  true)
) AS v(package_name, stripe_price_id, trial_period_days, allow_waiter_account, allow_delivery_account,
       scannorder_ready, stock_management, hr_management, planning_enabled, haccp_enabled,
       stock_enabled, scannorder_enabled, bookings_enabled, kiosks_enabled, delivery_enabled)
WHERE NOT EXISTS (SELECT 1 FROM packages p WHERE p.package_name = v.package_name);
