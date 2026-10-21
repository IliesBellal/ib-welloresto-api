package models

// RewardLineInDiscountID règle la transition « récompense de fidélité » entre
// l'API et les POS déjà installés (docs/ids-entiers-users-discounts-chantier.md, §9).
//
// Une ligne « produit offert » porte son identifiant de récompense dans
// reward_id (orderitems.reward_id). Les POS qui ne connaissent pas encore ce
// champ s'appuient sur discount_id pour deux choses : ne pas fusionner la ligne
// offerte avec une ligne payée du même produit (cart_merge_mixin.dart compare
// discountId), et la renvoyer à l'identique quand ils rechargent une commande.
//
// Tant que true, le pricing et la lecture des commandes renvoient donc AUSSI
// l'identifiant de récompense dans discount_id, comme avant. À l'écriture, il
// n'est jamais enregistré dans orderitems.discount_id (voir
// OrdersLifeCycleRepository.resolveLineDiscounts) : la base, la clôture
// fiscale et les statistiques sont propres dès le premier déploiement.
//
// Passer à false, puis supprimer la constante et ses branches, une fois tous
// les POS et ScanNOrder à jour (suivi de version des tablettes :
// users_devices.app_version).
const RewardLineInDiscountID = true
