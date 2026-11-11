-- GÉNÉRÉ par wello-back-office : npm run assistant:seed-sql, puis adapté.
-- Retire les demandes ajoutées par 210 (et leurs éventuels synonymes appris,
-- ON DELETE CASCADE). Les poids ajustés et l'indice « liste » ne sont pas
-- restaurés (sans effet sur le schéma) : pour tout retirer, utiliser
-- 207_assistant_seed.down.sql puis 206_assistant.down.sql.

BEGIN;

DELETE FROM assistant_intents WHERE intent_key IN (
    'orders.open',
    'staff.open',
    'product.open',
    'customers.open'
);

COMMIT;
