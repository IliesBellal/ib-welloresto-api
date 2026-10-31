# Promotions et TVA des commandes Uber Eats / Deliveroo

> Chantier du 2026-10-29, né de la préparation de la première synchro Pennylane
> (établissement 212, Croq'Ô'Pizzas Metz). Complète `TVA_MARKETPLACE_AUDIT.md`.
> Aucune migration : tout passe par des colonnes existantes.

## 1. Le problème

En comparant les écritures qu'aurait envoyées la synchro comptable aux relevés de
caisse d'octobre, l'écriture de chaque jour portait un écart « ventes − encaissements »
de 20 à 40 % du chiffre d'affaires. Il s'expliquait **au centime près** par le seul
canal Uber Eats.

Cause : une promotion « 1 acheté = 1 offert » sur les pizzas, financée par
l'établissement. Uber envoie les articles au prix de catalogue, la promotion à
part (`payment.promotions`) et le total payé dans
`payment.charges.sub_total_promo_applied`. Nous créions la commande avec ce total et
des lignes au prix de catalogue. Or le ticket (`receipt.GetOrderSaleLines`) et les
clôtures fiscales (`fiscal.ComputeDayClosure`) se reconstruisent à partir des
**lignes** :

- le ticket affichait 13,90 € de total pour des lignes qui en valaient 27,80 € ;
- le chiffre d'affaires et la TVA du canal étaient surévalués d'environ 31 % dans
  les clôtures (octobre : 12 612 € de lignes pour 8 685 € payés, 443 tickets sur
  492 touchés) ;
- `checkOrderConsistency` consignait un écart de prix sur ces commandes.

Deuxième problème, indépendant : les produits créés automatiquement à la réception
d'un article inconnu avaient une TVA fausse.

| plateforme | avant | effet |
|---|---|---|
| Uber Eats | `tva_in_id=5, tva_delivery_id=9, tva_take_away_id=3` | 10 % sur place, mais **20 % en livraison et à emporter** : 9 et 3 sont les catégories « boissons alcoolisées » |
| Deliveroo | aucune catégorie (défaut de table `0`) | « Non défini », **0 %** partout |

## 2. Ce qui a été fait

### 2.1 Remises dans le prix des lignes

Décision (Ilies, 2026-10-29) : **poser la remise dans le prix des lignes**
(`orderitems.price`, le prix de catalogue restant dans `orderitems.base_price`)
plutôt que d'ajouter automatiquement un paiement de remise.

- Les paiements du registre de caisse restent alignés sur la tablette de la
  plateforme ; aucun paiement artificiel.
- Le ticket, les clôtures, la synchro comptable et le contrôle « payé » n'ont pas
  changé : ils lisent déjà `orderitems.price`.
- La TVA se calcule sur le prix réellement payé, au taux de l'article remisé (un
  paiement de remise la répartirait au prorata de tous les taux).
- Le mécanisme est natif : `OrderProductPayload.Price` = prix de catalogue,
  `DiscountedPrice` = prix payé (déjà utilisé par les remises de la caisse).

Code :

- `internal/webhook/marketplacepromo` : répartition pure, sans plateforme.
  - `Allocate` : retire la remise aux unités que la plateforme désigne
    (`discounted_quantity`), dans la limite du prix de base de l'article (les options
    payantes ne sont jamais offertes). Montant exact → méthode `targeted` ; sinon
    au prorata des unités désignées (`targeted_prorata`) ; sinon sur toutes les
    lignes (`spread`).
  - `Prorata` : au prorata de poids donnés, ou du montant des lignes.
  - Une ligne dont les unités ne portent pas la même remise est **scindée**
    (Reine ×1 à 14,50 € et ×1 à 0,00 €) : c'est la seule façon de rester exact au
    centime quand la remise ne se divise pas par la quantité.
  - Garde-fous : une remise supérieure aux lignes est rejetée (lignes inchangées,
    avertissement) ; aucun prix négatif.
- Uber Eats : `service/promotion_mapper.go`, appelé par `handleOrderNotification`.
  Le montant de la remise est `Σ lignes − sub_total_promo_applied` : seule la part
  financée par l'établissement y est déduite (une promotion financée par Uber
  rembourse le restaurant, la vente n'est pas réduite). Les unités désignées
  viennent de `payment.promotions[].discount_items`, uniquement pour les
  promotions financées par l'établissement (`promo_funding_splits`).
- Deliveroo : `deliveroo_orders/promotion.go`, appelé par `ProcessNewOrder`.

Résultat sur la commande réelle 0574D : 74,68 € de lignes pour 46,19 € payés ; la
commande passe de 4 à 6 lignes (Pizza 4 Fromage ×1 + ×1 offerte, Pizza Reine ×1 +
×1 offerte, Chèvre-Miel, Oasis) et les lignes totalisent 46,19 €. Cette commande
est un test du dépôt (`promotion_mapper_test.go`, données du client retirées).

### 2.2 TVA des produits créés automatiquement

`models.MarketplaceTVAFor(alcohol)` (`internal/models/marketplace_tva.go`) :

| | sur place | à emporter | livraison |
|---|---|---|---|
| standard, 10 % | 5 | 2 | 7 |
| boisson alcoolisée, 20 % | 6 | 3 | 9 |

- Uber Eats (`menu.CreateExternalProductTx`) et Deliveroo (`SyncProduct`) utilisent
  la même table.
- Reconnaissance de l'alcool : pour Uber Eats, les étiquettes fiscales de la
  commande (`tax_info.labels` : `CAT_ALCOHOL`, `CAT_BEER`, `CAT_WINE`,
  `CAT_LIQUOR`, `CAT_SPARKLING_WINE`) **quand Uber les envoie** ; sinon, et pour
  Deliveroo, le nom du produit (`models.LooksAlcoholic` : mots entiers, sans accent,
  « sans alcool » et « 0.0 » exclus). Approximatif par nature : le restaurateur
  reste maître du taux final.
- Ni Uber Eats ni Deliveroo n'envoient de taux de TVA (cf. `TVA_MARKETPLACE_AUDIT.md`).
  Le taux est figé sur la ligne à la vente (`orderitems.tva_id`) : corriger un
  produit ne change que les ventes à venir.

### 2.3 Tests

- `marketplacepromo` : BOGO réel, option payante non offerte, prorata, centimes non
  divisibles, remise supérieure aux lignes, invariants (somme exacte, aucun prix
  négatif, quantités conservées).
- Uber Eats : commande 0574D complète, FABD1 (option payante), promotion
  financée par Uber, remise sur le panier, étiquettes fiscales.
- Deliveroo : remise sur un article, sur le panier, sans remise, prix déjà nets,
  total avec frais, quantité non divisible.
- Intégration Postgres (`-tags postgres_integration`, exécutés sur staging) : les
  catégories de TVA des produits créés par Uber Eats (standard et alcool) et par
  Deliveroo.

## 3. Hypothèses et limites connues

- **Deliveroo, non vérifié sur une vraie commande avec promotion.** La
  documentation Deliveroo décrit `item.total_price` « avant remises » et ne dit rien
  de `unit_price`. On suppose `unit_price` avant remise. Si c'est faux, l'écart
  lignes − total vaut 0 et rien ne change (pas de double remise possible). La remise
  n'est posée que si la commande annonce une remise (`discount_amount`,
  `offer_discount` ou `promotions`).
- **Deliveroo, options payantes.** Les lignes utilisent `unit_price` (sans les
  modificateurs) et le ticket exclut les lignes d'options pour Deliveroo. Une
  option payante n'entre donc nulle part dans les lignes, alors que le total la
  contient. Aucun écart en octobre pour l'établissement 212 (pas d'option payante) ;
  à surveiller ailleurs.
- **Uber Eats, `unit_price` comprend les options** (constaté sur FABD1 : 15,89 € =
  13,99 € + 1,90 €). Le plafond de remise par unité est donc `unit_price − options`.
  Si ce n'était pas vrai, la remise retomberait sur la répartition au prorata :
  le total reste exact.
- Une promotion sans `discount_items` (pourcentage ou montant fixe sur le panier)
  est répartie sur toutes les lignes au prorata : les lignes sont moins lisibles
  qu'un « 1 offert » mais les totaux et la TVA sont justes. La méthode utilisée est
  journalisée (`[UBER EATS] remise répartie sur les lignes`, champ `methode`).
- Les promotions d'Uber ne sont renvoyées qu'aux développeurs à qui Uber a activé
  `payment.promotions` : c'est le cas aujourd'hui (constaté sur 0574D).
- **Données passées non corrigées.** Les tickets sont scellés dans la chaîne fiscale.
  Les clôtures d'octobre des établissements qui ont eu des promotions restent
  surévaluées pour le canal concerné. La synchro Pennylane ne part que du jour
  d'activation de la correspondance de comptes (`sync_from`) : corriger avant
  d'activer suffit.

## 4. Reste à faire (par le métier)

1. **Corriger la TVA des produits déjà créés** (tous établissements) — voir les
   requêtes ci-dessous, à valider avec le comptable d'abord.
2. **Demander à Uber l'activation des étiquettes fiscales** (`tax_info.labels`) pour
   l'intégration, afin de ne plus dépendre du nom pour l'alcool.
3. **Valider Deliveroo** sur une vraie commande avec promotion (voir §3).
4. **Nettoyer les doublons** de produits (ex. des dizaines de « Pizza thon » et
   « Pizza saulcy » à 0 %, sans catégorie, établissement 212) : sans rapport avec les
   plateformes.
5. **Après déploiement**, relancer l'extraction par jour : l'écart Uber Eats doit
   tomber près de zéro.

### Requêtes

```sql
-- Ampleur : produits marketplace par catégorie de TVA
SELECT p.category, p.tva_in_id, p.tva_take_away_id, p.tva_delivery_id,
       count(*) AS produits, count(DISTINCT p.merchant_id) AS etablissements
FROM products p
WHERE p.category IN ('UBER_EATS', 'DELIVEROO')
GROUP BY 1, 2, 3, 4
ORDER BY 1, produits DESC;

-- Correctif (après validation comptable) : 10 % partout
UPDATE products
SET tva_in_id = 5, tva_take_away_id = 2, tva_delivery_id = 7
WHERE category IN ('UBER_EATS', 'DELIVEROO');
-- puis remettre à la main les boissons alcoolisées à (6, 3, 9)
```

## 5. Journal des décisions

| date | décision | raison |
|---|---|---|
| 2026-10-29 | Remise dans le prix des lignes, pas en paiement de remise | pas de paiement artificiel dans le registre ; aucun changement au ticket, aux clôtures ni au contrôle « payé » ; TVA au taux de l'article remisé |
| 2026-10-29 | Montant de la remise = lignes − `sub_total_promo_applied`, pas `promo_discount_value` | seule la part financée par l'établissement réduit la vente ; une promotion financée par Uber rembourse le restaurant |
| 2026-10-29 | Scinder les lignes plutôt que d'arrondir | exactitude au centime avec une quantité supérieure à 1 |
| 2026-10-29 | 10 % par défaut, 20 % si alcool reconnu | repas préparés vendus en livraison ou à emporter ; Uber/Deliveroo n'envoient aucun taux |
| 2026-10-29 | Deliveroo : remise posée seulement si la commande annonce une remise | un écart dû à des frais ou à des options payantes ne doit jamais être pris pour une promotion |
