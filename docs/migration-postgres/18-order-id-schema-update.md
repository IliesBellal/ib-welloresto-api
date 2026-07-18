# 18 — Conversion des colonnes `order_id` vivantes en `varchar` vers `INTEGER` (schéma cible)

Exécution du chantier scopé par [16-order-id-format-check.md](16-order-id-format-check.md) §2 et
préparé côté Go par [17-order-id-unification.md](17-order-id-unification.md) (zéro site Go vivant
typé `int` restant sur `order_id`). Seul le schéma Postgres cible
([04-schema-postgres-target.sql](04-schema-postgres-target.sql)) est modifié. **Aucun fichier `.go`
touché.**

## 1. Les 6 colonnes converties

Reprises telles que listées au rapport 16 §2 ("En `varchar` — à converger", sous-ensemble vivant) :

| Table | Colonne | Avant | Après | Nullable |
|---|---|---|---|---|
| `orderitems` | `order_id` | `varchar(20) NOT NULL` | `integer NOT NULL` | non — **dans la PK composite**, voir §2 |
| `orders` | `parent_order_id` | `varchar(50)` | `integer` | oui |
| `customer_loyalty_progress_order` | `order_id` | `varchar(30) NOT NULL` | `integer NOT NULL` | non |
| `customer_rewards` | `used_on_order_id` | `varchar(20)` | `integer` | oui |
| `stock_movements` | `order_id` | `varchar(50)` | `integer` | oui |
| `upsell_suggestions` | `order_id` | `varchar(64)` | `integer` | oui |

Les 2 colonnes `varchar` restantes du même inventaire (`order_changes_log.order_id`,
`order_ratings.order_id`) **ne sont pas touchées** : ce sont les 2 colonnes classées orphelines par
[03-table-usage-audit.md](03-table-usage-audit.md) — hors périmètre du rapport 16 §2 et de ce
chantier. Vérifié après coup : elles restent `varchar(25)`/`varchar(255)`.

## 2. `orderitems.order_id` — la PK composite, vérifiée intacte

`orderitems` a pour PK la clause table-level :

```sql
PRIMARY KEY (order_item_id, order_id, product_id)
```

Une clause `PRIMARY KEY (colA, colB, colC)` ne porte aucune déclaration de type — elle référence les
colonnes par nom. Changer le type de `order_id` (une colonne membre) ne modifie donc **pas** la
définition syntaxique de la contrainte ; seul le type de la colonne membre change, ce qui est
exactement l'effet recherché (les 3 colonnes de la PK sont maintenant toutes `integer` :
`order_item_id integer GENERATED ALWAYS AS IDENTITY`, `order_id integer`, `product_id integer`).

**Vérifié par reparsing AST** (§4) : la contrainte `Constraint(contype=CONSTR_PRIMARY, keys=(order_item_id, order_id, product_id))`
est retrouvée à l'identique après modification, sur les mêmes 3 colonnes.

Point relevé au rapport 16 (non traité ici, hors périmètre schéma) : l'upsert
`INSERT … ON DUPLICATE KEY UPDATE` sur cette PK ([order_life_cycle/repository.go:1309](../../internal/modules/order_life_cycle/repository.go#L1309))
devra être réécrit en `ON CONFLICT` pour Postgres — c'est un chantier Go séparé, indépendant du
type de la colonne.

## 3. Pas de `CHECK (order_id >= 0)` — décision motivée

Le pattern `CHECK (col >= 0)` du schéma cible n'est **pas** un ajout générique à toute colonne `_id`
entière : il est réservé, par la règle documentée en tête de fichier (ligne 9) et confirmée dans
[04-schema-mapping-notes.md](04-schema-mapping-notes.md), aux colonnes dont la **source MySQL était
explicitement `UNSIGNED`** — la perte du contrôle `UNSIGNED` lors du passage à `INTEGER` (qui accepte
les négatifs) est alors compensée par un `CHECK`. Seuls 2 cas existent dans tout le schéma :
`delivery_position.delivery_session_id` et `delivery_session.current_order_id`, tous deux
`int(10) UNSIGNED` côté MySQL.

Vérification faite sur les 6 colonnes converties ici, dans
[wello-resto-mysql-ddl.md](wello-resto-mysql-ddl.md) :

| Colonne | Type MySQL source |
|---|---|
| `orderitems.order_id` | `varchar(20)` |
| `orders.parent_order_id` | `varchar(50)` |
| `customer_loyalty_progress_order.order_id` | `varchar(30)` |
| `customer_rewards.used_on_order_id` | `varchar(20)` |
| `stock_movements.order_id` | `varchar(50)` |
| `upsell_suggestions.order_id` | `varchar(64)` |

Aucune n'était `UNSIGNED` — aucune ne l'a jamais été, puisqu'elles étaient toutes `varchar`. Ajouter
un `CHECK >= 0` ici inventerait une contrainte absente de la source, sans précédent dans le schéma :
les colonnes entières FK-like voisines qui référencent elles aussi un identifiant auto-increment
(`product_id integer NOT NULL`, `customer_id integer NOT NULL`, `booking_id integer NOT NULL`, et
même `orders.order_id` — la PK cible elle-même, qui n'a **pas** de `CHECK`) n'en portent aucun non
plus. **Décision : pas de `CHECK` ajouté**, pour rester cohérent avec le pattern existant plutôt que
d'en créer un nouveau.

## 4. Revalidation complète du fichier avec `pglast`

```
$ python -c "
import pglast
sql = open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8').read()
stmts = pglast.parse_sql(sql)
print(f'OK — {len(stmts)} statements parsed successfully')
"
OK — 453 statements parsed successfully
```

Le fichier complet (types ENUM, 180 `CREATE TABLE`, `COMMENT ON`, index) parse sans erreur —
inchangé en nombre d'instructions par rapport à avant ce chantier (aucune instruction ajoutée ou
supprimée, seulement 6 types de colonnes modifiés).

Vérification ciblée de la PK composite via l'AST (`pglast.ast`) :

```python
for c in node.tableElts:  # CreateStmt de la table orderitems
    print(type(c).__name__, getattr(c, "contype", None), getattr(c, "keys", None))
# ...
# Constraint 6 (<String sval='order_item_id'>, <String sval='order_id'>, <String sval='product_id'>)
```

`contype=6` correspond à `CONSTR_PRIMARY` : la PK composite est retrouvée intacte, sur les 3
colonnes attendues.

## 5. Vérification du périmètre : rien d'autre modifié

```
$ grep -n "order_id integer" docs/migration-postgres/04-schema-postgres-target.sql
```
retourne exactement les 6 colonnes ci-dessus en plus des 12 déjà `integer` avant ce chantier
(inventaire du rapport 16 §2) — aucune nouvelle occurrence parasite.

```
$ git diff docs/migration-postgres/04-schema-postgres-target.sql | grep -iE "order_id"
```
affiche exactement 6 paires `-`/`+`, une par colonne du §1 — aucune autre ligne du fichier n'a été
touchée par cette session.

`order_changes_log.order_id` (`varchar(25)`), `order_ratings.order_id` (`varchar(255)`),
`orders.cash_register_id` (`varchar(11)`) et `payments.cash_register_id` (`varchar(20)`) confirmés
inchangés — les deux premiers parce qu'orphelins (hors périmètre du rapport 16 §2), les deux
derniers parce qu'explicitement exclus par la consigne de ce chantier.

Aucun fichier `.go` modifié. Aucune migration MySQL touchée.

## 6. Synthèse

| Question | Réponse |
|---|---|
| Colonnes converties | 6 : `orderitems.order_id`, `orders.parent_order_id`, `customer_loyalty_progress_order.order_id`, `customer_rewards.used_on_order_id`, `stock_movements.order_id`, `upsell_suggestions.order_id` |
| PK composite `orderitems` | `(order_item_id, order_id, product_id)` — intacte, vérifiée par reparsing AST |
| `CHECK (order_id >= 0)` | Non ajouté — le pattern est réservé aux colonnes ex-`UNSIGNED` MySQL ; aucune des 6 ne l'était |
| Validation `pglast` | 453 instructions, fichier entier reparse sans erreur |
| `cash_register_id` (orders, payments) | Non touché, conforme à la consigne |
| Fichiers `.go` | Aucun touché |

Ce chantier clôt, côté schéma, la convergence de type `order_id` amorcée par les rapports 16-17 :
les 6 colonnes vivantes en varchar référençant `orders.order_id` sont maintenant `integer`, alignées
sur les 12 colonnes qui l'étaient déjà et sur la PK elle-même. Reste hors périmètre, à traiter dans
un chantier dédié : la migration des données (`CAST` des valeurs existantes, contrôle préalable des
résidus non numériques signalé au rapport 16 §2), la réécriture de l'upsert `orderitems` en
`ON CONFLICT`, et la décision sur la colonne hybride `cash_register_id`.
