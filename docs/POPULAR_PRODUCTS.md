# Produits populaires (`is_popular`) : refonte du calcul nocturne

Chantier ouvert le 2026-09-28. Ce document suit les décisions au fil de l'eau.

**Objectif (Ilies)** : désigner au mieux les produits qui ont du succès, pour
augmenter les ventes.

## 1. À quoi sert `is_popular`

- **Badge « Populaire »** sur les cartes produit de la borne
  (`wello-kiosk`, `product_card.dart`) et du Scan&Order
  (`wello-resto-scannorder`, `ProductCard.tsx`).
- **Priorité dans l'upsell côté client** : `upsell_controller.dart` (borne),
  `UpsellPopup.tsx` (SNO).
- **Dernier recours de l'upsell côté API** : `ListFeaturedProducts`, appelé
  seulement quand les associations et le LLM ne peuvent même pas être essayés
  (Redis en panne, liste des candidats en échec). Depuis D11
  (`UPSELL_COMPLETION.md`), `is_popular` n'est plus une étape de complément.
- **Route dépréciée** `GET /scannorder/{slug}/upsell` : plus aucun appel
  dans les fronts connus. Logs de prod (`api_request_logs`, relevé du
  2026-09-28) : aucun `GET` sur 60 jours, 379 `POST` (la nouvelle route)
  venant de 5 établissements depuis le 2026-08-15. Elle peut être supprimée.
  C'est une tâche à part, hors de ce chantier.
- La caisse (POS Flutter) ne l'utilise pas.

## 2. Algorithme actuel et défauts

Cron `UpdatePopularProducts`, chaque nuit à 2h
([internal/tasks/products.go](../internal/tasks/products.go)) : meilleure
vente de chaque catégorie (au moins 5 lignes de commande sur 30 jours), plus
les 10 meilleures ventes globales sans seuil.

- Le top 10 global se concentre sur la catégorie dominante : jusqu'à 90 % d'une
  catégorie marquée, alors que d'autres n'en ont aucun.
- Aucun filtre sur l'état des commandes : les commandes annulées ou refusées
  comptent.
- Les variantes ont souvent une `category` vide. Elles sont classées dans une
  fausse catégorie `''` au lieu de celle de leur groupe.
- Les produits désactivés ou retirés de la carte restent candidats et prennent
  des places.
- Il compte des lignes de commande, pas des commandes : 6 cocas pour une même
  table comptent autant que 6 clients.
- Un produit récent est comparé sur 30 jours à des produits installés.
- Tout est remis à zéro chaque nuit, sans stabilité : un produit à la limite
  du seuil gagne et perd le badge d'un jour à l'autre.
- Les égalités sont départagées au hasard (`LIMIT 1`).

## 3. Décisions

### P1 — Mesure du succès : commandes distinctes (2026-09-28)

Nombre de commandes valides contenant le produit, tous canaux confondus
(Uber Eats et Deliveroo compris). Une commande compte une fois par produit,
quelle que soit la quantité.

*Pourquoi* : la popularité, c'est le nombre de clients qui choisissent le
produit, pas le volume d'une seule table.

### P2 — Commandes valides (2026-09-28)

`state IN ('CLOSED', 'DONE')` et `upper(brand_status) NOT IN ('CANCELED',
'DENIED', 'DELETED')`. C'est le périmètre de l'analytics
(`internal/modules/analytics/scope.go`), plus `DENIED`.

*Pourquoi `DENIED` en plus* : c'est un refus à la prise de commande, le
produit n'a jamais été préparé ni vendu. Valeurs vérifiées sur le staging :
`CLOSED`, `CANCELED`, `DENIED`, `COMPLETED` pour les commandes fermées.

### P3 — Ventes upsell conservées (2026-09-28, décision d'Ilies)

Les lignes `is_upsell` comptent comme les autres. Le risque d'auto-renforcement
(un produit populaire est suggéré, donc vendu, donc reste populaire) sera
examiné lors de l'analyse upsell de fin décembre 2026. Noté dans
`UPSELL_COMPLETION.md`, section « Reporté ».

### P4 — Fenêtre de 28 jours, pondération dans le temps (2026-09-28)

- 28 jours, soit 4 semaines complètes : chaque jour de la semaine pèse autant.
- Chaque commande pèse `0.5 ^ (âge en jours / 14)` : elle perd la moitié de son
  poids en 14 jours.
- Le score est ramené en « commandes sur 28 jours au rythme actuel » : un
  produit vendu régulièrement une fois par jour vaut environ 28. Formule :
  `somme des poids × 28 × ln 2 / (14 × (1 − 0.5 ^ (d / 14)))`, où `d` est le
  nombre de jours de présence du produit.
- Produit créé il y a moins de 28 jours : `d` = ses jours de présence, au
  minimum 7 (il n'a pas besoin de 28 jours d'historique, mais un seul bon
  jour ne suffit pas).

*Pourquoi* : suivre les tendances (nouvelle carte, saison) sans rendre le
résultat instable, et laisser sa chance à un nouveau plat.

Exemple vérifié sur le staging : 15 commandes toutes âgées de 8 à 27 jours
donnent un score de 10,3. Le produit ralentit, son score baisse.

### P5 — Candidats et catégorie d'affichage (2026-09-28)

- Candidat : `enabled`, `status IN ('1', 'available')`, pas un groupe
  (`is_product_group`). Une variante n'est candidate que si son groupe est
  lui-même actif.
- `available = false` (rupture temporaire) ne retire pas le produit : les
  fronts masquent déjà le badge d'un produit indisponible.
- Catégorie : celle du produit, sinon celle de son groupe. Seules les
  catégories actives (`productcateg.enabled`) comptent. Un produit sans
  catégorie résoluble n'est affiché nulle part, il n'est pas candidat.

### P6 — Sélection par catégorie (2026-09-28)

- Le 1er est marqué si son score atteint le seuil absolu (4, voir P10).
- Les suivants sont marqués seulement s'ils atteignent au moins 50 % du score
  du 1er.
- Plafond : 25 % des produits de la catégorie (arrondi à l'inférieur), minimum
  1, maximum 3.
- Le top 10 global est supprimé.
- Égalités : départagées par `product_id`.

*Pourquoi* : le badge doit rester un signal. Une catégorie qui se vend de
façon homogène n'a qu'un populaire, son 1er. Une catégorie de 4 produits n'en
a qu'un.

### P7 — Stabilité (2026-09-28)

- Un produit déjà populaire reste candidat tant que son score atteint 75 % du
  seuil absolu, au lieu de 100 % pour y entrer.
- À score proche, le produit déjà marqué passe devant dans le classement. Le
  plafond reste strict.
- On n'écrit que les changements (produits qui gagnent ou perdent le flag).

### P8 — Colonne `popularity_score` (2026-09-28, validé)

Migration Postgres. `ListFeaturedProducts` trie par score au lieu de piocher
au hasard. Effet direct limité (dernier recours de l'upsell), mais utile pour
comparer les classements en décembre et pour permettre aux fronts de classer
les populaires entre eux. La route SNO dépréciée n'est pas modifiée.

### P9 — Fréquence : une fois par nuit, inchangée (2026-09-28)

Le coût est négligeable (une requête groupée par établissement, à 2h). Le
manque de stabilité venait de la remise à zéro complète, pas de la
fréquence. Passer à un calcul par semaine retarderait jusqu'à 7 jours le badge
d'un nouveau plat qui marche.

### P10 — Seuil absolu : 4 (2026-09-28, d'après la prod)

Un score de 4 correspond à environ une commande par semaine. Un produit déjà
populaire le reste jusqu'à 3 (P7). Chiffres et raisonnement en §4.

### P11 — Au moins 3 commandes réelles (2026-09-28, trouvé par les tests)

En plus du score, un produit doit figurer dans au moins 3 commandes valides de
la fenêtre, qu'il soit déjà populaire ou non.

*Pourquoi* : l'extrapolation de P4 donne environ 4,6 à un produit créé la
veille avec une seule vente. Il aurait été marqué populaire. Pour un produit
installé, un score de 4 demande déjà environ 3 commandes : la règle ne change
rien aux chiffres de calibration, elle ne concerne que les nouveaux produits.

## 4. Calibration (phase 0)

Requêtes : [popular-products-calibration-prod.sql](popular-products-calibration-prod.sql),
à exécuter sur la prod en lecture seule. Elles comparent l'algorithme actuel
aux seuils 4, 6, 8 et 12.

Testées sur le staging (2026-09-28). Un seul établissement a des commandes sur
28 jours (21 commandes), donc les résultats ne servent pas à calibrer :
- actuel : 10 produits marqués, dont 6 pizzas, et une catégorie marquée à
  100 % ;
- nouveau, seuil 4 : 2 produits marqués, dans la seule catégorie qui domine
  nettement.

### Résultats de prod (2026-09-28)

8 établissements abonnés ont des commandes valides sur 28 jours (de 23 à 877
commandes). En moyenne, 73 produits et 9 catégories par établissement.

| Variante | Produits marqués (moy.) | Part des produits | Catégories vendues couvertes | Catégories vendues sans populaire | Part max. d'une catégorie (moy.) | Étab. avec une catégorie > 50 % |
|---|---|---|---|---|---|---|
| Actuel | 12,1 | 16,5 % | 77,8 % | 14 | 66,9 % | 4 |
| Seuil 4 | 7,4 | 10,1 % | 66,7 % | 21 | 35,5 % | 1 |
| Seuil 6 | 5,9 | 8,0 % | 55,6 % | 28 | 31,3 % | 1 |
| Seuil 8 | 5,3 | 7,2 % | 49,2 % | 32 | 29,3 % | 1 |
| Seuil 12 | 4,1 | 5,6 % | 41,3 % | 37 | 27,5 % | 1 |

Score du 1er de chaque catégorie qui a vendu (63 catégories) : quartiles 2,3 /
6,6 / 21,1. Le 2e vaut en médiane 69 % du 1er, le 3e 36 %.

Par établissement, catégories couvertes, de l'actuel au seuil 4 : 212 : 8 → 7,
236 : 7 → 7, 226 : 13 → 11, 235 : 6 → 5, 241 : 5 → 6, 231 : 3 → 3, 2 : 3 → 2,
234 : 4 → 1.

### Lecture

- **La concentration disparaît.** Part max. moyenne d'une catégorie : de 67 %
  à 36 %. La seule catégorie restant au-dessus de 50 % (établissement 226) est
  une petite catégorie de 1 ou 2 produits dont l'unique élu vend bien. Ce
  n'est pas le problème de départ.
- **Moins de badges, qui veulent dire plus.** De 12 à 7 produits marqués en
  moyenne (de 16,5 % à 10 % de la carte).
- **Un peu moins de catégories couvertes (78 % → 67 %), et c'est voulu.** Les
  catégories perdues sont celles dont le meilleur produit fait moins
  d'une commande par semaine. Surtout chez les petits volumes : 234 (23
  commandes en 4 semaines) passe de 4 catégories couvertes à 1. L'algorithme
  actuel ne les couvrait qu'en comptant les commandes annulées et les
  lignes plutôt que les commandes, et grâce au top 10 global sans seuil : un
  badge sur un produit vendu une ou deux fois en un mois.
- **Pourquoi 4 et pas plus** : c'est le seuil testé qui couvre le plus de
  catégories. Il reste au-dessus du bruit (au moins 3 commandes réelles, P11),
  et le seuil relatif de 50 % ainsi que le plafond limitent déjà les grands
  établissements. De 4 à 12, les grands établissements (212, 236) changent
  peu. Ce sont les petits qui perdent tout.
- **Pas de seuil adapté au volume de l'établissement.** On pourrait abaisser
  le seuil pour les petits établissements, mais à moins d'une commande par jour,
  aucun produit n'a assez de ventes pour être distingué des autres. Avec
  8 établissements, on n'a pas de quoi le régler. À revoir si le parc grandit.

## 5. Implémentation, phase 1 (2026-09-28)

Tout est dans [internal/tasks/products.go](../internal/tasks/products.go).
Le cron (`0 2 * * *`, [cmd/api/tasks.go](../cmd/api/tasks.go)) ne change pas.

- **Paramètres** : constantes nommées en tête de fichier, chacune renvoyant
  à sa décision (P4, P6, P7, P10, P11).
- **Liste des établissements** : `SELECT DISTINCT`. Un établissement avec
  plusieurs abonnements était traité plusieurs fois par nuit.
- **Candidats** (`popularCandidatesQuery`, `loadPopularCandidates`) : une
  seule requête par établissement renvoie les produits candidats, leur
  catégorie d'affichage, leur flag actuel, la somme pondérée de leurs
  commandes et le nombre de commandes. Syntaxe Postgres uniquement
  (`make_interval`, `extract(epoch ...)`), seul moteur en production.
- **Score** (`popularScore`) : calculé en Go à partir de la somme pondérée et
  de l'âge du produit (formule de P4). Même formule que la requête de
  calibration.
- **Sélection** (`selectPopularProducts`) : fonction pure, testée sans base.
  Par catégorie : tri sur le score (avec l'avantage de 10 % du produit déjà
  populaire), puis `product_id` ; élection jusqu'au plafond des produits qui
  passent les trois seuils (commandes réelles, absolu, relatif au 1er), avec
  la tolérance de 75 % pour un produit déjà populaire.
- **Écriture** : une transaction courte par établissement, deux `UPDATE`
  seulement sur les lignes qui changent. Le retrait vise tout produit marqué
  hors sélection, candidat ou non : un produit désactivé perd son flag.

### Tests

- Unitaires ([internal/tasks/products_test.go](../internal/tasks/products_test.go)) :
  score d'une vente quotidienne (environ 28), produit récent extrapolé,
  plancher de 7 jours, poids des ventes anciennes, plafond selon la taille,
  catégorie dominante qui ne prive plus les autres, seuil absolu, seuil
  relatif, stabilité sur les deux seuils, avantage du produit en place,
  égalités, minimum de 3 commandes.
- Vérifié contre le staging sur une connexion en lecture seule forcée
  (`default_transaction_read_only`), avec une sonde temporaire supprimée
  ensuite : la requête tourne et donne les scores de la calibration
  (établissement 2 : 10,3 et 5,7, les 2 élus). Les deux `UPDATE` passent
  l'analyse et la liaison des paramètres (`= ANY` sur un `[]int64`, liste
  vide comprise) et ne sont bloqués que par la lecture seule.
- Le test d'intégration existant `TestUpdateMerchantPopularProducts_Postgres`
  ne correspondait plus aux règles (commandes à l'état `OPEN`, catégorie sans
  ligne `productcateg`). Réécrit en phase 2.

## 6. Tests d'intégration, phase 2 (2026-09-28)

Dans [internal/tasks/postgres_integration_test.go](../internal/tasks/postgres_integration_test.go),
tag `postgres_integration`. Chaque test crée son propre établissement
sentinelle (`seedTaskMerchant`), ses catégories, produits et commandes, et
supprime tout à la fin.

| Test | Ce qu'il vérifie |
|---|---|
| `TestUpdateMerchantPopularProducts_Postgres` | Plafond (catégorie de 4 : un seul élu), catégories indépendantes, retrait du flag d'un produit qui ne vend plus, d'un produit désactivé et d'un produit retiré de la carte, résultat identique au second passage. |
| `..._InvalidOrdersIgnored_Postgres` | Commandes `canceled` (en minuscules), `DENIED`, `OPEN` et hors fenêtre de 28 jours ignorées ; `DONE` / `COMPLETED` comptée. |
| `..._VariantUsesGroupCategory_Postgres` | Une variante sans catégorie prend celle de son groupe ; le groupe n'est jamais marqué ; un produit sans catégorie ni groupe n'est pas candidat. |
| `..._UpsellLinesCounted_Postgres` | Les lignes `is_upsell` comptent (P3). |
| `..._Hysteresis_Postgres` | À score égal (environ 3,4), le produit déjà populaire le reste, le nouveau n'entre pas. |
| `..._NewProduct_Postgres` | Produit créé il y a 2 jours : 1 vente ne suffit pas (P11), 3 ventes suffisent. |

Lancés sur le staging (décision d'Ilies, 2026-09-28), seulement ces six tests :

```bash
POSTGRES_URL="$RENDER_STAGING_DATABASE_URL" go test -tags postgres_integration ./internal/tasks/ -run 'TestUpdateMerchantPopularProducts' -count=1
```

Résultat : les 6 passent. Aucune donnée de test restante vérifiée ensuite
(établissements `itest-tasks`, catégories `it-%`, commandes `itest` : 0).

**Les tests détectent bien une régression.** Deux bugs introduits
volontairement, un à la fois, puis le fichier restauré :
- filtre sur l'état des commandes retiré : `InvalidOrdersIgnored` échoue
  (commandes annulées, refusées et ouvertes comptées) ;
- tolérance de 75 % ramenée à 100 % : `Hysteresis` échoue.
