# 40 — Correction `orders.parent_order_id` et balayage proactif complet des colonnes entières issues d'un `varchar` source

Date: 2026-07-21
Branche: migration/postgres

## Objectif

Deux chantiers dans ce document :

1. Corriger le blocage du rapport [39](39-full-data-load-rehearsal-v3.md) : `orders.parent_order_id`
   revient de `integer` à son type source MySQL (`varchar`), au lieu de tenter d'arbitrer une règle de
   conversion pour les 12 valeurs non numériques.
2. Balayer proactivement, sur le dump réel, **toutes** les colonnes du schéma cible actuellement
   `integer`/`bigint`/`smallint` qui portaient un `varchar`/`char`/`text` côté MySQL source — pas
   seulement `parent_order_id` — pour éviter qu'une 4ᵉ colonne du même genre ne bloque une future
   répétition de chargement.

Aucune donnée réelle n'est citée dans ce document — uniquement noms de tables, de colonnes, de
fichiers, comptages, et des descriptions structurelles (longueur, classes de caractères) pour les
jetons non numériques rencontrés, jamais leur valeur.

## 1. `orders.parent_order_id` : retour à `varchar`

### 1.1 Largeur d'origine

Confirmée dans [wello-resto-mysql-ddl.md](wello-resto-mysql-ddl.md), ligne 2032 :

```
`parent_order_id` varchar(50) DEFAULT NULL COMMENT 'Deliveroo : Previous brand_order_id before remake',
```

`varchar(50)` — exactement la largeur documentée au rapport [18](18-order-id-schema-update.md) §3
avant la conversion vers `integer`.

### 1.2 Modification du schéma cible

[04-schema-postgres-target.sql](04-schema-postgres-target.sql), table `orders` :

```diff
-    parent_order_id integer,
+    parent_order_id varchar(50),
```

Seule cette ligne a été modifiée pour ce point — `git diff` sur le fichier confirme qu'aucune autre
ligne touchant `orders` ou toute autre table n'a été altérée par ce changement (les autres différences
présentes sur le fichier au moment de ce chantier — retrait de `customer.is_migrated` et
`orders.isDelivery`, élargissement de 3 colonnes `varchar` — étaient déjà en cours, non committées,
avant le début de cette tâche ; voir rapports [35](35-dead-columns-removal.md) et migration
`066_widen_varchar_columns`).

Le `COMMENT ON COLUMN orders.parent_order_id` ('Deliveroo : Previous brand_order_id before remake')
reste inchangé — il documente déjà correctement la nature externe de la valeur, cohérente avec un
`varchar`.

Revalidation `pglast` du fichier complet après modification :

```
$ python -c "
import pglast
sql = open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8').read()
stmts = pglast.parse_sql(sql)
print(f'OK — {len(stmts)} statements parsed successfully')
"
OK — 457 statements parsed successfully
```

Le fichier parse sans erreur (le compte de 457 inclut les autres modifications non committées déjà en
cours sur le fichier, décrites ci-dessus — non comparable directement au compte de 453 du rapport 18,
qui datait d'avant ces changements).

### 1.3 Aucun changement requis dans `transform_mysql_csv.py`

Le script classe le type de sortie d'une colonne (`_classify_column_kind`) en lisant directement le
type déclaré dans [04-schema-postgres-target.sql](04-schema-postgres-target.sql) — il n'existe
**aucune liste figée** de colonnes numériques en dur dans le code (contrairement aux colonnes
`merchant_id`, sourcées du rapport 13, ou aux règles `ZERO_DATE_*`/`ROW_DROP_IF_NULL_COLUMNS`, scopées
`(table, colonne)`). Une fois `parent_order_id` redéclarée `varchar(50)` dans le schéma, le script la
classe automatiquement `"text"` au lieu de `"numeric"`, et la valeur passe par
`_sql_quote_string(value)` (guillemetée) plutôt que par le chemin "littéral nu" réservé aux colonnes
numériques — exactement le comportement demandé, sans aucune validation ni tentative de `CAST`
numérique sur cette colonne.

Vérifié directement :

```
$ python -c "
from transform_mysql_csv import load_schema, DEFAULT_SCHEMA, DEFAULT_MERCHANT_REPORT
schema = load_schema(DEFAULT_SCHEMA, DEFAULT_MERCHANT_REPORT)
print(schema['orders'].column_kinds['parent_order_id'])
"
text
```

Avant ce chantier (colonne encore `integer` dans le schéma), la même commande renvoyait `numeric`.
**Aucune ligne de code n'a été modifiée dans
[transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py)** — le mécanisme existant
(déjà utilisé pour toutes les colonnes `varchar` du schéma) suffit.

## 2. Balayage proactif : méthode

### 2.1 Constitution de la liste des colonnes à risque

Un script d'audit structurel (non conservé dans le dépôt, cf. §5) a croisé, pour les 181 tables du
schéma cible :

- chaque colonne actuellement `integer`/`bigint`/`smallint` dans
  [04-schema-postgres-target.sql](04-schema-postgres-target.sql) ;
- contre le type de la colonne de même nom dans la `CREATE TABLE` correspondante de
  [wello-resto-mysql-ddl.md](wello-resto-mysql-ddl.md).

Portée : **389** colonnes `integer`/`bigint`/`smallint` côté cible, **718** colonnes
`varchar`/`char`/`text` côté source, sur les 181 tables communes aux deux DDL (`planning_day_comments`
est absente du DDL MySQL — table ajoutée après coup par le rapport
[26](26-planning-day-comments-integration.md) depuis une migration dédiée, pas depuis le dump
phpMyAdmin ; ses colonnes sont `varchar`/`text`/`date`, aucune n'est numérique — hors périmètre du
risque décrit ici). Contrôle de couverture : chacune des 389 colonnes numériques cible a bien une
colonne de même nom dans la table MySQL source correspondante (0 colonne orpheline / renommée non
retrouvée) — la comparaison nom-à-nom est donc fiable sur l'ensemble du schéma, pas seulement sur le
sous-ensemble déjà documenté par les rapports 16-18.

### 2.2 Résultat du croisement : 6 colonnes concernées, pas plus

| Table | Colonne | Type MySQL source | Type cible actuel | Origine documentée |
|---|---|---|---|---|
| `orderitems` | `order_id` | `varchar(20)` | `integer` | Rapport [18](18-order-id-schema-update.md) |
| `customer_loyalty_progress_order` | `order_id` | `varchar(30)` | `integer` | Rapport 18 |
| `customer_rewards` | `used_on_order_id` | `varchar(20)` | `integer` | Rapport 18 |
| `stock_movements` | `order_id` | `varchar(50)` | `integer` | Rapport 18 |
| `upsell_suggestions` | `order_id` | `varchar(64)` | `integer` | Rapport 18 |
| `sub_cash_registers` | `cash_register_id` | `varchar(20)` | `integer` | Rapport [14](14-tier1-conversion-log.md) (Tier 1, module `user_services`) — **pas** couverte par les rapports 16-18, qui ne portaient que sur l'unification `order_id` |

`orders.parent_order_id` n'apparaît plus dans cette liste : redevenue `varchar` (§1), elle n'est plus
une colonne cible numérique et sort donc mécaniquement du périmètre de ce risque.

Aucune autre colonne issue des Tiers 1 à 4 ([14](14-tier1-conversion-log.md),
[25](25-tier2-conversion-log.md), [27](27-tier3-conversion-log.md), [29](29-tier4-conversion-log.md))
ni de l'unification `merchant_id` ([12](12-merchant-id-unification.md)/[13](13-merchant-id-schema-update.md))
ne correspond à ce schéma de risque : les colonnes `merchant_id` vont dans le sens inverse
(`int` MySQL → `varchar(64)` cible côté Postgres), donc sans risque de jeton non numérique nu sur une
colonne cible numérique — leur valeur transite déjà guillemetée via `rules.merchant_fields`, quel que
soit son contenu.

## 3. Balayage proactif : résultat sur le dump réel

Scan direct du dump réel (`data-migration/migration_welloresto_data.sql`, hors dépôt, jamais committé)
via `iter_dump_rows` (même tokenizer que le générateur), sur les 6 colonnes du §2.2 plus
`orders.parent_order_id` à titre de confirmation post-correction. Pour chaque valeur non-NULL non
numérique rencontrée, seule sa forme structurelle est rapportée (longueur, présence de `:`/`-`/lettres)
— jamais la valeur elle-même.

| Colonne | Lignes totales | `NULL` | Numériques | Non numériques |
|---|---:|---:|---:|---:|
| `orders.parent_order_id` | 32 849 | 32 837 | 0 | **12** — forme : longueur 39, contient `:`, contient `-`, contient des lettres (cohérent avec le rapport 39 : préfixe 2 lettres + `:` + UUID) |
| `orderitems.order_id` | 75 284 | 0 | 75 282 | **2** — forme : longueur 4, contient des lettres (cohérent avec le rapport [38](38-full-data-load-rehearsal-v2.md) : chaîne source `"null"`, distincte du mot-clé SQL `NULL`) |
| `customer_loyalty_progress_order.order_id` | 1 761 | 0 | 1 761 | 0 |
| `customer_rewards.used_on_order_id` | 30 | 25 | 5 | 0 |
| `stock_movements.order_id` | 18 879 | 9 688 | 9 191 | 0 |
| `upsell_suggestions.order_id` | 207 | 206 | 1 | 0 |
| `sub_cash_registers.cash_register_id` | 6 | 0 | 6 | 0 |

**Conclusion du balayage : aucune colonne supplémentaire à risque.** Les deux seules colonnes portant
des valeurs non numériques restent exactement celles déjà rencontrées et déjà traitées :

- `orders.parent_order_id` — corrigée ici en `varchar(50)` (§1), donc plus concernée par ce risque.
- `orderitems.order_id` — déjà couverte par la règle d'exclusion de ligne du rapport 38
  (`ROW_DROP_IF_NULL_COLUMNS`), toujours active, revalidée à l'identique (2 lignes) lors de la
  régénération du §4.

Les 4 colonnes restantes du périmètre élargi (`customer_loyalty_progress_order.order_id`,
`customer_rewards.used_on_order_id`, `stock_movements.order_id`, `upsell_suggestions.order_id`, plus la
colonne Tier 1 nouvellement incluse `sub_cash_registers.cash_register_id`) sont **100 % numériques sur
leurs valeurs non-NULL** — aucune correction, aucun arbitrage nécessaire pour elles.

## 4. Régénération des 147 fichiers

`generate-all-sql` relancé sur le dump réel, schéma cible corrigé (§1), dans un dossier temporaire hors
dépôt :

- **147/147 tables générées, 0 échec** (`failed_tables: {}`).
- `dropped_null_key_rows_by_table: {"orderitems": 2}` — inchangé par rapport aux rapports 38/39 : la
  règle d'exclusion des 2 lignes `orderitems.order_id` se déclenche toujours exactement pareil.
- `dropped_source_columns_by_table: {"customer": ["is_migrated"], "orders": ["isDelivery"]}` —
  inchangé.
- **Total toutes tables : 472 774 lignes** — identique aux rapports 38/39, confirmant que le dump
  source n'a pas changé et que la correction n'a fait perdre ni gagner aucune ligne ailleurs.
- `090_orders.sql` : `orders` génère maintenant intégralement (32 849 lignes, valeur inchangée) sans
  jeton non guillemeté — les 12 tokens Deliveroo sortent désormais correctement guillemetés comme toute
  valeur `varchar`.

### Vérification en conditions réelles (chargement Postgres)

Au-delà de la simple régénération, une répétition de chargement a été rejouée pour confirmer la
correction en conditions réelles (même protocole que les rapports 36/38/39 : reset complet du Postgres
de dev, chargement séquentiel strict `ON_ERROR_STOP=1`, arrêt immédiat au premier échec) :

- Schéma cible chargé : **0 erreur**, 181 tables.
- Chargement séquentiel : **dépasse le point de blocage du rapport 39** (`090_orders.sql` charge
  intégralement, contre l'échec au même fichier précédemment) et progresse jusqu'au fichier **100/147**
  avec succès.
- Comptages exacts sur les 100 tables chargées avant l'arrêt suivant : **100/100, 0 écart** avec les
  `row_counts` du rapport de génération — `orders` (32 849) et `orderitems` (75 282) inclus,
  confirmant que la correction ne modifie aucun comptage ailleurs dans le schéma.
- `orders.parent_order_id` vérifié après chargement : 12 valeurs non NULL, 0 valeur qui matche un motif
  numérique, 12 qui ne matchent pas — cohérent avec le scan du §3, chargées sans erreur car la colonne
  n'est plus `integer`.

**Nouveau blocage rencontré, hors périmètre de cette tâche, non corrigé** : le chargement s'arrête au
fichier **101/147** (`101_planning_shifts.sql`) sur une erreur Postgres d'une **nature différente** de
tout ce qui précède — une valeur chaîne vide (`''`) sur une colonne `enum`
(`planning_shifts_status_enum`) rejetée car elle ne correspond à aucun des libellés de l'énumération.
Ce n'est **pas** une colonne issue d'une conversion `varchar` → `integer` (donc hors périmètre du
balayage des §2-3) — c'est une classe de bug distincte (valeur source vide sur un type `enum` cible
plutôt que numérique). Conformément à la consigne de ce chantier (« ne corrige rien d'autre que
`parent_order_id` »), **aucune modification n'a été faite pour ce nouveau blocage** — il est seulement
signalé ici pour une prochaine session, sur le modèle des rapports 37/38/39 (audit dédié aux colonnes
`enum` du schéma, à envisager si ce schéma de risque devait se reproduire).

## 5. Nettoyage

Les 147 fichiers `.sql` régénérés (contenant de vraies données) ont été supprimés du dossier temporaire
à la fin de la session, ainsi que leur copie dans le conteneur Postgres de dev utilisée pour le
chargement (`/tmp/load` et fichiers de log associés), le script d'audit croisé DDL/schéma et le script
de scan du dump (tous deux locaux, non conservés). Aucun fichier de sortie contenant de vraies données
n'a été conservé. Le Postgres de dev est laissé dans l'état atteint par cette répétition (100 tables
chargées avec succès, `planning_shifts` et les 46 tables suivantes vides ou absentes de données) —
aucune remise à zéro supplémentaire n'a été demandée après l'arrêt, cohérent avec le précédent du
rapport 39. **Rien n'a été commité.**

## 6. Synthèse

| Question | Réponse |
|---|---|
| Largeur source `orders.parent_order_id` | `varchar(50)` — confirmée dans `wello-resto-mysql-ddl.md` |
| Correction appliquée | `04-schema-postgres-target.sql` : `parent_order_id integer` → `parent_order_id varchar(50)` (1 ligne) |
| Changement dans `transform_mysql_csv.py` | Aucun — la classification est pilotée par le schéma, déjà vérifiée automatiquement correcte |
| Colonnes balayées (schéma entier, integer/bigint ← varchar/char/text source) | 6 au total : les 5 restantes de l'unification `order_id` (rapport 18) + `sub_cash_registers.cash_register_id` (Tier 1, nouvellement identifiée) |
| Colonnes à risque confirmées | 2 : `orders.parent_order_id` (corrigée ici) et `orderitems.order_id` (déjà couverte par la règle du rapport 38) |
| Colonnes vérifiées saines (100 % numérique sur le non-NULL) | 5 : `customer_loyalty_progress_order.order_id`, `customer_rewards.used_on_order_id`, `stock_movements.order_id`, `upsell_suggestions.order_id`, `sub_cash_registers.cash_register_id` |
| Régénération | 147/147, 0 échec, 472 774 lignes au total — identique aux rapports 38/39 |
| Chargement réel | Dépasse le blocage du rapport 39 (`orders` charge intégralement) ; progresse à 100/147, 0 écart de comptage sur les tables chargées |
| Nouveau blocage (101/147, `planning_shifts`) | Nature différente (`enum` + chaîne vide, pas une colonne numérique) — signalé, **non corrigé**, hors périmètre de cette tâche |
| Fichiers commités | Aucun |

Point ouvert pour la suite : le blocage `101_planning_shifts.sql` (valeur vide sur une colonne `enum`)
reste à traiter dans une prochaine session, selon le même protocole d'arbitrage que les rapports 37
(sentinel de date), 38 (`orderitems.order_id`) et celui-ci (`parent_order_id`) — un audit dédié aux
colonnes `enum` du schéma cible, sur le même modèle que le balayage numérique fait ici, est une option
si ce nouveau schéma de risque devait se reproduire sur d'autres colonnes `enum`.
