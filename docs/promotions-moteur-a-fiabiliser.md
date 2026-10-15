# Moteur de promotions — points à fiabiliser

**Statut :** constats et décisions du 2026-10-15, **rien n'est développé**. Ce sera l'objet d'une session dédiée, à mener **avant** la création de promotions par l'import de carte photo (voir § 5).

Périmètre : `applyDiscounts` ([orders/service.go](../internal/modules/orders/service.go)), appelée par `ComputePricing`, le calcul de prix commun à la caisse, à la borne et au Scan&Order. À la date du constat, aucune copie du calcul n'a été trouvée côté applications Flutter (recherche de `discounted_quantity` / `discountedQuantity`) : la correction se fait à un seul endroit. Les tests existants sont dans [orders/discounts_pricing_test.go](../internal/modules/orders/discounts_pricing_test.go).

---

## 1. Fonctionnement actuel (vérifié dans le code)

- Le panier est éclaté en **une ligne par unité**, dans l'ordre reçu (`buildSelectedProducts`). Aucun tri.
- Pour chaque promotion (par `prefered_order`), `applyDiscounts` :
  1. filtre par mode de commande, code promo, cumulabilité, `discounted_quantity > 0` ;
  2. compte les unités éligibles (produit ciblé ou aucun ciblé, options obligatoires, prix saisi pour `NEWPRICE`) ;
  3. vérifie le minimum : `QUANTITY` sur les unités **éligibles**, `CURRENCY` sur le **panier entier** (`baseTotal`) ;
  4. détermine `n` : `discounted_quantity` si non cumulable ; `éligibles − (éligibles % discounted_quantity)` si cumulable ;
  5. remise les `n` premières unités éligibles, **dans l'ordre du panier**.
- Le calcul porte sur le prix de base de l'unité (`sp.Price`). Les suppléments d'options sont comptés à part dans les totaux (`computeTotals`) et ne sont pas remisés, sauf prix d'option promotionnel (`discounts_products_options.new_price`, `applyPromoOptionPrices`).

## 2. Défauts constatés

### P1 — L'unité remisée dépend de l'ordre du panier
La première unité éligible reçoit la remise, pas la moins chère. « 1 offerte à partir de 4 » peut offrir la pizza la plus chère, et le résultat peut différer entre caisse, borne et Scan&Order selon l'ordre d'ajout.

### P2 — Un nouveau prix peut augmenter le prix
En `NEWPRICE`, le prix calculé est le prix saisi, sans comparaison avec le prix de l'unité : un 9 € sur une pizza à 8 € la facture 9 €. Seul le négatif est écarté.

### P3 — Pas de vrai « N achetés, 1 offert »
- Non cumulable : la promotion ne joue qu'**une fois par commande** (8 pizzas : 1 offerte, pas 2).
- Cumulable par paliers : remise **toutes** les unités des groupes complets. Un test le fige : 5 unités, quantité 2, 50 % → 4 unités remisées (`cumulable par paliers de 2 : 4 unités remisées sur 5`). Ce n'est pas « la 4ᵉ offerte ».

### P4 — Effet sur l'existant
Toute correction change le comportement des promotions déjà configurées (notamment celles dont `discounted_quantity` est inférieur au nombre d'unités éligibles).

## 3. Décisions

| # | Décision | Date |
|---|---|---|
| M1 | **L'unité remisée est celle qui coûte le moins au commerçant**, c'est-à-dire celle dont la remise est la plus petite. En égalité, l'ordre du panier tranche (résultat déterministe). | proposée 2026-10-15 |
| M2 | **Le classement se fait sur le prix de base de l'unité**, sans les suppléments d'options, puisque ceux-ci ne sont pas remisés. Exemple : pizza A 10 € + supplément 2 €, pizza B 11 € sans supplément. Pour une pizza offerte, c'est A qui est offerte (10 € < 11 €) ; le client paie ensuite les 2 € de supplément de A. | **confirmée par Ilies, 2026-10-15** |
| M3 | **Une unité offerte paie quand même ses suppléments.** | **décidée par Ilies, 2026-10-15** |
| M4 | **Le minimum en euros se compare au panier entier**, comme aujourd'hui. Pas de changement. | **décidée par Ilies, 2026-10-15** |
| M5 | **Un nouveau prix ne s'applique jamais s'il ne baisse pas le prix** : les unités dont la remise serait ≤ 0 sont ignorées (corrige P2, et découle de M1). | proposée 2026-10-15 |
| M6 | **Mode « N achetés, 1 offert »** : une unité remisée par groupe complet de N+1 unités éligibles, soit `floor(éligibles / (N+1))`, la moins chère de chaque groupe (corrige P3). À ajouter comme comportement explicite, sans réinterpréter les promotions « cumulables » existantes. | proposée 2026-10-15 |

M2 est confirmée. M1, M5 et M6 sont des **propositions** non encore validées comme conception détaillée : à confirmer au début de la session dédiée.

## 4. À prévoir pour la session dédiée

- **Tests** : indépendance à l'ordre du panier (panier mélangé, même total) ; unité la moins chère remisée ; égalités ; nouveau prix supérieur au prix ; paliers N+1 (4, 5, 8, 9 unités) ; suppléments toujours facturés sur l'unité offerte ; minimum en euros sur le panier entier.
- **Avant déploiement** : requête **en lecture seule sur la production** pour recenser les promotions actives dont le résultat change (non cumulables avec `discounted_quantity` inférieur aux unités éligibles possibles, cumulables par paliers). Staging n'est pas représentatif.
- **Prévenir les commerçants** concernés : l'article remisé change.
- Vérifier que les listes de promotions de la borne et du Scan&Order (`kiosk/discounts_list_test.go`, `scannorder/discounts_list_test.go`) n'annoncent rien qui contredise le nouveau calcul.

## 5. Lien avec l'import de carte par photo

**Mise à jour du 2026-10-15 :** l'import photo crée désormais les promotions lues sur la carte **avec le moteur tel qu'il est** (voir [import-carte-ia-03-porte-ia.md](import-carte-ia-03-porte-ia.md) § 12). Elles sont toujours créées **désactivées**, et leurs limites sont signalées à la relecture. Cette session moteur lèvera ces limites :

- M1/M6 : « N-ième offert » joue aujourd'hui une fois par commande sur le premier article du panier ; la promotion importée porte déjà une note qui l'indique, à retirer quand le moteur sera corrigé ;
- M5 : l'import retire déjà du ciblage les produits qu'un prix fixe n'abaisserait pas ; la garde du moteur le rendra redondant, pas inutile.

Décision d'Ilies du 2026-10-15 : M2 (classement sur le prix de base) est confirmée.
