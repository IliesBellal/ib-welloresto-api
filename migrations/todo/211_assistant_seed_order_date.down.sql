-- Retire la date de « ouvrir une commande » (retour à 210). Pour tout
-- retirer, utiliser 210 à 207 puis 206_assistant.down.sql.

BEGIN;

UPDATE assistant_intents
SET slots = '[{"name":"number","entity":"number","required":true,"prompt":"Quel est le numéro de la commande ?"},{"name":"channel","entity":"channel","required":false}]'::jsonb,
    examples = '["Ouvre la commande 42098","Retrouve le ticket 1207"]'::jsonb,
    updated_at = now()
WHERE intent_key = 'orders.open';

COMMIT;
