# Livrable 5 — Backlog des opportunités

**Révisé le 2026-10-16** avec les données de prod et les réponses aux cinq
questions ([L9](L9-resultats-baseline-prod.md)) : objectif **CA** avec le moins
de friction possible, environ 280 sessions upsell par semaine sur SNO et borne,
aucun coût connu pour les produits suggérés, exclusion d'un produit demandée
par les restaurateurs.

## 1. Méthode de priorisation

**Indice = Gain × Confiance ÷ Effort**, le risque servant de veto ou de
condition (test obligatoire).

- **Gain** (1-5) : effet attendu sur le CA incrémental ou sur la friction,
  directement ou en rendant mesurable une décision qui les conditionne
  (« indirect »). Repère à volume actuel ([L9 §6](L9-resultats-baseline-prod.md#6-ce-que-cela-change)) :
  +1 point d'ajout ≈ +270 € TTC par an ; +0,1 point d'abandon ≈ −230 € par an.
- **Confiance** : 1,0 (constat vérifié, mécanisme certain), 0,7 (mécanisme
  clair, ampleur inconnue), 0,4 (hypothèse à valider).
- **Effort** (1-5) : 1 ≈ moins de 2 jours, 2 ≈ 2 à 5 jours, 3 ≈ 1 à 2
  semaines, 4 ≈ 2 à 4 semaines, 5 ≈ plus d'un mois (un développeur, tests
  compris).
- **Priorité** : P1 si indice ≥ 1,0 ou habilitant d'un P1 ; P2 entre 0,5 et
  1,0 ; P3 en dessous ou hors de portée au volume actuel.
- **Faisabilité** : *E* = existant ; *N* = migration ou nouvelles données ;
  *R* = développement client lourd.

Les migrations sont à appliquer par Ilies.

## 2. Tableau

| ID | Cat. | Problème | Solution | Gain | Effort | Risque | Dépendances | Conf. | Faisab. | Indice | Priorité |
|---|---|---|---|---|---|---|---|---|---|---|---|
| U-01 | A | Indicateurs faux : acceptation vide, acceptation sur commande non payée, appels comptés comme expositions (C-02, C-03, C-22) | Acceptée = articles non vides **et** commande clôturée non annulée ; proposée = liste renvoyée non vide ; POS efface l'id si plus de ligne upsell | 3 (indirect) | 1 | F | B8 | 1,0 | E | 3,0 | **P1** |
| U-10 | B | Pas de diversité (C-11) | Au plus 2 articles par catégorie, catégories du panier en dernier ; en test | 3 | 1 | F | U-03 pour mesurer | 0,7 | E | 2,1 | **P1** |
| U-04 | A | Contexte d'exposition perdu (C-04) | Colonnes panier, type de commande, politique, version, latence totale, nombre renvoyé | 3 (indirect) | 2 | F | migration | 1,0 | N | 1,5 | **P1** |
| U-05 | A | Places perdues par le filtre de mode après moteur (C-10) | Drapeaux et prix par mode dans les candidats | 2 | 1 | F | — | 0,7 | E | 1,4 | **P1** |
| U-09 | B | Associations toujours en tête, presque jamais prises (C-08, L9 §3) | Échelle commune (part lissée pour les petits prix), **calibrée par origine** sur les ajouts observés, classement unique ; en test (`unified_v1`) | 3 | 2 | M | U-03 | 0,7 | E | 1,05 | **P1** |
| U-26 | A | Le restaurateur ne peut pas retirer un produit des suggestions (question 5) | Drapeau produit « ne jamais proposer », réglable dans le back-office, appliqué à toutes les étapes | 2 | 2 | Nul | migration, back-office | 1,0 | N | 1,0 | **P1** |
| U-27 | A | POS : appels sans affichage, suivi perdu en mise à jour (C-05, C-23) | N'appeler que si la barre est affichée ; suivre `upsell_suggestion_id` aussi en mise à jour de commande | 2 (indirect) | 2 | F | — | 1,0 | E | 1,0 | **P1** |
| U-03 | A | Aucun témoin (C-01) | Couche de politique ; témoin 20 % puis 10 % sur SNO et borne ; exposition liée à la commande | 3 (filet) | 3 | M | U-01, U-04 | 0,7 | N | 0,7 | **P1** (habilitant des tests) |
| U-13 | B | Popup à 99,8 % des paiements (C-12) | Seuil de CA espéré en dessous duquel l'étape est sautée ; en test | 4 (friction) | 2 | M | U-03, U-09 | 0,4 | E | 0,8 | P2 |
| U-22a | B | Variante la moins chère choisie, contre l'objectif CA (C-09) | Sonde : variante **la plus vendue** d'un groupe ; en test | 2 | 1 | F | U-03 | 0,4 | E | 0,8 | P2 |
| U-12 | B | Classement aveugle au prix (C-09) | Valeur = probabilité calibrée × prix HT (CA espéré), variante `unified_v2`, seulement quand la calibration distingue la tranche de prix | 3 | 2 | M | U-09 et assez d'expositions | 0,4 | E | 0,6 | P2 |
| U-28 | B | Étape bloquante avant paiement, contraire à l'objectif de friction | Suggestions intégrées au panier (tiroir SNO, écran panier borne), sans étape ; en test contre le popup | 4 (friction) | 3 | M (moins d'ajouts) | U-03, U-04 | 0,4 | R | 0,53 | P2 |
| U-06 | A | Articles 7 à 10 enregistrés sans être vus (C-21) | Plafond d'affichage par canal côté serveur | 1 | 1 | Nul | — | 1,0 | E | 1,0 | P2 |
| U-08 | A | Attribution par intersection, en TTC (C-05) | Attribution par ligne `is_upsell` de commandes clôturées, en HT | 2 (indirect) | 2 | F | U-27 | 0,7 | E | 0,7 | P2 |
| U-07 | A | Cron non supervisé (C-16) | Alerte sur l'âge de `_meta` et la part de `none` | 1 | 1 | Nul | — | 0,7 | E | 0,7 | P2 |
| U-16 | B | Boucle auto-renforçante, marketplaces et annulations dans les statistiques (C-07, C-17) | Exclusion des statistiques de propension, avec mesure de couverture | 2 | 1 | M | U-03, décision « Reporté » | 0,7 | E | 1,4 → P2 | P2 |
| U-23 | D | Responsabilités dispersées (C-18) | Découpage ciblé au fil de U-03 et U-09 | 2 (habilitant) | 3 | M | — | 0,7 | E | 0,47 | P2 (habilitant) |
| U-11 | C | Coûts non saisis pour les produits suggérés (B4 : 0 %) | Saisie des recettes et coûts de composants pour les 10 à 41 produits suggérés par établissement ; liste « produits suggérés sans coût » dans le back-office | 1 (objectif CA) | 2 | Nul | restaurateurs | 0,7 | E | 0,35 | P3 (à réactiver si l'objectif devient la marge) |
| U-15 | B | Nombre d'articles | Test 3 contre 6 | 1 | 1 | M (positions 4-6 = 27 % des ajouts) | U-03 | 0,4 | E | 0,4 | P3 |
| U-14 | B | Accroche jamais affichée (C-13) | Raison « souvent pris avec … » ; en test | 2 | 3 | F | 3 clients | 0,4 | E | 0,27 | P3 |
| U-22b | D | Aucune montée en gamme | Type de suggestion « remplacer par » (taille, menu, option) | 4 | 5 | M (UX) | U-22a positif, 3 clients | 0,4 | R | 0,32 | P3 |
| U-24 | D | Pas d'apprentissage commun | Rôle produit réglé par le marchand | 2 | 3 | F | décision produit | 0,4 | N | 0,27 | P3 |
| U-21 | C | Pas d'a priori catégorie → catégorie | A priori lissé pour les produits sans association | 2 | 3 | F | volume | 0,4 | E | 0,27 | P3 |
| U-25 | C | LLM (C-14) | Laisser coupé ; accroche seulement s'il revient | 1 | 1 | F | — | 0,7 | E | 0,7 | P3 (faible valeur) |
| U-17 | C | Pas de taux d'acceptation observés | Taux lissés bêta-binomial | 3 | 3 | M | U-04 | 0,4 | N | 0,4 | P3 (hors de portée au volume actuel) |
| U-18 | C | Incrémentalité par segment | Écart exposé/témoin par segment | 4 | 4 | M | U-03 | 0,4 | N | 0,4 | P3 (hors de portée) |
| U-19 | C | Pas d'exploration (C-20) | Bandit entre 2 à 4 politiques | 3 | 4 | M | U-03, U-17 | 0,4 | N | 0,3 | P3 (hors de portée) |
| ~~U-20~~ | — | Charge cuisine | — | — | — | — | — | — | — | — | **abandonné** (question 5) |

**Faits** : U-02 (requêtes de référence) a été exécuté le 2026-10-16 ; reste
B8 à lancer.

**Ce qui a changé depuis la version initiale** : l'objectif CA remplace la
marge (U-12 révisé, U-11 en P3) ; la latence n'est plus un sujet (U-07 réduit
à la supervision du cron) ; U-03 n'est plus « bloquant » mais habilitant,
puisqu'il ne peut détecter que de fortes dégradations (L9 §6) ; trois nouveaux
éléments viennent des données et des réponses (U-26, U-27, U-28) ; U-22 est
scindé entre une sonde gratuite et la vraie montée en gamme.

## 3. Fiches détaillées (P1 et P2)

Format : problème · modification · mécanisme · données · code · coût ·
risques · indicateur · vérification.

### U-01 — Indicateurs fiables

- **Problème** : C-02, C-03, C-22.
- **Modification** : dans l'Analytics, acceptée = `jsonb_array_length(accepted_items) > 0`
  **et** commande liée clôturée, non annulée ; « proposées » = listes non vides
  renvoyées (après U-05, U-06 et U-27) ; le POS efface l'identifiant accepté
  quand plus aucune ligne `isUpsell` ne reste ; libellé « Suggestion » au lieu
  de « Proposé par IA ».
- **Mécanisme** : évite de juger l'upsell sur un taux faux ; aujourd'hui, 8
  acceptations suivies sur 45 ne correspondent à aucune vente (B8).
- **Code** : `analytics/upsell.go`, `upsell_controller.dart` (POS). **Coût** :
  < 2 j. **Risque** : rupture de série dans l'onglet, à signaler.
- **Vérification** : test d'intégration Analytics (acceptation vide et
  commande annulée exclues) ; B8 avant/après.

### U-10 — Diversité

- **Modification** : re-classement glouton : au plus 2 articles par catégorie
  au premier passage, catégories déjà au panier en dernier, complétion ensuite.
- **Mécanisme** : plus de catégories distinctes pour le même nombre de places.
- **Risque** : écarter une deuxième boisson probable. En test, dans le même
  bras que U-09 ([L8 R3](L8-recommandations-techniques.md#r3--score-commun-ca-espéré-et-diversité)).

### U-04 — Contexte d'exposition

- **Modification** : colonnes `order_type`, `cart_product_ids`, `session_key`,
  `policy`, `experiment_key`, `engine_version`, `returned_count`,
  `latency_total_ms`, `client_tracks_exposure` ; produit source et score par
  article. Détail en [L8 R1](L8-recommandations-techniques.md#r1--journal-dexposition-et-indicateurs-fiables).
- **Coût** : 2 à 5 j, migration additive.

### U-05 — Disponibilité par mode dans le moteur

- **Modification** : `ListAvailableProductsForUpsell` lit `available_in`,
  `available_take_away`, `available_delivery` et les prix par mode ; filtrage
  selon le type de commande avant sélection.
- **Mécanisme** : places remplies au lieu d'être perdues ; seuil « petit prix »
  sur le prix réellement affiché.

### U-09 — Échelle commune

- **Problème** : C-08 ; données : associations en tête, 2 ajouts sur 266
  présentations, contre 1,32 % pour les petits prix (L9 §3).
- **Modification** : [L8 R3](L8-recommandations-techniques.md#r3--score-commun-ca-espéré-et-diversité) :
  score brut commun (P(B|A) ou P(B)), multiplié par un facteur de calibration
  par origine estimé chaque nuit sur les ajouts observés (aujourd'hui 0,75 %
  des présentations pour une association, 1,32 % pour un petit prix).
- **Indicateur** : sessions avec ajout et CA par session exposée.
- **Taille** : un gain de +50 % (5,4 % → 8,1 % de sessions avec ajout payé) se
  lit en 10 à 12 semaines à 50/50 ; un gain de +30 % en environ 25 semaines.

### U-26 — Exclusion d'un produit par le restaurateur

- **Demande** : question 5.
- **Modification** : colonne `products.upsell_excluded boolean NOT NULL DEFAULT FALSE` ;
  case « Ne jamais proposer en suggestion » sur la fiche produit du
  back-office ; filtre dans `ListAvailableProductsForUpsell`,
  `computeUpsellLowPriceList`, `ListFeaturedProducts` ; une association vers
  un produit exclu est ignorée. Un groupe exclu exclut ses variantes.
- **Mécanisme** : retire les suggestions incongrues (frais de livraison à 0 €
  au POS, sauces offertes, produits que le restaurateur ne veut pas pousser)
  sans règle sur les noms.
- **Coût** : 2 à 4 j (migration, API, back-office). **Risque** : nul.
- **Détail** : [L8, annexe](L8-recommandations-techniques.md#annexe--exclusion-dun-produit-par-le-restaurateur-u-26).

### U-27 — POS : appels et suivi

- **Modification** : (1) côté POS, ne déclencher `onCartChanged` que si la
  barre est affichée (mise en page tablette et `posUpsellEnabled`) ; (2) côté
  API, déclencher le suivi aussi lors d'une mise à jour de commande qui porte
  `upsell_suggestion_id` (le POS l'envoie déjà).
- **Mécanisme** : les lignes POS deviennent des expositions réelles ; les
  ajouts faits sur une commande existante sont enfin suivis.
- **Coût** : 2 à 4 j. **Vérification** : à 212, appels POS par commande et
  acceptations POS suivies avant/après.

### U-03 — Couche de politique et témoin

- **Rôle révisé** : filet contre une forte dégradation (abandon +3 points
  détectable en ~10 semaines avec 20 % de témoin) et support de tous les tests
  (U-09, U-10, U-12, U-13, U-22a, U-28). Il ne peut pas prouver que le popup
  est rentable au volume actuel (seuil d'environ 0,6 point d'abandon).
- **Modification** : [L8 R2](L8-recommandations-techniques.md#r2--groupe-témoin-et-couche-de-politique).
- **Coût** : 1 à 2 semaines, en grande partie partagé avec U-04.

### U-13 — Seuil d'affichage

- **Modification** : si le meilleur CA espéré (probabilité × prix HT) est sous
  un seuil, liste vide (`source = below_threshold`), donc étape sautée sur SNO
  et borne.
- **Mécanisme** : moins de friction là où l'upsell rapporte peu. Repère
  chiffré : « afficher seulement s'il existe une association » supprimerait
  l'étape pour 61 % des sessions chez 212 et 226 en perdant 37 % des ajouts,
  mais 90 % chez 231 ([L9 §6](L9-resultats-baseline-prod.md#6-ce-que-cela-change)) :
  le seuil doit être réglé par établissement, et testé.

### U-22a — Sonde « variante la plus vendue »

- Une association vers un groupe propose la variante la plus vendue au lieu de
  la moins chère (`cheapestVariants`). Aucun changement client ; premier signal
  sur la montée en gamme, aligné sur l'objectif CA.

### U-12 — CA espéré

- Valeur = probabilité calibrée × prix HT (règle *(c)* conservée) ; les
  produits à 0 € tombent naturellement en dernier. Variante `unified_v2`,
  testée après `unified_v1`, et seulement quand la calibration distingue la
  tranche de prix : les scores bruts surestiment l'ajout d'un ordre de
  grandeur, davantage pour les associations ; sans cette précaution, un
  article cher à score gonflé passerait en tête.

### U-28 — Suggestions sans étape

- **Modification** : afficher les suggestions dans le tiroir du panier SNO et
  l'écran panier de la borne, sans popup bloquant ; « Passer la commande » va
  directement au paiement.
- **Mécanisme** : supprime par construction le risque d'abandon lié à l'étape
  (le seul que l'on ne peut pas mesurer au volume actuel), au prix probable de
  moins d'ajouts.
- **Prérequis** : U-04 (la ligne ne doit compter que si la liste a été vue,
  et un appel par changement de panier ne doit plus créer une ligne à chaque
  fois). **Test** : popup contre intégré, indicateurs : sessions avec ajout,
  CA par session, appels par commande.

### U-06, U-08, U-07, U-16, U-23

- **U-06** : `GenerateUpsell` reçoit le plafond d'affichage du canal (6 pour
  SNO et borne).
- **U-08** : acceptation lue sur les lignes `is_upsell` des commandes
  clôturées, en HT ; l'intersection par `product_id` reste en secours.
- **U-07** : alerte si `upsell:patterns:<merchant>:_meta.computed_at` a plus de
  30 h, ou si la part de `none` / `featured_fallback` dépasse un seuil.
- **U-16** : exclure des statistiques de propension les lignes `is_upsell`,
  les commandes Uber Eats et Deliveroo, les annulations ; mesurer la perte de
  couverture avec la requête de comparaison existante avant de décider.
- **U-23** : voir C-18, à mener au fil de U-03 et U-09.

## 4. Fiches courtes (P3)

- **U-11 — Coûts** : le mécanisme existe (coût d'achat des composants) ; il
  manque la saisie. Chez 212, 1 309 lignes `INCOMPLETE_RECIPE` montrent des
  recettes commencées mais incomplètes. Une liste « produits suggérés sans
  coût » dans le back-office ciblerait les 10 à 41 produits concernés par
  établissement.
- **U-15 — Nombre d'articles** : les positions 4 à 6 font 27 % des ajouts ;
  réduire coûterait du CA pour un gain de friction faible (l'écran reste le
  même).
- **U-14 — Raison affichée** : « Souvent pris avec <produit> » ; demande le
  produit source par article.
- **U-22b — Montée en gamme** : seulement si la sonde U-22a est positive.
- **U-24, U-21, U-25** : inchangés.
- **U-17, U-18, U-19** : demandent des centaines d'expositions par produit ou
  par politique et par semaine ; à environ 280 sessions par semaine en tout,
  hors de portée tant que l'adoption n'a pas fortement augmenté.
