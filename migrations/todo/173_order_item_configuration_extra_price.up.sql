-- Conformité caisse, lot D (docs/attestation-conformite-05-lot-D-brief.md,
-- constat 2) : le surcoût d'une option payante (configurable_attribute_options
-- .extra_price) est facturé au client — la caisse l'ajoute au prix de la
-- ligne, Uber Eats le porte sur la commande — mais n'était enregistré nulle
-- part dans la vente : order_item_configuration ne gardait que l'option et sa
-- quantité. Le ticket (lignes et TVA ventilée) l'ignorait donc.
--
-- extra_price : surcoût unitaire de l'option tel que facturé, figé à
-- l'écriture de la ligne (order_life_cycle.freezeOptionPrice), en centimes.
-- NULL sur les lignes antérieures : les lectures retombent alors sur le prix
-- du catalogue, faute de mieux. Aucune ligne existante n'est modifiée.
--
-- ORDRE DE DÉPLOIEMENT : avant le code du lot D (l'insertion écrit la
-- colonne).
ALTER TABLE order_item_configuration ADD COLUMN IF NOT EXISTS extra_price integer;

COMMENT ON COLUMN order_item_configuration.extra_price IS 'Surcoût unitaire de l''option tel que facturé, figé à l''écriture (centimes) ; NULL avant la migration 173.';
