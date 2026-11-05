-- Rollback de 189_orderitems_reward_id.up.sql.
-- À jouer APRÈS le retrait du code qui lit ou écrit orderitems.reward_id
-- (orders_fetcher_builder, order_life_cycle). Perd le lien ligne -> récompense
-- des commandes déjà passées.

ALTER TABLE orderitems DROP COLUMN IF EXISTS reward_id;
