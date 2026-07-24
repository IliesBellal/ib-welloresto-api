# Inventaire par module — ordre de conversion Postgres — 07

> Fait suite à [03-table-usage-audit.md](03-table-usage-audit.md) (tables vivantes vs orphelines). Objectif : donner, module par module (`internal/modules/*` et `internal/webhook/*`), un score de risque de conversion MySQL → Postgres, et un ordre de conversion recommandé. Analyse en lecture seule — aucun fichier source modifié.

## Méthode

Pour chaque module, comptage par grep exhaustif sur tout le sous-arbre Go du module :

- **Sites d'appel SQL** : occurrences de `.Query(`, `.QueryContext(`, `.QueryRow(`, `.QueryRowContext(`, `.Exec(`, `.ExecContext(` — ce sont les seules méthodes `database/sql` utilisées dans ce repo (confirmé par sondage sur `menu/repository.go`).
- **Placeholders dynamiques** : sites où le SQL est construit avec `strings.Repeat` (génération de `?,?,?` pour des clauses `IN (...)`) ou `fmt.Sprintf` injectant du SQL (nom de colonne/table, `IN (%s)`, `SET %s`, `ORDER BY` dynamique). Les `fmt.Sprintf` utilisés pour des logs, du hashing ou du formatage non-SQL ont été exclus après inspection manuelle (sinon le score aurait été gonflé artificiellement — ce filtrage concerne surtout `menu` et `pos`).
- **Fonctions de date MySQL-spécifiques** : `DATE_FORMAT`, `TIMESTAMPDIFF`, `TIMESTAMPADD`, `UTC_TIMESTAMP`, `CONVERT_TZ`, `NOW()`, `DATE_ADD`, `DATE_SUB`, `FROM_UNIXTIME`, etc. — chacune n'a pas d'équivalent syntaxique direct en Postgres (à réécrire avec `to_char`, `age`/`extract`, `now() AT TIME ZONE`, `AT TIME ZONE`, `+ interval`, `to_timestamp`...).
- **`ON DUPLICATE KEY UPDATE`** : à réécrire en `INSERT ... ON CONFLICT ... DO UPDATE` (sémantique proche mais syntaxe différente, et nécessite une contrainte unique explicite en cible).
- **Procédure stockée (`CALL ...`)** : le corps de la procédure n'est pas dans ce repo (MySQL only) — nécessite un portage manuel séparé, hors du scope "traduire une requête Go".
- **Tests** : présence de `_test.go` dans le sous-arbre du module (filet de sécurité pendant la conversion).

### Formule de score de risque

```
score = (nb sites SQL)
      + 2 × (nb placeholders dynamiques)
      + 3 × (nb fonctions de date MySQL)
      + 1 × (nb ON DUPLICATE KEY UPDATE)
      + 5 × (1 si le module appelle une procédure stockée, sinon 0)
```

Poids choisis pour refléter le coût de réécriture réel : un site SQL de plus est linéaire (bug faible), un placeholder dynamique double le risque (SQL construit par concaténation = fragile face au changement de dialecte de placeholders `?` → `$1`), une fonction de date MySQL triple le risque (pas de traduction 1:1, erreurs de fuseau horaire fréquentes), `ON DUPLICATE` est un risque modéré et local, une procédure stockée ajoute un forfait de +5 car elle bloque la conversion tant que son corps SQL n'est pas récupéré et réécrit indépendamment du code Go.

## Modules exclus

- **`admin`** : un seul fichier (`upsell_handler.go`), aucun accès SQL direct — délègue tout à `internal/tasks`. Zéro surface de migration.
- **`bookingcomm`** : orchestration d'envoi d'e-mails/SMS (Brevo) uniquement, aucune requête SQL dans le module lui-même (dépend de `mailer.Service`/`sms.Service`, pas de repository). Zéro surface de migration.

Aucun autre module n'a été exclu : chaque module restant est associé à au moins une table "vivante" identifiée dans [03-table-usage-audit.md](03-table-usage-audit.md) (aucun module ne travaille exclusivement sur les 37 tables orphelines).

## Procédures stockées identifiées (risque transverse)

Quatre modules appellent des procédures stockées MySQL dont le corps n'est pas dans ce repo — **à extraire de la prod (`SHOW CREATE PROCEDURE ...`) avant toute conversion Postgres** de ces modules, car elles peuvent contenir elles-mêmes des fonctions de date ou des lectures de tables invisibles au grep statique (cf. `average_distribution_time_by_category`/`_history`, déjà signalé dans 03) :

| Procédure | Modules appelants |
|---|---|
| `GET_AVERAGE_DISTRIBUTION_TIME` | `order_life_cycle`, `orders`, `ubereats` |
| `GET_POS_STATUS` | `pos`, `scannorder` |
| `GET_CASH_REGISTER_REPORT`, `GET_CASH_REGISTER_REPORT_MOP` | `cash_registers` |

## Tableau — du plus simple au plus complexe

| # | Module | Sites SQL | Placeholders dyn. | Fonctions date MySQL | ON DUP KEY | Proc. stockée | Tests | **Score** |
|---|---|---|---|---|---|---|---|---|
| 1 | `bookingevents` | 1 | 0 | 0 | 0 | non | non | **1** |
| 1 | `webhook/deliveroo_menu` | 1 | 0 | 0 | 0 | non | non | **1** |
| 1 | `allergens` | 1 | 0 | 0 | 0 | non | non | **1** |
| 4 | `receipt` | 3 | 0 | 0 | 0 | non | non | **3** |
| 5 | `user_services` | 4 | 0 | 0 | 0 | non | non | **4** |
| 6 | `bookingcore` | 2 | 0 | 1 (UTC_TIMESTAMP) | 0 | non | oui (3) | **5** |
| 6 | `planning/daycomments` | 4 | 0 | 0 | 1 | non | oui (1) | **5** |
| 8 | `audit` | 3 | 0 | 1 (NOW) | 0 | non | non | **6** |
| 8 | `messaggio` | 2 | 0 | 1 (DATE_FORMAT+UTC_TIMESTAMP) | 1 | non | non | **6** |
| 10 | `googlemaps` | 3 | 0 | 1 (DATE_FORMAT+UTC_TIMESTAMP) | 1 | non | non | **7** |
| 10 | `printers` | 7 | 0 | 0 | 0 | non | non | **7** |
| 12 | `upsell` | 7 | 0 | 1 (DATE_SUB/NOW) | 0 | non | non | **10** |
| 13 | `translation` | 9 | 0 | 0 | 2 | non | non | **11** |
| 14 | `webhook/brevo_sms_reply` | 3 | 0 | 3 (UTC_TIMESTAMP) | 0 | non | oui (1) | **12** |
| 14 | `tags` | 12 | 0 | 0 | 0 | non | non | **12** |
| 16 | `discounts` | 14 | 0 | 0 | 0 | non | non | **14** |
| 17 | `integrations` | 17 | 0 | 0 | 0 | non | non | **17** |
| 18 | `notification` | 6 | 0 | 4 (DATE_SUB/ADD, UTC_TIMESTAMP) | 0 | non | non | **18** |
| 19 | `availabilities` | 15 | 2 | 0 | 0 | non | oui (1) | **19** |
| 20 | `webhook/ubereats` | 15 | 0 | 2 (UTC_TIMESTAMP) | 0 | non | non | **21** |
| 20 | `webhook/deliveroo_orders` | 21 | 0 | 0 | 0 | non | non | **21** |
| 22 | `stats` | 12 | 0 | 4 (DATE_FORMAT, CONVERT_TZ) | 0 | non | oui (1) | **24** |
| 23 | `deliveroo` | 11 | 0 | 5 (UTC_TIMESTAMP, DATE_ADD) | 0 | non | non | **26** |
| 24 | `reservation` | 19 | 0 | 3 (UTC_TIMESTAMP) | 0 | non | non | **28** |
| 25 | `locations` | 22 | 0 | 3 (UTC_TIMESTAMP) | 0 | non | non | **31** |
| 26 | `webhook/stripe` | 24 | 0 | 7 (UTC_TIMESTAMP, FROM_UNIXTIME) | 0 | non | non | **45** |
| 27 | `auth` | 16 | 1 | 9 (UTC_TIMESTAMP, NOW) | 1 | non | oui (2) | **46** |
| 28 | `stocks` | 31 | 0 | 6 (UTC_TIMESTAMP, DATE_FORMAT) | 0 | non | non | **49** |
| 29 | `users` | 42 | 1 | 3 (UTC_TIMESTAMP) | 0 | non | oui (1) | **53** |
| 30 | `cash_registers` | 32 | 3 | 5 (UTC_TIMESTAMP) | 1 | **oui** | non | **59** |
| 31 | `orders` | 19 | 14 | 4 (UTC_TIMESTAMP) | 0 | **oui** | oui (2) | **64** |
| 32 | `delivery_sessions` | 42 | 5 | 6 (UTC_TIMESTAMP) | 0 | non | non | **70** |
| 33 | `ubereats` | 24 | 0 | 14 (DATE_ADD, UTC_TIMESTAMP, NOW) | 2 | **oui** | non | **73** |
| 34 | `scannorder` | 31 | 0 | 14 (DATE_ADD, UTC_TIMESTAMP, NOW) | 0 | **oui** | non | **78** |
| 35 | `kiosk` | 39 | 2 | 12 (UTC_TIMESTAMP) | 1 | non | oui (1) | **80** |
| 36 | `customers` | 50 | 4 | 8 (UTC_TIMESTAMP) | 0 | non | oui (1) | **82** |
| 37 | `haccp` ⁽²⁾ | 60 | 9 | 2 (UTC_TIMESTAMP) | 0 | non | oui (4) | **84** |
| 38 | `pos` (+ accounting, reports) | 47 | 8 | 20 (DATE_FORMAT, UTC_TIMESTAMP) | 1 | **oui** | non | **129** |
| 39 | `menu` | 109 | 11 | 1 (UTC_TIMESTAMP) | 3 | non | oui (1) | **137** |
| 40 | `bookings` | 58 | 3 | 27 (UTC_TIMESTAMP, DATE_FORMAT, TIMESTAMPDIFF) | 2 | non | oui (7) | **147** |
| 41 | `order_life_cycle` | 69 | 7 | 26 (UTC_TIMESTAMP, TIMESTAMPADD) | 4 | **oui** | oui (3) | **170** |
| 42 | `planning` (12 sous-packages) | 143 | 2 | 11 (DATE_FORMAT, TIMESTAMPDIFF, DATE_ADD, CONVERT_TZ, NOW) | 2 | non | oui (22) | **182** |

`planning` regroupe `documents`, `employees`, `leave`, `performance`, `refs`, `revenueforecast`, `schedule`, `settings`, `shifttemplates`, `swaps`, `timeentries`, `weektemplates` — un seul module fonctionnel avec un seul owner de schéma, décomposer la conversion en sous-packages est possible en pratique (voir recommandations) mais le score global reflète la charge totale de la migration planning.

`planning/daycomments` (table `planning_day_comments`, migration 065) est un **13ᵉ sous-package**, ajouté **après** cet audit initial — il n'est donc pas compté dans les "12 sous-packages"/le score 182 de la ligne `planning` ci-dessus, et suit son propre cycle de conversion indépendant (score **5**, ligne dédiée dans le tableau) plutôt que d'être noyé dans l'agrégat Tier 4. Détail dans [26-planning-day-comments-integration.md](26-planning-day-comments-integration.md).

⁽²⁾ `haccp` : ligne **recomptée en entier** (pas seulement ajustée du delta) par le
[rapport 56](56-haccp-traceability-integration.md), suite à l'ajout de
`CreateTraceabilityRecord`/`ListTraceabilityRecords`/`GetTraceabilityRecord`/`HasTraceabilityRecords`/
`findTraceabilityPhotosByRecordIDs` (tables `haccp_traceability_records`/`haccp_traceability_photos`,
migration 067, postérieure à la conversion Tier 3 de ce module — commit `0b4509f`, après le commit
`94e6bf0` "Tier 3"). Ancienne ligne : 51 sites / 6 placeholders / 2 fonctions date / 0 ON DUPLICATE /
non / oui (3) / score **69**. Nouvelle ligne (même méthode grep que [§ Méthode](#méthode) ci-dessus,
ré-exécutée sur tout le sous-arbre `internal/modules/haccp/` hors fichiers `_test.go`) : 60 sites (+9,
dont 7 directement attribuables aux 5 nouvelles fonctions ci-dessus, écart résiduel de 2 non
réconcilié précisément — voir rapport 56 §6), 9 placeholders dynamiques (+3, dont le nouveau site
`strings.Repeat`/`fmt.Sprintf` de `findTraceabilityPhotosByRecordIDs` pour la clause `IN (...)` sur
les `record_id`), fonctions de date et `ON DUPLICATE` inchangés (les nouvelles requêtes ne posent
`created_at`/`updated_at` que côté Go, `time.Now().UTC()`, comme `planning_day_comments`), aucune
procédure stockée, 4 fichiers de test (`postgres_integration_test.go` exerce désormais aussi les 4
fonctions de lecture/écriture ci-dessus, contre 3 déjà présents avant ce chantier). Score recalculé
**84** (inchangé de Tier — toujours dans la fourchette 51–100) : la ligne est déplacée de son
ancienne position (rang 32, entre `orders` et `delivery_sessions`) à sa nouvelle position par score
croissant (rang 37, entre `customers` et `pos`), les rangs intermédiaires décalés en conséquence.

## Ordre de conversion recommandé

**Tier 1 — quasi gratuit (score ≤ 10), à faire en premier pour valider l'outillage/patterns de conversion** : `bookingevents`, `webhook/deliveroo_menu`, `allergens`, `receipt`, `user_services`, `bookingcore`, `planning/daycomments`, `audit`, `messaggio`, `googlemaps`, `printers`, `upsell`. Aucun de ces modules n'a de procédure stockée ni de placeholders dynamiques ; la plupart n'ont qu'une seule fonction de date `UTC_TIMESTAMP()` (→ `now() AT TIME ZONE 'UTC'` ou `timezone('UTC', now())`) ou aucune. Bon lot pour établir la convention de traduction MySQL→Postgres (driver, placeholders `?`→`$1`, gestion des erreurs `sql.ErrNoRows`) sans risque métier significatif. `planning/daycomments` est le seul de ce lot dont le seul chemin d'UPDATE est un `ON DUPLICATE KEY UPDATE` (upsert) — aucune fonction de date MySQL dans ses requêtes (`created_at`/`updated_at` sont posés côté Go via `time.Now().UTC()`, pas en SQL).

**Tier 2 — risque faible à modéré (11–50)** : `translation`, `webhook/brevo_sms_reply`, `tags`, `discounts`, `integrations`, `notification`, `availabilities`, `webhook/ubereats`, `webhook/deliveroo_orders`, `stats`, `deliveroo`, `reservation`, `locations`, `webhook/stripe`, `auth`, `stocks`. `auth` mérite une attention particulière malgré son score modéré : il gère les tokens/MFA/sessions, donc les tests existants (`last_login_test.go`, `pin_test.go`) doivent rester verts à chaque étape. `webhook/stripe` a le plus de fonctions de date de ce tier (`FROM_UNIXTIME` — Stripe envoie des epochs Unix, vérifier la conversion `to_timestamp()` still en UTC).

**Tier 3 — risque élevé (51–100), nécessite plus de tests avant conversion** : `users`, `cash_registers` (⚠️ procédure stockée `GET_CASH_REGISTER_REPORT*`), `orders` (⚠️ le plus de placeholders dynamiques du repo — 14 — cohérent avec le passé `orders_fetcher_builder` déjà audité en [02-security-fix-orders-builder.md](02-security-fix-orders-builder.md) ; ⚠️ procédure stockée `GET_AVERAGE_DISTRIBUTION_TIME`), `haccp`, `delivery_sessions`, `ubereats` (⚠️ procédure stockée), `scannorder` (⚠️ procédure stockée `GET_POS_STATUS`), `kiosk`, `customers`. Pour les modules à procédure stockée de ce tier, extraire et traduire la procédure MySQL **avant** de commencer le module Go correspondant — sinon le travail de traduction des requêtes Go sera bloqué en fin de parcours par une dépendance non résolue.

**Tier 4 — cœur métier, à traiter en dernier avec double couverture de tests (> 100)** : `pos` (⚠️ procédure stockée `GET_POS_STATUS`, partagée avec `scannorder` — coordonner la traduction une seule fois pour les deux), `menu` (le plus de placeholders dynamiques après `orders`, cœur du catalogue produit), `bookings` (le plus de fonctions de date MySQL après `order_life_cycle`/`planning` — beaucoup de logique de calcul de créneaux/disponibilité sensible au fuseau horaire, bonne couverture de tests existante à exploiter), `order_life_cycle` (⚠️ procédure stockée partagée avec `orders`/`ubereats` — score le plus élevé après `planning`, cœur du cycle de vie commande/paiement), `planning` (score le plus élevé du repo, 92 fichiers Go, 22 fichiers de tests déjà en place — recommandé de le découper par sous-package dans l'ordre `refs` → `settings` → `employees` → `documents` → `shifttemplates`/`weektemplates` → `schedule` → `timeentries` → `leave`/`swaps` → `performance`/`revenueforecast`, du référentiel le plus statique vers la logique métier la plus dynamique).

**Règle transverse pour tout le repo** : traiter les 4 procédures stockées (`GET_AVERAGE_DISTRIBUTION_TIME`, `GET_POS_STATUS`, `GET_CASH_REGISTER_REPORT`, `GET_CASH_REGISTER_REPORT_MOP`) comme un chantier séparé et préalable — récupérer leur corps SQL en prod, vérifier si elles référencent des tables orphelines (cf. doute déjà documenté dans 03 pour `GET_AVERAGE_DISTRIBUTION_TIME`), et les réécrire en fonctions Postgres (`CREATE FUNCTION`/`CREATE PROCEDURE`) ou en requêtes Go directes avant de convertir les 6 modules qui en dépendent.
