-- LOT B B2c-0 (docs/decisions.md) : pricing_catalog gagne son propre
-- stripe_price_id — distinct de packages.stripe_price_id (existant,
-- kind=plan uniquement, et déjà documenté comme non fiable : le seul id
-- réellement peuplé ("Essentiel", packages.id=1) s'est avéré être un objet
-- LIVE MODE, inutilisable avec la clé test de vérification de ce chantier,
-- et Pro/Complet (packages.id=108/109) ont stripe_price_id='' depuis leur
-- création — voir migration 134). pricing_catalog.stripe_price_id couvre
-- plan ET module (pas seulement plan comme packages), et n'est peuplé
-- qu'avec de vrais Price Stripe créés en mode test par cmd/ensure_stripe_prices
-- (voir docs/decisions.md pour les identifiants obtenus).
ALTER TABLE pricing_catalog
    ADD COLUMN IF NOT EXISTS stripe_price_id text,
    ADD COLUMN IF NOT EXISTS per_unit_stripe_price_id text;
COMMENT ON COLUMN pricing_catalog.stripe_price_id IS 'NULL = pas encore de Price Stripe réel pour ce code — la construction/mise à jour de l''abonnement Stripe récurrent (B2c-0) échoue explicitement plutôt que d''improviser un montant. Renseigné par cmd/ensure_stripe_prices, jamais à la volée par le code de service.';
COMMENT ON COLUMN pricing_catalog.per_unit_stripe_price_id IS 'planning uniquement : le Price Stripe du tarif par salarié (subscription_items.planning_employee), distinct de stripe_price_id qui porte le montant fixe mensuel du module planning lui-même.';
