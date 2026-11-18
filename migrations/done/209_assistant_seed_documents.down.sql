-- Retire la demande « documents à télécharger » ajoutée par 209 (et ses
-- éventuels synonymes appris). Le reste de 209 met à jour 207 : pour tout
-- retirer, utiliser 207_assistant_seed.down.sql puis 206_assistant.down.sql.
DELETE FROM assistant_intents WHERE intent_key = 'documents.download';
