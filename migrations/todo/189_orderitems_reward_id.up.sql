-- Récompense de fidélité portée par une ligne de commande
-- (docs/ids-entiers-users-discounts-chantier.md, §9).
--
-- Une récompense « produit offert » (customer_rewards, reward_type =
-- 'free_product') était enregistrée dans orderitems.discount_id : deux
-- espaces de numérotation différents dans la même colonne. Un reward_id pouvait
-- alors porter le nom et l'attribution d'une remise sans rapport, entrer dans
-- la charge utile fiscale signée comme une remise, et violer la clé étrangère
-- de discount_redemptions (aucune remise ne porte ce numéro).
--
-- reward_id est le champ propre : discount_id ne contient plus que des
-- remises (discounts.discount_id_new). Pas de clé étrangère : customer_rewards
-- n'est pas rattachée aux autres tables par des FK, et l'ajouter sur
-- orderitems (table la plus chaude du schéma) n'apporte rien ici. Hors charge
-- utile fiscale signée : les lignes signées gardent leur discount_id, la
-- valeur ajoutée n'altère donc aucune signature.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. La lecture des commandes
-- (orders_fetcher_builder) et l'écriture des lignes sélectionnent la colonne.
--
-- Ajout d'une colonne nullable sans valeur par défaut : instantané, mais il
-- prend un verrou ACCESS EXCLUSIVE le temps de son acquisition. orderitems
-- est une table très sollicitée : jouer avec SET lock_timeout = '3s' (et
-- relancer en cas d'échec), jamais pendant un service.

ALTER TABLE orderitems ADD COLUMN IF NOT EXISTS reward_id integer;

COMMENT ON COLUMN orderitems.reward_id IS 'customer_rewards.reward_id de la récompense « produit offert » portée par la ligne. Jamais dans discount_id. Hors charge utile fiscale signée.';
