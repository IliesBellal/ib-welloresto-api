# Chantier moteur de promotions — décisions, spécification, plan de tests

**Statut (2026-10-17) :** phase de cadrage, export de production reçu et analysé (§ 10), liste de préparation au § 11. **Aucun code du moteur n'est modifié.** Ce document fige les décisions prises avec Ilies, la règle cible, le protocole d'export des promotions de production, le plan de tests et les points encore ouverts. Il complète [promotions-moteur-a-fiabiliser.md](promotions-moteur-a-fiabiliser.md) (constats P1 à P4 et premières décisions M1 à M6 du 2026-10-15), qui reste la référence sur l'état actuel du moteur.

Périmètre : `applyDiscounts` ([orders/service.go](../internal/modules/orders/service.go)), appelée par `ComputePricing` (caisse, borne, Scan&Order). Les promotions affichées par la borne et le Scan&Order dupliquent les règles d'éligibilité ([kiosk/service.go](../internal/modules/kiosk/service.go), [scannorder/repository.go](../internal/modules/scannorder/repository.go)) : elles doivent rester cohérentes avec le nouveau calcul.

---

## 1. Objectifs

1. **Corriger le moteur** pour qu'il se comporte comme les caisses du commerce (Square, caisses de restauration rapide) : l'article remisé est le moins coûteux pour le commerçant, le résultat ne dépend pas de l'ordre du panier, une promotion « N achetés, 1 offert » se répète, un prix fixe n'augmente jamais un prix.
2. **Garantir que les promotions actuellement en production restent actives et fonctionnelles**, par des tests construits sur leur configuration réelle (§ 5 et § 6), et recenser celles dont le résultat change.

Principe de la mise en production (décision D4) : **un seul comportement, le bon, pour toutes les promotions**, avec avertissement préalable des commerçants dont une promotion change de résultat. Pas de second moteur « historique ».

## 2. Décisions

| # | Décision | Origine | Date |
|---|---|---|---|
| D1 | **L'unité remisée est celle dont la remise est la plus petite** (la moins chère pour un pourcentage ou une offerte). En égalité, l'ordre du panier tranche. Le classement se fait sur le **prix de base** (M1 + M2). | Ilies (« la moins chère ») | 2026-10-16 |
| D2 | **Une promotion cumulable se répète, une promotion non cumulable ne joue qu'une fois par commande.** « Si la promo n'est pas marquée non cumulable, elle peut se cumuler elle-même plusieurs fois. » | Ilies | 2026-10-16 |
| D3 | **Modèle de groupes** (précise D2) : taille de groupe = `max(quantité minimum, quantité remisée)` ; la « quantité remisée » est le nombre d'unités remisées **par groupe**. Cumulable : `floor(unités éligibles / taille du groupe)` groupes. Non cumulable : un seul groupe. Voir § 3. | proposé et validé par Ilies | 2026-10-16 |
| D4 | **Corriger partout et prévenir** les commerçants concernés (pas de colonne « mode de calcul », pas d'ancien moteur conservé). | Ilies | 2026-10-16 |
| D5 | **Les unités remisées sont les `n` plus petites remises** quand plusieurs unités sont éligibles (8 pizzas de 8 à 15 €, 2 offertes : celles à 8 et 9 €). | Ilies | 2026-10-16 |
| D6 | **Une promotion ne remise jamais une unité dont elle ne baisse pas le prix** (un prix fixe supérieur ou égal au prix de l'unité ne la touche pas) ; la promotion ne consomme pas de quantité remisée sur elle (M5). Précisé par D12 : cette unité compte quand même comme « achetée ». | Ilies | 2026-10-16, précisé 2026-10-18 |
| D7 | **Un pourcentage ne s'applique qu'au prix de base**, jamais aux suppléments d'options. Une unité offerte paie quand même ses suppléments (M3). Aucun changement par rapport à aujourd'hui. | Ilies | 2026-10-16 |
| D8 | **Le minimum en euros se compare au panier entier** (M4). Inchangé. | Ilies | 2026-10-15 |
| D9 | **Cumul entre promotions : l'ordre de priorité (`prefered_order`) est conservé.** Une promotion non cumulable appliquée bloque les suivantes ; un article déjà remisé n'est jamais remisé deux fois. On le teste et on le fige, sans le changer. | Ilies | 2026-10-16 |
| D10 | **Remises sur le total du panier (`ORDER_TOTAL`, plafonds `max_redemptions`) : hors du cœur du chantier**, documentées au § 7 et traitées plus tard dans la session. | Ilies | 2026-10-16 |
| D11 | **Export de la production par requête SQL en lecture seule** exécutée par Ilies, résultat transmis, convertie en tests « golden » (§ 5). | Ilies | 2026-10-16 |
| D12 | **Une unité que la promotion n'abaisse pas (D6) compte comme « achetée » mais n'est jamais remisée.** Elle reste à son prix, compte pour le minimum en quantité et pour les groupes, et ne consomme pas la quantité remisée. Exemple « 3 achetés, le 4ᵉ à 5 € » avec des articles à 4, 5, 6 et 7 € : les quatre comptent pour le minimum, seul celui à 6 € passe à 5 € (le moins touché parmi ceux que la promotion abaisse). *Remplace la version du 2026-10-17, qui excluait ces unités du minimum et aurait privé le client d'une offre méritée.* | Ilies | 2026-10-18 |
| D13 | **Une unité déjà remisée par une promotion précédente ne compte ni pour le minimum, ni pour les groupes de la promotion suivante** (Q2, pratique usuelle des moteurs de promotions : une unité « consommée » par une offre ne sert pas de seuil à une autre). **Proposée, en attente de confirmation explicite.** Aucun effet sur la production actuelle (§ 10) : les seules promotions qui se chevauchent sont toutes non cumulables, donc la première appliquée bloque les suivantes. | proposée | 2026-10-17 |
| D14 | **La règle « unité la moins chère » s'applique aussi aux récompenses de fidélité `free_product`** (P5, Q3). À faire dans le même chantier, après le cœur du moteur de promotions. | Ilies (« Oui ») | 2026-10-17 |
| D15 | **Arrondi du pourcentage (entier tronqué sur le prix unitaire) et plafond par unité : conservés** (Q4). | Ilies (« Oui ») | 2026-10-17 |
| D16 | **Aucun recalcul de promotion côté applications** (caisse, borne, Scan&Order) : le calcul reste uniquement dans `ComputePricing` (Q5). | Ilies (« Non ») | 2026-10-17 |
| D17 | **Les remises au niveau de la commande (exemple : 5 € offerts sur la 10ᵉ commande) sont un second temps**, avec vérification préalable du paiement Stripe Checkout et du terminal de paiement (§ 12). On y reviendra plus tard. | Ilies | 2026-10-17, confirmé 2026-10-18 |
| D18 | **Pas d'exception pour les formules à 0 € ciblées par un prix fixe** (Q6) : D6 et D12 s'appliquent partout. Une formule à 0 € compte comme achetée, n'est jamais remisée, et ne passe donc plus de 0 € au prix fixe. « Tant pis » pour l'ancien comportement, **mais** les commerçants concernés (promotions 3 et 5 à 7 dans l'export) sont prévenus avant le déploiement pour saisir le prix de la formule dans le catalogue. Réserve : la requête F (prix réellement facturés) mesure ce que cela leur coûte. | Ilies | 2026-10-18 |

## 3. Règle cible (spécification de référence)

Pour chaque promotion, dans l'ordre de `prefered_order` (les filtres d'éligibilité globaux ne changent pas : mode de commande, code promo, cumulabilité avec une promotion non cumulable déjà appliquée, `discounted_quantity > 0`, produit ciblé, options obligatoires, prix saisi pour `NEWPRICE`) :

1. **Remise unitaire.** Pour chaque unité éligible non encore remisée, calculer son prix remisé comme aujourd'hui (pourcentage en arithmétique entière tronquée, montant fixe, prix fixe, plafond `MaxDiscountValue` appliqué par unité, plancher à 0) et la **remise** `= prix de base − prix remisé`.
2. **Unités remisables (D6/D12).** Parmi les unités éligibles, celles dont la remise est > 0 sont les **candidates**. Les autres (remise ≤ 0) restent éligibles : elles comptent comme « achetées » mais ne sont jamais remisées.
3. **Minimum.** `QUANTITY` : nombre d'unités **éligibles** (candidates ou non) ≥ minimum. `CURRENCY` : panier entier ≥ minimum (D8). Aucun minimum : toujours vrai.
4. **Nombre d'unités remisées `n`.**
   - Taille de groupe `G = max(minimum en quantité, quantité remisée)` ; si le minimum est en euros ou absent, `G = quantité remisée`.
   - Non cumulable : `n = min(quantité remisée, unités éligibles)`.
   - Cumulable : `n = floor(unités éligibles / G) × quantité remisée`.
   - Nombre réellement remisé : `min(n, nombre de candidates)`.
5. **Choix des unités (D1/D5).** Remiser, parmi les candidates, celles à la plus petite remise ; en égalité, l'ordre du panier.

Cas de contrôle (tous les prix de base, sans suppléments) :

| Promotion | Panier | Aujourd'hui | Cible |
|---|---|---|---|
| « 3 achetées, la 4ᵉ offerte » (minimum 4, remisée 1, 100 %), **non cumulable** | 8 pizzas de 8 à 15 € | 1 offerte : la 1ʳᵉ du panier | 1 offerte : celle à 8 € |
| idem, **cumulable** | 8 pizzas de 8 à 15 € | **toutes** les unités remisées (n = 8 − 8 % 1) | 2 offertes : 8 € et 9 € |
| idem, cumulable | 4, 5, 8, 9 pizzas | toutes | 1, 1, 2, 2 offertes |
| « 2 pizzas à −50 % » (minimum 2 ou vide, remisée 2), cumulable | 5 pizzas | 4 unités à −50 % | 4 unités à −50 % (les 4 moins chères) |
| idem, non cumulable | 5 pizzas | 2 unités | 2 unités (les 2 moins chères) |
| « 1 offerte dès 3 pizzas » non cumulable | 9 €, 11 €, 14 € | la 1ʳᵉ du panier | celle à 9 € |
| Prix fixe 9 € sur une pizza à 8 € | 1 pizza | facturée 9 € | reste à 8 € |
| « 3 achetés, le 4ᵉ à 5 € » (minimum 4, remisée 1, prix fixe 5 €), non cumulable | 4, 5, 6 et 7 € | la 1ʳᵉ du panier (4 € → 5 €) : total 23 € au lieu de 22 € | les quatre comptent, celui à 6 € passe à 5 € : total 21 € |
| idem | 4, 5, 5, 5 € | la 1ʳᵉ du panier passe à 5 € : total 20 € au lieu de 19 € | aucune candidate : rien n'est remisé, 19 € |
| idem, **cumulable** | 8 articles dont 3 à 4 ou 5 € et 5 à 6 € ou plus | toutes les unités passent à 5 € | 2 groupes, 2 unités remisées parmi les candidates (les moins touchées) |
| Formule à 0 € ciblée par un prix fixe 6 € (min. 2, remisée 2, cumulable) | 1 formule + 1 pizza à 8,70 € | la formule passe à 6 € et la pizza à 6 € | la formule compte, reste à 0 € ; la pizza passe à 6 € (D18) |
| −20 % sur une pizza 10 € + supplément 2 € | 1 pizza | 8 + 2 = 10 € | 8 + 2 = 10 € (inchangé, D7) |

Promotions existantes dont le résultat **ne change pas** : cumulable avec minimum ≤ quantité remisée ou minimum en euros (même résultat qu'aujourd'hui sauf choix de l'unité), non cumulable ciblant un seul produit à prix unique. Résultat qui **change** : choix de l'unité (P1, toutes les promotions où il y a plus d'unités éligibles que d'unités remisées), cumulable avec minimum en quantité > quantité remisée, prix fixe supérieur au prix (P2).

## 4. Constats complémentaires (vérifiés dans le code, 2026-10-16)

Ajoutés à P1 à P4 du document précédent.

- **P5 — Les récompenses de fidélité ont le même défaut que P1.** `applyRewards` ([orders/service.go](../internal/modules/orders/service.go)) offre, pour `free_product`, le **premier** produit du panier qui figure dans la liste et n'est pas déjà remisé, pas le moins cher. À décider (§ 8, Q3).
- **P6 — Le minimum compte les unités déjà remisées.** `countEligible` compte toutes les unités éligibles sans tenir compte de `DiscountedPrice`, alors que l'application, elle, saute les unités déjà remisées. Avec deux promotions sur les mêmes produits, le minimum de la seconde peut être satisfait par des unités que la première a déjà prises. À décider (§ 8, Q2).
- **P7 — Les remises sur le total du panier ne sont pas calculées** et ne sont pas filtrées : voir § 7.
- **P8 (sans effet pratique) —** `GetDiscountProducts` ne filtre pas `discounts_products.enabled`. L'écran d'édition supprime les lignes (`DELETE`), donc aucune ligne désactivée n'existe en pratique ; seule la duplication de produit insère avec `enabled = TRUE`. À aligner par cohérence quand on touche à la requête.
- **Rappel des tests existants :** [orders/discounts_pricing_test.go](../internal/modules/orders/discounts_pricing_test.go) (~28 cas), [orders/discount_options_test.go](../internal/modules/orders/discount_options_test.go), intégration Postgres ([orders/discounts_postgres_integration_test.go](../internal/modules/orders/discounts_postgres_integration_test.go)), listes borne et Scan&Order. Plusieurs figent le comportement actuel qui va changer (par exemple « cumulable par paliers de 2 : 4 unités remisées sur 5 » reste vrai, mais le choix des unités change) : à relire cas par cas, sans les supprimer, en notant pour chacun s'il reste vrai.

## 5. Protocole d'export de la production (D11)

1. Ilies exécute [diagnostic-promotions-prod.sql](diagnostic-promotions-prod.sql) sur la production, en lecture seule (la transaction est `READ ONLY` puis `ROLLBACK`). Les 5 requêtes ont été validées sur staging (syntaxe et colonnes).
2. Le résultat de chaque requête (A à E) est exporté en CSV et transmis. Aucune donnée personnelle : pas de nom de promotion, de produit ni de marchand, pas de code promo, pas de client ; le marchand est une empreinte courte (`merchant_ref`).
   - **A** : une ligne par promotion, avec son état (`live`, `inactive`, `expired`, `future`), son usage sur 90 jours et les indicateurs d'impact (`flag_cumulable_min_sup_remisee`, `flag_choix_unite_peut_changer`, `flag_nouveau_prix_non_inferieur`, `flag_portee_panier`, `flag_quantite_remisee_nulle`).
   - **B** : produits ciblés et prix du catalogue sur les trois canaux. **C** : contraintes d'options. **D** : créneaux horaires. **E** : vue d'ensemble par forme de promotion.
3. Les fichiers reçus sont rangés dans `docs/` ou dans un dossier de fixtures de test, **jamais** laissés avec des identifiants de marchand en clair.
4. Les prix sont ceux du catalogue d'aujourd'hui (les paniers de test sont fabriqués avec, pas avec de vraies commandes).

## 6. Plan de tests

Ordre imposé : **les tests de caractérisation d'abord, le moteur ensuite.**

**Phase 0 — Caractérisation de l'existant (avant toute modification).**
- Pour chaque promotion `live` de l'export, fabriquer des paniers types à partir de ses produits et prix réels : un panier qui qualifie, un panier juste en dessous du minimum, un panier mélangé (plusieurs prix), le même panier dans un ordre différent.
- Enregistrer le **résultat du moteur actuel** (total, unités remisées) comme référence « avant ». Ces tests passent sur le moteur actuel.
- Tests Postgres d'intégration par `ComputePricing` (balise `postgres_integration`, contre staging) pour une promotion de chaque forme : pourcentage, montant fixe, prix fixe, N-ième offert, par mode de commande, par créneau horaire, avec code.

**Phase 1 — Tests de la règle cible (rouges d'abord).**
- Indépendance à l'ordre du panier (panier mélangé, même total) ; unité la moins chère remisée ; égalités ; prix fixe supérieur au prix ; paliers 4, 5, 8, 9 unités cumulable et non cumulable ; modèle de groupes (D3) ; suppléments toujours facturés sur l'unité offerte ; minimum en euros sur le panier entier ; ordre de priorité et blocage par une promotion non cumulable (D9, figé).

**Phase 2 — Différentiel avant/après.**
- Rejouer les paniers de la phase 0 sur le moteur corrigé. Toute différence doit être **listée et justifiée** (liste blanche : choix de l'unité, P2, minimum > quantité remisée). Toute autre différence est un bug.
- Produire pour chaque marchand concerné la liste « promotion, ancien résultat, nouveau résultat », base du message d'avertissement.

**Phase 3 — Avant déploiement.**
- Relancer l'export sur la production et le différentiel sur les données fraîches. Vérifier que les listes borne et Scan&Order n'annoncent rien qui contredise le calcul (`kiosk/discounts_list_test.go`, `scannorder/discounts_list_test.go`). Prévenir les commerçants.

Après correction : retirer la note `promotionEngineNthFreeNote` de [importer/ai_promotions.go](../internal/modules/menu/importer/ai_promotions.go) (limite levée), **revoir** le retrait, par l'import, des produits dont le prix fixe n'abaisse pas le prix : avec D12 ces produits comptent comme « achetés » dans « N achetés, 1 à X € », donc les retirer du ciblage leur ferait perdre ce rôle (la garde du moteur suffit, le retrait devient contre-productif), et mettre à jour [import-carte-ia-03-porte-ia.md](import-carte-ia-03-porte-ia.md) D26 et D27.

## 7. Hors cœur du chantier : remises sur le total du panier (D10)

La table `discounts` porte `discount_scope` (`PRODUCT` ou `ORDER_TOTAL`), `max_redemptions` et `max_redemptions_per_customer` (migration 041), et `discount_redemptions` sait tracer une utilisation de portée `CART`. Mais :
- `applyCartDiscount` n'a jamais été écrite ([decisions.md](decisions.md) : « une fonctionnalité jamais construite ») ;
- `GetDiscounts` ne filtre pas la portée : une promotion `ORDER_TOTAL` qui arriverait à la base serait traitée comme une promotion produit ;
- les plafonds d'utilisation ne sont lus nulle part dans le calcul de prix.

L'export (colonne `flag_portee_panier`) dit s'il en existe en production. À traiter plus tard dans la session : au minimum un test qui verrouille le comportement (ignorer ou refuser proprement), ensuite, si besoin produit, la fonctionnalité.

## 8. Questions encore ouvertes

| # | Question | Ma recommandation |
|---|---|---|
| Q1 | Unité que la promotion n'abaisse pas (D6) : compte-t-elle pour le minimum ? | **Tranchée (D12, révisée le 2026-10-18)** : oui, elle compte comme achetée mais n'est jamais remisée. |
| Q2 | P6 : une unité déjà remisée compte-t-elle pour la promotion suivante ? | **Proposée (D13)** : non. À confirmer ; sans effet sur la production actuelle. |
| Q3 | P5 : règle « moins chère » pour les récompenses `free_product` ? | **Tranchée (D14)** : oui. |
| Q4 | Arrondi du pourcentage et plafond par unité : conservés ? | **Tranchée (D15)** : oui. |
| Q5 | Les applications recalculent-elles une promotion localement ? | **Tranchée (D16)** : non. |
| **Q6** | **Formules à 0 € ciblées par un prix fixe.** Deux commerçants ciblent par un prix fixe un produit « formule » (`is_product_group`) dont le prix catalogue est 0 € (produit 587 : 0 € ; produit 1541 : 0 €). Aujourd'hui le moteur facture alors la formule au prix fixe (6 € ou 5 €), donc **une hausse de 0 € à 6 €**. D6 (« un prix fixe n'augmente jamais un prix ») supprimerait ce comportement. Est-ce une anomalie ou la façon dont ces commerçants vendent leur formule ? | **Tranchée (D18, 2026-10-18)** : issue (c), D6 et D12 s'appliquent partout, sans exception pour les formules ; les commerçants concernés sont prévenus et saisissent le prix de la formule dans le catalogue. Reste à mesurer, avec la requête F, ce que l'ancien comportement leur rapportait. |

## 10. Promotions en production (export du 2026-10-17)

Résultats des requêtes A à E reçus. **B et C sont tronquées** (promotion 9 : 25 produits sur 34 et 5 contraintes d'options sur 34 ; promotion 10 : absente des deux) : la section G de [diagnostic-promotions-prod.sql](diagnostic-promotions-prod.sql) les relance pour ces deux promotions, et la section F répond à Q6.

### 10.1 Vue d'ensemble

8 promotions non supprimées, 3 commerçants (empreintes `24a40264`, `776f8f8e`, `d6a6e3ef`) : **7 actives, 1 expirée**. Aucune promotion `CURRENCY`, aucun code promo, aucune `ORDER_TOTAL`, aucune quantité remisée nulle, aucune « cumulable avec minimum > quantité remisée ». Tout est en `PRODUCT`, et 7 sur 8 en prix fixe (`NEWPRICE`). **Seules deux promotions ont servi sur les 90 derniers jours** : la 3 et la 4.

| Promo | Commerçant | Forme | Usage 90 j | Remises 90 j |
|---|---|---|---|---|
| 3 | `776f8f8e` | Prix fixe 6,00 €, cumulable, minimum 2, remisée 2, sur place et à emporter, lundi au samedi 10:45–14:00, 30 produits | **511** | **1 474,60 €** |
| 4 | `24a40264` | Prix fixe 6,00 €, cumulable, minimum 2, remisée 2, tous modes, lundi au vendredi 08:45–13:00, 3 produits | 14 | 65,70 € |
| 5, 6, 7 | `d6a6e3ef` | Prix fixe saisi 6,00 / 5,00 / 5,00 €, non cumulable, minimum 1, remisée 1, mercredi et vendredi 15:00–23:59, 34 produits, **une option obligatoire différente par promotion** (2174, 2176, 2178) | 0 | 0 |
| 8 | `d6a6e3ef` | Prix fixe 0 € (« 4 articles, 1 offert »), à emporter, lundi, minimum 4, remisée 1, non cumulable, **options à 0 €** (3 options non obligatoires par produit) | 0 | 0 |
| 10 | `d6a6e3ef` | Prix fixe (`discount_value` 3,00 €, prix saisi non reçu : export tronqué), à emporter, lundi, minimum 3, remisée 1, non cumulable | 0 | 0 |
| 9 | `d6a6e3ef` | −50 %, à emporter, tous les jours, minimum 2, remisée 1, non cumulable, option 2176 obligatoire à 2,00 € | 0 | 0 |
| 92 | `24a40264` | −25 %, sur place, cumulable, 6 produits | 0 | 0 |

Le commerçant `d6a6e3ef` a donc une **échelle du lundi à emporter** : 2 articles → promotion 9, 3 articles → promotion 10, 4 articles → promotion 8, chacune non cumulable (la première appliquée bloque les suivantes). Les montants des promotions 5, 6, 7 et 10 sont à confirmer avec les exports complets.

### 10.2 Ce que la correction change pour eux

- **Promotions 3 et 4 (cumulables, minimum = quantité remisée = 2)** : même groupe qu'aujourd'hui (D3). Le résultat est **identique** avec 2, 4, 6… articles éligibles. Avec un nombre **impair** (3, 5…), un article reste au prix normal : aujourd'hui c'est le dernier du panier, demain **le plus cher** des articles éligibles (les moins chers sont remisés). C'est le seul changement, et il touche la promotion qui rapporte le plus (511 utilisations). Note : mon indicateur `flag_choix_unite_peut_changer` ne couvre que les promotions non cumulables, il ne les signalait pas.
- **Promotions 5, 6, 7, 8, 9, 10 (non cumulables, 1 article remisé)** : l'article remisé passe du premier du panier au **moins cher** (par exemple un article à 8,00 € et un à 10,00 € : celui à 8,00 € est remisé). Aucune n'a servi en 90 jours.
- **Promotion 8, options à 0 €** : l'article offert garde ses options à 0 € via `discounts_products_options.new_price` (`applyPromoOptionPrices`). C'est un **prix d'option promotionnel explicite**, distinct des suppléments « ordinaires » de la décision D7 (qui restent facturés sur une unité offerte). À conserver et à tester : D7 ne porte que sur les suppléments sans prix promotionnel.
- **Promotion 92** : jamais applicable, `valid_from` (2026-06-26) **postérieure** à `valid_to` (2026-04-30), probablement une année oubliée. Sans effet sur le moteur, à signaler au commerçant.

### 10.3 Observations de données (sans effet sur le moteur)

- Pour `NEWPRICE`, `discount_value` n'est pas utilisé (le back-office désactive la saisie et renvoie à l'onglet Produits) ; les valeurs en base sont obsolètes ou incohérentes (promotion 5 : 200 contre 600 saisis ; promotion 7 : 300 contre 500). Le prix lu est `discounts_products.new_price`.
- Produits ciblés mais non disponibles (69, 547, 589, 726, 727) : inertes. Le produit 547 a un prix sur place de 9 centimes (coquille probable ; il est indisponible).
- Horaires : le commentaire de colonne dit « UTC », mais le moteur compare à l'heure **locale** du marchand ; les valeurs de l'export (10:45–14:00) sont bien des heures locales.

### 10.4 Cas « golden » à fabriquer (phase 0)

Pour chaque promotion active, avec les prix réels de l'export : (a) panier qui qualifie, (b) juste sous le minimum, (c) panier à prix mélangés dans deux ordres différents, (d) hors créneau (jour, heure, mode de commande), (e) nombre impair d'articles pour les cumulables.
Cas particuliers imposés par l'export :
- promotion 3 : 1, 2, 3, 4, 5 articles aux prix 790 / 870 / 990 ; formule 587 seule et avec un autre article (Q6) ; livraison (non éligible) ; dimanche (hors créneau) ; 14:00 pile (borne de fin) ;
- promotion 4 : produits 67, 68 (prix différents par canal : 1200 / 1290 / 1320 et 1380 / 1380 / 1420) ; produit 69 indisponible ;
- échelle du lundi `d6a6e3ef` : 2, 3, 4, 5 articles aux prix 800 / 800 / 1000 / 1100 ; blocage en chaîne entre les trois promotions ; article offert avec ses options à 0 € (promotion 8) ;
- promotions 5, 6, 7 : un article avec l'option 2174, un avec 2176 : la première promotion appliquée bloque la seconde ;
- formule à 0 € (1541) avec chacun des prix fixes (Q6) ;
- promotion 92 et promotion expirée : aucun effet.

## 11. Liste de préparation avant de coder

| | Élément | État |
|---|---|---|
| ✔ | Règle cible et décisions D1 à D17 | Figées (D13 à confirmer) |
| ✔ | Export de production A à E | Reçu et analysé (§ 10) |
| ✔ | Base de tests actuelle verte (`orders`, `kiosk`, `scannorder`, `discounts`, `menu/importer`) | Vérifiée le 2026-10-17 |
| ☐ | Export complet des promotions 9 et 10 (section G) | **À fournir** |
| ☐ | Prix réellement facturés sur les formules (section F) | **À fournir** |
| ✔ | Q6 : formule à 0 € et prix fixe | Tranchée (D18) ; reste à mesurer l'impact avec la requête F et à prévenir les 2 commerçants |
| ☐ | D13 : confirmation explicite | À confirmer |
| ☐ | Relecture cas par cas des tests existants qui figent l'ancien comportement (`discounts_pricing_test.go`, `discount_options_test.go`, listes borne et Scan&Order) | À faire en phase 0 |
| ☐ | Vérification des remises de commande sur Stripe Checkout et terminal (§ 12) | Second temps, test de vérification à écrire |
| ✔ | Listes de promotions de la borne et du Scan&Order | Vérifiées le 2026-10-18 : elles ne font que badger les produits ciblés (`promoProducts`, kiosk/service.go), sans règle de quantité ni de minimum ; le nouveau calcul ne les contredit pas |
| ✔ | Recalcul côté applications (D16) | Confirmé dans le code : la caisse Flutter n'utilise que `discountedPrice` renvoyé par `/orders/pricing` (estimation locale du total avant réponse, sans règle de promotion). Le serveur enregistre tel quel les prix et le total envoyés par la caisse |
| ✔ | Migration de base de données | Aucune : D4 exclut une colonne « mode de calcul », le correctif est uniquement dans le code |
| ☐ | **Commande ouverte modifiée (ajout d'articles) sur la caisse** | À vérifier en phase 0 : si le prix est recalculé sur tout le panier, choisir « l'article le moins cher » peut déplacer la remise d'une ligne déjà enregistrée ou payée vers une nouvelle. Lire comment la caisse appelle `/orders/pricing` pour une commande existante et écrire le test |
| ☐ | Uber Eats, Deliveroo | Non concernés d'après le mapper (ils portent leurs propres promotions, `SubTotalPromoApplied`) : à ne pas modifier, à confirmer en relisant leur appel |
| ☐ | Moment du déploiement | Hors des créneaux de la promotion 3 (lundi au samedi 10:45–14:00, la plus utilisée) et des autres créneaux actifs ; ré-export de la production juste avant, pour voir les promotions créées ou activées entre-temps |
| ☐ | Message aux commerçants | Trois commerçants seulement : formule à 0 € (promotions 3 et 5 à 7), changement de l'article remisé (toutes), date incohérente de la promotion 92 |

La phase 0 (tests de caractérisation, aucun changement du moteur) peut démarrer dès maintenant : Q6 n'est plus bloquante. Les exports F et G sont nécessaires pour compléter les cas de la promotion 10 et mesurer l'impact sur les formules, pas pour écrire les tests des autres promotions.

## 12. Second temps : remises au niveau de la commande et paiement (D17)

Exemple visé : 5 € offerts sur la 10ᵉ commande. Vérifications faites dans le code (2026-10-17), **à confirmer par des tests** avant toute construction :

- **Les promotions produit passent bien dans le paiement.** Stripe Checkout fabrique ses lignes à partir de `DiscountedPrice` + suppléments ([stripe/checkout.go](../internal/infrastructure/stripe/checkout.go)) ; le terminal de paiement de la borne relit `orders.TTC` (kiosk/service.go, `CreateTerminalPaymentIntent`). Aucun risque pour le chantier en cours.
- **Une remise de commande n'est pas une remise d'article.** Les récompenses `fixed_discount` et `percent_discount` sont calculées par `applyRewards` comme un **paiement `REWARD`** ajouté à `Order.Payments`, sans toucher aux prix des lignes ni au TTC. Or Scan&Order ([scannorder/service.go:1252](../internal/modules/scannorder/service.go)) et la borne ([kiosk/service.go:1950](../internal/modules/kiosk/service.go)) **vident la liste des paiements après le calcul de prix**, et les lignes de Stripe Checkout ne contiennent que les articles. Par lecture du code, une telle remise **n'est pas déduite du montant encaissé** sur ces canaux, alors que `UsedRewards` semble consommer la récompense (`consumeUsedRewards`, order_life_cycle). À reproduire par un test avant d'agir.
- **Piste de conception** (à décider à ce moment-là) : répartir la remise de commande au prorata sur les lignes (comme les caisses du commerce, ce qui garde cohérents TVA, total, lignes Stripe et montant du terminal) plutôt que l'ajouter comme un paiement. Cela touche aussi la chaîne fiscale et les exports comptables ([TVA_FIGEE_LIGNES_COMMANDE.md](TVA_FIGEE_LIGNES_COMMANDE.md)) : chantier à part entière, avec les remises `ORDER_TOTAL` (§ 7).

## 13. Journal

- **2026-10-16** — Cadrage : relecture du moteur et du modèle (`discounts`, `discounts_products`, `discounts_schedules`, `discounts_products_options`, `discount_redemptions`), questions à Ilies, décisions D1 à D11, requête d'export prod validée sur staging, ce document. Constats P5 à P8. Aucun code applicatif modifié.
- **2026-10-17** — Export de production reçu (8 promotions, 3 commerçants, 2 utilisées) et analysé (§ 10). Réponses d'Ilies à Q1 à Q5 (D12 à D16), remarque sur les remises de commande (D17, § 12, circuit de paiement lu dans le code). Nouveau point bloquant Q6 (formules à 0 € ciblées par un prix fixe). Requête d'export complétée (sections F et G, validées sur staging), base de tests verte. Aucun code applicatif modifié.
- **2026-10-18** — Q6 tranchée : une unité que la promotion n'abaisse pas compte comme achetée mais n'est jamais remisée (D12 révisé, D6 précisé, spécification § 3 réécrite, nouveaux cas de contrôle), pas d'exception pour les formules à 0 € (D18), retrait par l'import des produits non abaissés à revoir (§ 6). D17 confirmé (remises de commande plus tard). Aucun code applicatif modifié.
