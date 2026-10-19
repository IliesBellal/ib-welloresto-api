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
| D13 | **Une unité déjà remisée par une promotion précédente ne compte ni pour le minimum, ni pour les groupes de la promotion suivante** (Q2, pratique usuelle des moteurs de promotions : une unité « consommée » par une offre ne sert pas de seuil à une autre). **Confirmée par Ilies le 2026-10-18.** Aucun effet sur la production actuelle (§ 10) : les seules promotions qui se chevauchent sont toutes non cumulables, donc la première appliquée bloque les suivantes. | proposée 2026-10-17, confirmée par Ilies | 2026-10-18 |
| D19 | **Commande ouverte modifiée (ajout d'articles) : recalcul sur toute la commande, sans geler les lignes déjà enregistrées ou payées** (recommandation, **à confirmer par Ilies**). Le total est celui que donnerait la même commande passée en une fois ; la ligne qui porte la remise peut changer d'une saisie à l'autre. Garantie à vérifier par test en phase 1 : un ajout en deux temps donne le même total qu'en un seul (indépendance à l'ordre d'ajout). Limite connue et inchangée : atteindre un seuil (minimum, groupe complet) peut faire baisser le prix de lignes déjà enregistrées, y compris le total (prix fixe 5 €, minimum 2 : 10 € devient 7 € avec un article à 2 €), comme aujourd'hui. Justification au § 13.3. | proposée, **confirmée par Ilies** | 2026-10-18 |
| D20 | **Principe : le pricing et la prise de commande ne sont jamais empêchés par une promotion** ; mieux vaut rater une promotion que bloquer une commande. Garde-fous G1 (chargement dégradé), G2 (isolation des paniques), G3 (contrôle de cohérence) et G5 (surface minimale) du § 14. | Ilies (principe) ; garde-fous proposés | 2026-10-18 |
| D21 | **Pas d'interrupteur d'ancien moteur** (G4 écarté) : le nouveau calcul remplace directement l'ancien, sans mode `shadow`. Retour arrière = redéploiement du code. L'ancien calcul est conservé **uniquement dans un fichier de test**, comme oracle du test différentiel (§ 14.3). | Ilies | 2026-10-18 |
| D22 | **Deux livraisons** : promotions produit d'abord, récompenses de fidélité (D14) ensuite (G6). | Ilies | 2026-10-18 |
| D23 | **Chargement dégradé confirmé (G1)** : si les promotions ou les récompenses ne se chargent pas, le pricing continue sans promotion, au prix du catalogue, avec une erreur dans les journaux. | Ilies | 2026-10-18 |
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

Résultats des requêtes A à E reçus le 2026-10-17. B et C étaient tronquées pour les promotions 9 et 10 ; la section G (reçue le 2026-10-18) les complète. **La section F, qui répond à Q6, ne renvoie aucune ligne** : en 180 jours, aucune ligne de commande n'a été facturée avec une promotion sur une formule ou sur un prix fixe supérieur ou égal au prix du catalogue. Le comportement « la formule à 0 € passe à 6 € » **n'a donc jamais joué en production sur cette période**, et D18 ne coûte rien à ce jour aux commerçants concernés (il reste à prévenir par prudence).

Apport de G : les promotions 9 et 10 ciblent bien les 34 produits (31 produits à 8,00 €, un à 10,00 €, un à 11,00 €, la formule 1541 à 0 €). La promotion 9 a un prix d'option promotionnel de 2,00 € sur l'option 2176 (obligatoire) ; la promotion 10 est un prix fixe de 3,00 € avec l'option 2176 (obligatoire) à 0 €. La formule 1541 est donc aussi ciblée par un prix fixe de 3,00 € (hausse de 0 à 3 €, comme pour les promotions 5 à 7).

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
| ✔ | Règle cible et décisions D1 à D18 | Figées (D13 confirmée le 2026-10-18) ; D19 (commande ouverte) proposée |
| ✔ | Export de production A à E | Reçu et analysé (§ 10) |
| ✔ | Base de tests actuelle verte (`orders`, `kiosk`, `scannorder`, `discounts`, `menu/importer`) | Vérifiée le 2026-10-17 |
| ✔ | Export complet des promotions 9 et 10 (section G) | Reçu le 2026-10-18 (§ 10) |
| ✔ | Prix réellement facturés sur les formules (section F) | Reçue le 2026-10-18 : **aucune ligne** en 180 jours, le cas n'a jamais joué en production |
| ✔ | Q6 : formule à 0 € et prix fixe | Tranchée (D18) ; impact mesuré nul ; prévenir quand même les commerçants |
| ✔ | D13 | Confirmée par Ilies le 2026-10-18 |
| ✔ | Phase 0 : tests de caractérisation | **Écrits et verts le 2026-10-18** (§ 13) : 27 cas du moteur, 15 cas de créneaux, 5 cas de bout en bout sur Postgres |
| ☐ | Relecture cas par cas des tests existants qui figent l'ancien comportement (`discounts_pricing_test.go`, `discount_options_test.go`) | À faire au moment de la correction du moteur (phase 1) |
| ☐ | Vérification des remises de commande sur Stripe Checkout et terminal (§ 12) | Second temps, test de vérification à écrire |
| ✔ | Listes de promotions de la borne et du Scan&Order | Vérifiées le 2026-10-18 : elles ne font que badger les produits ciblés (`promoProducts`, kiosk/service.go), sans règle de quantité ni de minimum ; le nouveau calcul ne les contredit pas |
| ✔ | Recalcul côté applications (D16) | Confirmé dans le code : la caisse Flutter n'utilise que `discountedPrice` renvoyé par `/orders/pricing` (estimation locale du total avant réponse, sans règle de promotion). Le serveur enregistre tel quel les prix et le total envoyés par la caisse |
| ✔ | Migration de base de données | Aucune : D4 exclut une colonne « mode de calcul », le correctif est uniquement dans le code |
| ✔ | **Commande ouverte modifiée (ajout d'articles) sur la caisse** | D19 confirmée par Ilies ; la garantie « total indépendant de l'ordre » est couverte par `TestEngine_Properties_OrderIndependence` et `TestEngine_DisjointPromotions_TotalIndependentOfCartOrder` (§ 15.3) |
| ✔ | Phase 1 : moteur corrigé, garde-fous G1 à G3, tests différentiel et propriétés | Faite le 2026-10-18 (§ 15), non commitée |
| ✔ | Uber Eats, Deliveroo | Validé par Ilies le 2026-10-18 : non concernés, à ne pas modifier |
| ✔ | Moment du déploiement | Validé par Ilies le 2026-10-18 : hors des créneaux de la promotion 3 (lundi au samedi 10:45–14:00, la plus utilisée) et des autres créneaux actifs ; ré-export de la production juste avant |
| ✔ | Message aux commerçants | Validé par Ilies le 2026-10-18 : trois commerçants ; l'article remisé change (toutes), formule à 0 € (promotions 3 et 5 à 7, sans effet observé), date incohérente de la promotion 92 |
| ✔ | Après le moteur : retrait de la note « N-ième offert », revue du retrait des produits non abaissés par l'import, récompenses de fidélité (D14) | Validé par Ilies le 2026-10-18 |

Les phases 0 (§ 13) et 1 (§ 15) sont faites ; D19 confirmée. Restent la phase 3 (ré-export de la production, avertissement des commerçants, déploiement hors créneaux, § 15.4) puis la seconde livraison (récompenses, D22).

## 12. Second temps : remises au niveau de la commande et paiement (D17)

Exemple visé : 5 € offerts sur la 10ᵉ commande. Vérifications faites dans le code (2026-10-17), **à confirmer par des tests** avant toute construction :

- **Les promotions produit passent bien dans le paiement.** Stripe Checkout fabrique ses lignes à partir de `DiscountedPrice` + suppléments ([stripe/checkout.go](../internal/infrastructure/stripe/checkout.go)) ; le terminal de paiement de la borne relit `orders.TTC` (kiosk/service.go, `CreateTerminalPaymentIntent`). Aucun risque pour le chantier en cours.
- **Une remise de commande n'est pas une remise d'article.** Les récompenses `fixed_discount` et `percent_discount` sont calculées par `applyRewards` comme un **paiement `REWARD`** ajouté à `Order.Payments`, sans toucher aux prix des lignes ni au TTC. Or Scan&Order ([scannorder/service.go:1252](../internal/modules/scannorder/service.go)) et la borne ([kiosk/service.go:1950](../internal/modules/kiosk/service.go)) **vident la liste des paiements après le calcul de prix**, et les lignes de Stripe Checkout ne contiennent que les articles. Par lecture du code, une telle remise **n'est pas déduite du montant encaissé** sur ces canaux, alors que `UsedRewards` semble consommer la récompense (`consumeUsedRewards`, order_life_cycle). À reproduire par un test avant d'agir.
- **Piste de conception** (à décider à ce moment-là) : répartir la remise de commande au prorata sur les lignes (comme les caisses du commerce, ce qui garde cohérents TVA, total, lignes Stripe et montant du terminal) plutôt que l'ajouter comme un paiement. Cela touche aussi la chaîne fiscale et les exports comptables ([TVA_FIGEE_LIGNES_COMMANDE.md](TVA_FIGEE_LIGNES_COMMANDE.md)) : chantier à part entière, avec les remises `ORDER_TOTAL` (§ 7).

## 13. Phase 0 : tests de caractérisation (2026-10-18)

### 13.1 Ce qui a été écrit

Aucun code applicatif n'est modifié. Trois fichiers de tests, plus la correction d'un test existant.

| Fichier | Contenu | Exécution |
|---|---|---|
| [orders/discounts_prod_golden_test.go](../internal/modules/orders/discounts_prod_golden_test.go) | **27 cas** sur les promotions réelles (3, 4 et l'échelle du lundi 8, 10, 9 ainsi que 5, 6, 7), avec les prix du catalogue de l'export, les options obligatoires et les prix d'option promotionnels. Chaque cas fige le résultat du moteur **actuel** ; **8 cas** portent en plus le résultat attendu après correction (`target`), calculé à la main d'après la règle du § 3. | `go test ./internal/modules/orders/`, sans base |
| [orders/discounts_prod_postgres_integration_test.go](../internal/modules/orders/discounts_prod_postgres_integration_test.go) | **15 cas de créneaux** aux bornes réelles (début inclus, fin exclue, minuit, jours, promotion 92 aux dates inversées jamais active, promotion sans créneau toujours active) et **5 cas de calcul complet** de la promotion 3 par `ComputePricing` sur un vrai Postgres. | `POSTGRES_URL=… go test -tags postgres_integration -run ProdPromotions ./internal/modules/orders/` |
| [orders/discounts_postgres_integration_test.go](../internal/modules/orders/discounts_postgres_integration_test.go) | **Correction d'un test existant** : `TestDiscountTimeRestriction_Postgres` fixait la date « 2026-09-21 » alors que ses promotions valent à partir d'hier ; dès la fin septembre aucune n'était plus active et le test échouait sur staging. Il utilise maintenant la date du jour. | idem |

Résultat : tout est vert sur staging (le moteur actuel), ainsi que les packages `orders`, `kiosk`, `scannorder`, `discounts` et `menu/importer`.

### 13.2 Comment s'en servir le jour de la correction

La constante `goldenTargetEngine` (discounts_prod_golden_test.go) vaut `false`. Quand le moteur est corrigé, on la passe à `true` :
- les 19 cas sans `target` doivent rester **identiques** ;
- les 8 cas avec `target` doivent donner exactement la valeur cible ;
- toute autre différence est un bug.

Un second test (`TestProdPromotionsGolden_TargetDefinedForEveryChange`) garantit qu'un cas ne change jamais sans que sa valeur cible et ses remises appliquées soient renseignées. Le test de bout en bout de la promotion 3 suit la même constante. Les valeurs cibles n'ont **pas** été exécutées : elles sont calculées à la main et seront vérifiées par le moteur corrigé.

Les 8 cas qui changent : promotion 3 avec les prix du plus cher au moins cher, promotion 3 avec une formule à 0 €, promotion 4 avec trois articles, lundi avec deux articles de prix différents, lundi avec un article à 11 € en premier, lundi avec la formule en premier, mercredi avec deux articles de prix différents, mercredi avec la formule.

### 13.3 Commande ouverte modifiée : recommandation (D19)

Constats dans le code (2026-10-18) :
- `buildSelectedProducts` **recompose la commande de zéro** à chaque appel : `DiscountID` et `DiscountedPrice` des lignes reçues sont ignorés, l'identifiant de ligne n'est pas recopié. La caisse envoie sa commande entière à `/orders/pricing`, et le serveur enregistre telles quelles les lignes et le total qu'elle renvoie.
- Avec le moteur actuel, ajouter un article **à la fin** du panier ne déplace jamais une remise (« les premiers du panier »). Avec la règle cible, ajouter un article moins cher peut la déplacer vers lui. C'est le seul changement de comportement dû à la correction.

Recommandation : **ne pas geler** les lignes déjà enregistrées ou payées.
1. Geler obligerait la caisse à renvoyer les remises déjà accordées et le serveur à les respecter ; cela rendrait le total dépendant de l'ordre dans lequel le client commande, ce que D1 vise justement à supprimer.
2. Ce qui compte pour le client est le **total** de la commande, pas la ligne qui porte la remise. Avec la règle cible, le total d'un ajout en deux temps est le même qu'en un seul ; avec le moteur actuel il ne l'est pas.
3. Le cas où atteindre un seuil fait baisser le prix de lignes déjà enregistrées existe déjà aujourd'hui (4ᵉ article qui rend le 1ᵉʳ gratuit) et n'est pas aggravé.

À écrire en phase 1 : test « ajout en deux temps = même total qu'en un seul » sur chaque forme de promotion.

**Observation de même nature, antérieure au chantier (P9)** : puisque le calcul repart de zéro, modifier une commande ouverte **après la fin d'un créneau** retire la promotion de toutes ses lignes (promotion 3 à 14:01, par exemple), et utilise les prix du catalogue du moment. Non traité ici ; à signaler à Ilies pour la suite.

## 14. Garde-fous de mise en production (principe : mieux vaut rater une promotion que bloquer une commande)

Principe posé par Ilies le 2026-10-18 : **le pricing et la prise de commande ne doivent jamais être empêchés par une promotion**. Une promotion ratée coûte une remise ; un pricing en erreur coûte une commande.

### 14.1 Risques constatés dans le code actuel (indépendants de la correction)

- `loadDiscountStructures` ([orders/service.go](../internal/modules/orders/service.go), appelée par `ComputePricing`) **renvoie l'erreur** si l'une des trois requêtes de promotions échoue : tout le pricing échoue, donc plus de prise de commande sur la caisse, la borne et le Scan&Order. Même chose pour `GetRewards`.
- Aucune protection contre une **panique** dans `applyDiscounts` : elle remonte jusqu'à la requête HTTP.

### 14.2 Garde-fous proposés

| # | Garde-fou | Effet |
|---|---|---|
| G1 | **Chargement dégradé** : si les promotions (ou les récompenses) ne se chargent pas, on journalise l'erreur (déjà fait par `logPricingStageError`) et le pricing continue **sans promotion**, au prix du catalogue. | Une panne de la table des promotions ne bloque plus les commandes. |
| G2 | **Isolation des paniques** : l'application des promotions est enveloppée par une récupération de panique ; en cas de panique, journal d'erreur, toutes les remises sont annulées, le prix du catalogue s'applique. | Un défaut du nouveau calcul ne peut pas faire tomber une requête. |
| G3 | **Contrôle de cohérence du résultat** : après calcul, vérifier que chaque unité a un prix remisé compris entre 0 et son prix de base (jamais une hausse), que le nombre d'unités remisées ne dépasse pas le nombre d'unités, et que les prix d'option promotionnels sont positifs ou nuls. Si un contrôle échoue : journal d'erreur, remises annulées. | Un résultat absurde n'arrive jamais jusqu'à Stripe ni au ticket. |
| ~~G4~~ | ~~Interrupteur `PROMO_ENGINE_MODE` (`legacy` / `shadow` / `new`)~~ — **écarté par Ilies le 2026-10-18 (D21)**. | Le filet repose donc sur G1 à G3, les tests différentiels et le déploiement hors créneaux. |
| G5 | **Le nouveau calcul n'ajoute aucune requête SQL ni signature publique** : il remplace uniquement l'intérieur d'`applyDiscounts` ; aucune migration. | Surface de changement minimale. |
| G6 | **Deux livraisons** : la première ne touche que les promotions produit ; les récompenses de fidélité (D14) viennent dans une seconde livraison séparée. | Un seul comportement change à la fois. |

L'ancien calcul n'est pas conservé dans le code de production. Il vit dans un fichier de test (`discounts_legacy_oracle_test.go`) qui sert d'oracle au test différentiel.

### 14.3 Tests supplémentaires prévus en phase 1

- **Test différentiel** : sur plusieurs milliers de paniers et de promotions tirés au hasard (graine fixe), pour toutes les formes (pourcentage, montant, prix fixe, cumulable ou non, quantité remisée, minimum en quantité ou en euros, options), **quand le choix de l'unité n'a pas d'importance** (toutes les unités éligibles ont le même prix de base), le nouveau calcul doit donner **exactement** le résultat de l'ancien. Toute différence est une régression.
- **Tests de propriétés** sur les mêmes tirages : aucune panique ; jamais de hausse de prix ; jamais de prix négatif ; nombre d'unités remisées ≤ `n` ; résultat identique quel que soit l'ordre du panier ; un second passage donne le même résultat.
- **Tests des garde-fous** : panique simulée dans le calcul (G2), résultat incohérent injecté (G3), échec simulé du chargement des promotions et des récompenses (G1) → le pricing renvoie le prix du catalogue sans erreur.
- **Cas golden** (§ 13) avec `goldenTargetEngine = true`.

### 14.4 Déploiement

1. Tests verts (golden avec `goldenTargetEngine = true`, différentiel, propriétés, garde-fous), suite complète et tests Postgres sur staging.
2. Ré-export de la production, rejeu des cas golden sur les données fraîches, liste « ancien résultat / nouveau résultat » par commerçant.
3. Avertissement des commerçants concernés.
4. Déploiement **hors des créneaux** de la promotion 3 (lundi au samedi 10:45–14:00) et des autres créneaux actifs. Retour arrière = redéploiement de la version précédente (pas d'interrupteur, D21) ; les garde-fous G1 à G3 limitent l'impact d'un défaut à une promotion non appliquée, jamais à une commande bloquée.
5. Surveillance des journaux « promotions annulées » (G2/G3) et « pricing dependency failed » (G1) après le déploiement.

## 15. Phase 1 : moteur corrigé (2026-10-18)

### 15.1 Ce qui a changé dans le code

| Fichier | Changement |
|---|---|
| [orders/discounts_engine.go](../internal/modules/orders/discounts_engine.go) (nouveau) | `applyDiscounts` réécrit d'après le § 3, même signature ; `discountedUnitPrice` (calcul d'une unité, plafond, plancher à 0) ; `applyDiscountsSafe` (G2 : récupération de panique, G3 : contrôle de cohérence, annulation de toutes les remises et journal « promotions annulées, prix du catalogue appliqué ») ; `checkDiscountResult`. |
| [orders/service.go](../internal/modules/orders/service.go) | L'ancien `applyDiscounts` est retiré. `ComputePricing` appelle `applyDiscountsSafe` et **continue sans promotion** si les promotions ou les récompenses ne se chargent pas (G1, D23). Aucune requête SQL ni migration. |
| [orders/discounts_legacy_oracle_test.go](../internal/modules/orders/discounts_legacy_oracle_test.go) (nouveau, test) | Copie verbatim de l'ancien calcul, oracle du test différentiel. Hors production. |
| [orders/discounts_engine_test.go](../internal/modules/orders/discounts_engine_test.go) (nouveau, test) | Règles une à une, différentiel, propriétés, garde-fous (§ 15.3). |
| [orders/discounts_prod_golden_test.go](../internal/modules/orders/discounts_prod_golden_test.go) | `goldenTargetEngine = true`. |

Comportements du nouveau calcul, tous décidés (D1 à D13) sauf le dernier :
- l'unité remisée est la **plus petite remise** parmi celles que la promotion abaisse (ordre du panier en égalité) ;
- une unité que la promotion n'abaisse pas **compte comme achetée** mais n'est jamais remisée ;
- cumulable : `floor(unités éligibles / max(minimum, remisée)) × remisée` unités ; non cumulable : la quantité remisée une seule fois ;
- une unité déjà remisée par une promotion précédente n'est plus disponible pour la suivante ;
- **unité de remise inconnue** (ni `PERCENTAGE`, `CURRENCY` ni `NEWPRICE`) : la promotion est ignorée. L'ancien calcul rendait alors **tous les articles gratuits** (prix à 0) ; aucun cas en production dans l'export du 2026-10-17, mais le défaut est supprimé.

### 15.2 Vérifications faites

| Vérification | Résultat |
|---|---|
| Cas golden de production (27) avec `goldenTargetEngine = true` | Verts : les **19 cas inchangés** donnent le même résultat qu'avant, les **8 cas à valeur cible** donnent exactement la valeur calculée à la main avant le code |
| Avant d'écrire le nouveau calcul, avec l'ancienne attente | Exactement les 8 mêmes cas échouaient, aucun autre : toute la suite existante (`TestApplyDiscounts` et les autres) passait déjà avec le nouveau moteur |
| Test différentiel contre l'ancien calcul | **40 000** tirages aléatoires (graine fixe), choix de l'unité sans importance : résultat **identique** (unités, prix, suppléments d'options, remises appliquées) |
| Propriétés | 40 000 tirages avec valeurs extrêmes, négatives, unité inconnue, promotions qui se chevauchent : aucune panique, jamais de hausse ni de prix négatif ; 15 000 tirages : total indépendant de l'ordre du panier |
| Mutations | 5 défauts injectés à la main (plafond décalé, tri inversé, unités déjà remisées recomptées, taille de groupe, filtre de code promo) : chacun est détecté par au moins un test, le fichier est restauré à l'identique |
| Postgres sur staging | Verts : créneaux réels, promotion 3 de bout en bout (dont « trois articles du plus cher au moins cher » avec la valeur cible), test d'activation corrigé |
| Suite complète `go test ./internal/... ./cmd/...` | Verte, **sauf `planning/employees`, `planning/leave`, `planning/swaps`** : échecs préexistants sans lien (ce module n'importe pas `orders` et n'a aucune modification en cours) |
| Compilation | `go build ./...` et `go vet` (avec et sans balise `postgres_integration`) propres |

### 15.3 Tests ajoutés (phase 1)

Règles (`discounts_engine_test.go`) : unité la moins chère quel que soit l'ordre (toutes les permutations) ; égalité = ordre du panier ; « N-ième offert » avec 3 à 12 unités, cumulable et non ; « 2 pizzas à −50 % » ; prix fixe qui n'augmente jamais et unités qui comptent (4, 5, 6, 7 € → seul 6 € passe à 5 €) ; unité à 0 € qui compte sans être remisée ; suppléments toujours facturés sur l'unité offerte ; minimum en euros sur le panier entier ; unité déjà remisée non recomptée (D13) ; unité de remise inconnue ; indépendance à l'ordre sur les 120 permutations d'un panier avec deux promotions.
Garde-fous : panique simulée (G2), 12 cas de `checkDiscountResult` (G3), pricing complet qui réussit quand `GetDiscounts`, `GetDiscountProducts`, `GetDiscountProductOptions` ou `GetRewards` échouent (G1).

### 15.4 Reste à faire avant la mise en production

1. **Rien n'est commité** (règle : pas de commit sans demande explicite).
2. Ré-export de la production et rejeu des cas sur données fraîches ([diagnostic-promotions-prod.sql](diagnostic-promotions-prod.sql)).
3. Avertir les commerçants (§ 11), puis déployer **hors des créneaux** de la promotion 3.
4. Surveiller après déploiement : « promotions annulées, prix du catalogue appliqué » (G2/G3) et « pricing continue sans promotion » (G1) ; la moindre occurrence est un défaut à analyser.
5. Seconde livraison (D22) : récompenses de fidélité, règle « article le moins cher » (D14).
6. Import de carte photo : retirer `promotionEngineNthFreeNote`, revoir le retrait des produits non abaissés, mettre à jour [import-carte-ia-03-porte-ia.md](import-carte-ia-03-porte-ia.md) D26 et D27 (§ 6).
7. Relire cas par cas `discounts_pricing_test.go` et `discount_options_test.go` : tous passent, mais certains figent un comportement qui n'est plus la règle de référence (ex. « non cumulable sur 2 unités » ne dit rien du choix de l'unité).

## 16. Journal

- **2026-10-16** — Cadrage : relecture du moteur et du modèle (`discounts`, `discounts_products`, `discounts_schedules`, `discounts_products_options`, `discount_redemptions`), questions à Ilies, décisions D1 à D11, requête d'export prod validée sur staging, ce document. Constats P5 à P8. Aucun code applicatif modifié.
- **2026-10-17** — Export de production reçu (8 promotions, 3 commerçants, 2 utilisées) et analysé (§ 10). Réponses d'Ilies à Q1 à Q5 (D12 à D16), remarque sur les remises de commande (D17, § 12, circuit de paiement lu dans le code). Nouveau point bloquant Q6 (formules à 0 € ciblées par un prix fixe). Requête d'export complétée (sections F et G, validées sur staging), base de tests verte. Aucun code applicatif modifié.
- **2026-10-18** — Q6 tranchée : une unité que la promotion n'abaisse pas compte comme achetée mais n'est jamais remisée (D12 révisé, D6 précisé, spécification § 3 réécrite, nouveaux cas de contrôle), pas d'exception pour les formules à 0 € (D18), retrait par l'import des produits non abaissés à revoir (§ 6). D17 confirmé (remises de commande plus tard). Aucun code applicatif modifié.
- **2026-10-18 (suite)** — Export complété : F vide (le cas de la formule à 0 € n'a jamais joué en 180 jours), G reçue. D13 confirmée, points 4 à 7 de la liste validés par Ilies. **Phase 0 réalisée** (§ 13) : 27 cas golden du moteur dont 8 à valeur cible, 15 cas de créneaux et 5 cas de calcul complet sur Postgres, test existant corrigé (date figée). Recommandation D19 pour la commande ouverte, à confirmer. Aucun code du moteur modifié.
- **2026-10-18 (fin)** — D19 confirmée. Principe d'Ilies : mieux vaut rater une promotion que bloquer le pricing (D20). Constat dans le code : un échec de chargement des promotions ou des récompenses fait échouer tout le pricing, et rien n'isole une panique. Garde-fous G1 à G6, tests différentiels et plan de déploiement par paliers consignés au § 14 (D21 et D22 à confirmer). Aucun code du moteur modifié.
- **2026-10-18 (code)** — D21 (pas d'interrupteur), D22 (deux livraisons), D23 (chargement dégradé) confirmées par Ilies. **Phase 1 réalisée** : `applyDiscounts` réécrit (`discounts_engine.go`), enveloppe de sécurité `applyDiscountsSafe`, chargement dégradé dans `ComputePricing`, ancien calcul déplacé en oracle de test. Vérifié : 27 cas golden, 40 000 tirages différentiels identiques à l'ancien calcul, propriétés et indépendance à l'ordre, 5 mutations détectées, Postgres sur staging, suite complète verte hors échecs préexistants de `planning/*`. Non commité.
