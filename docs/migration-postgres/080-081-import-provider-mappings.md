# 080 / 081 — Tables de mapping d'import + élargissement de `configurable_attribute_options.title`

Phase 2 de la feature « Importer des produits » (back-office → API). Phase 1 a extrait les cœurs de création
partagés (`insertProductTx`, `insertAttributeTx`, cf. commit `1b0028f`). **Cette phase est purement DDL** :
aucune logique applicative, aucun endpoint, aucun fichier Go touché.

Le besoin vient de l'audit [`docs/audit-import-produits.md`](../audit-import-produits.md) §3.5 : aucune des
tables cibles de l'import (`products`, `productcateg`, `tags`, `configurable_attributes`,
`configurable_attribute_options`) ne porte de colonne `provider` / `external_id` / `sku`. Sans traçabilité, un
import est **non idempotent** (rejouer le même fichier recrée tout), **non réversible** (impossible d'identifier
ce qu'un import donné a créé) et **non reprenable** après échec partiel.

## 1. Livrables

| Fichier | Contenu |
|---|---|
| [`migrations/080_import_provider_mappings.up.sql`](../../migrations/080_import_provider_mappings.up.sql) | 5 `CREATE TABLE` + 5 index UNIQUE + 5 index de lookup inverse + commentaires |
| [`migrations/080_import_provider_mappings.down.sql`](../../migrations/080_import_provider_mappings.down.sql) | `DROP TABLE IF EXISTS` des 5 tables |
| [`migrations/081_widen_configurable_attribute_options_title.up.sql`](../../migrations/081_widen_configurable_attribute_options_title.up.sql) | `ALTER COLUMN title TYPE varchar(80)` |
| [`migrations/081_widen_configurable_attribute_options_title.down.sql`](../../migrations/081_widen_configurable_attribute_options_title.down.sql) | Retour à `varchar(25)` + avertissement d'échec sur données longues |

**Numéros `080` / `081`** : vérifiés libres dans `migrations/` (max `079`) et dans `migrations/done/` (max `068`).

**DDL Postgres uniquement**, conformément à la convention des migrations récentes portant un en-tête explicite
(`074_planning_enabled_boolean_postgres`, `078_password_resets`, `079_…_ingredient_link`) : `timestamptz`,
`now()`, `GENERATED ALWAYS AS IDENTITY`, `IF NOT EXISTS`, `COMMENT ON`. Les migrations `062`→`073` étaient encore
en syntaxe MySQL (`MODIFY`, `tinyint(1)`, `AFTER`) ; `075`→`077` sont neutres.

**Divergence assumée avec le patron 079** : ce dernier mettait aussi à jour
[`04-schema-postgres-target.sql`](04-schema-postgres-target.sql). Ce n'est **pas** fait ici, sur consigne
explicite — cette baseline est désynchronisée (migration `067_haccp_traceability` manquante, cf.
[`docs/decisions.md`](../decisions.md)). **Les migrations sont la source de vérité.**

**Découpage en deux migrations** : les 5 tables de mapping relèvent d'un même concern (traçabilité d'import) et
sont créées ensemble ; l'élargissement de `title` est un concern distinct (contrainte de schéma existante, avec
son propre risque au rollback) et vit dans sa propre migration réversible indépendamment.

## 2. Migration 080 — tables `import_*_mapping`

Cinq tables, une par entité cible, calquées sur le patron `integration_uber_eats_*_mapping` /
`integration_deliveroo_*_mapping` ([`04-schema-postgres-target.sql`](04-schema-postgres-target.sql) ~L1666-1733).

```
id             integer GENERATED ALWAYS AS IDENTITY NOT NULL  -- PK
merchant_id    varchar(64)  NOT NULL
provider       varchar(32)  NOT NULL   -- 'zelty', 'wello-generic', ...
external_id    varchar(64)  NOT NULL   -- ID chez le provider (ex. Zelty ZD1557688)
wello_id       <typé sur la PK cible>  NOT NULL
creation_date  timestamptz  NOT NULL DEFAULT now()
deletion_date  timestamptz  NULL
enabled        boolean      NOT NULL DEFAULT true
```

### Typage de `wello_id` (vérifié sur la base réelle, pas sur la baseline)

| Table de mapping | Cible | PK cible | `wello_id` |
|---|---|---|---|
| `import_products_mapping` | `products` | `product_id` integer | `integer` |
| `import_categories_mapping` | `productcateg` | `categ_id` integer | `integer` |
| `import_tags_mapping` | `tags` | `tag_id` varchar(42) | `varchar(42)` |
| `import_attributes_mapping` | `configurable_attributes` | `id` varchar(64) | `varchar(64)` |
| `import_attribute_options_mapping` | `configurable_attribute_options` | `id` integer | `integer` |

Requête de contrôle utilisée (jointure `pg_index` / `pg_attribute` sur les 5 tables cibles) — résultat conforme
aux valeurs ci-dessus.

⚠️ **Piège catégories** : `wello_id` porte `productcateg.categ_id` (la vraie PK, integer), et **non**
`productcateg.merchant_categ_id` — le `varchar(20)` par lequel `products.category` référence réellement une
catégorie. La résolution `categ_id` → `merchant_categ_id` est à la charge de l'applicatif (Phase 3+).
C'est consigné en `COMMENT ON COLUMN` pour que l'écart ne se perde pas.

### Choix de conception

**Tables dédiées plutôt que colonnes sur les entités.** Ajouter `provider`/`external_id` sur `products` &
consorts imposerait une migration sur des tables chaudes du menu pour un besoin qui ne concerne que le chemin
d'import. Le découplage suit le patron `integration_*` déjà en place.

**Une table par entité plutôt qu'une table polymorphe.** C'est ce que fait le patron existant, et cela permet de
typer `wello_id` correctement (integer vs varchar) au lieu de tout stocker en texte — les tables `integration_*`
historiques stockent `product_id varchar(50)` alors que la PK est un integer ; on ne reproduit pas ce défaut.

**`UNIQUE (merchant_id, provider, external_id)`** = clé d'idempotence. `provider` en fait partie : deux providers
peuvent légitimement émettre le même `external_id`.

**L'unique est volontairement sans filtre sur `enabled`.** Un mapping désactivé continue donc de bloquer un
ré-import du même `external_id`, ce qui évite de recréer un doublon d'une entité supprimée côté Wello.
Réactiver un mapping est une décision explicite. (Un index partiel `WHERE enabled` aurait l'effet inverse.)

**`INDEX (merchant_id, wello_id)`** = lookup inverse : « d'où vient cette entité ? » et parcours de rollback d'un
import. Préfixé par `merchant_id` car aucune requête ne cherchera un `wello_id` hors scope marchand, et le pool
applicatif est plafonné à 1 connexion ([`internal/database/postgres.go`](../../internal/database/postgres.go)) —
un seq scan y bloquerait l'unique connexion.

**Aucune FK créée**, convention du chantier de migration. Les FK candidates sont listées en en-tête du `.up.sql`.

## 3. Migration 081 — `title` varchar(25) → varchar(80)

`configurable_attribute_options.title` était la plus courte colonne de libellé du menu (`products.name` est en
varchar(255), `configurable_attributes.title` en varchar(80)). 25 caractères ne tiennent pas des libellés réels :
`"Supplement fromage de chevre"` fait 28 caractères. En MySQL non-strict le dépassement était tronqué
silencieusement ; **Postgres lève une erreur dure 22001**, ce qui ferait échouer un import entier sur un simple
libellé un peu long.

80 aligne la colonne sur `configurable_attributes.title`, dont elle est le pendant côté option.

Élargir un `varchar(n)` en `varchar(m)` avec `m > n` **ne réécrit pas la table** en Postgres (changement de typmod
seul) : opération instantanée, `ACCESS EXCLUSIVE` bref.

### Rollback risqué — assumé et documenté

Le `down` **échouera** dès qu'un libellé stocké dépassera 25 caractères. Aucune troncature automatique n'est
faite : elle ferait perdre de la donnée métier sans trace. La requête de contrôle est dans le `.down.sql` :

```sql
SELECT id, configurable_attribute_id, title, length(title)
FROM configurable_attribute_options
WHERE length(title) > 25
ORDER BY length(title) DESC;
```

## 4. Vérification d'application

Menée sur un **clone schéma-seul** de la base de dev (`pg_dump --schema-only` → base jetable
`import_mig_check`, 186 tables, 0 ligne), et non sur `welloresto_dev` directement : deux sessions y étaient
actives, et la base de dev partagée ne doit pas être mutée par une vérification. La base jetable a été supprimée
après coup ; `welloresto_dev` est resté intact (contrôlé : 0 table `import_*`, `title` toujours varchar(25)).

| Étape | Résultat |
|---|---|
| `080.up` puis `081.up` | appliqués sans erreur (`ON_ERROR_STOP=1`) |
| `\d import_products_mapping` | 8 colonnes, PK, UNIQUE + index wello_id présents |
| `\d configurable_attribute_options` | `title` en `character varying(80)` |
| Typage `wello_id` × 5 | conforme au tableau §2 |
| Index × 5 tables | 3 par table (pkey + uq + idx), 15 au total |
| Doublon `(merchant, provider, external_id)` | **rejeté** — `duplicate key value violates unique constraint` |
| Même `external_id`, autre `provider` | accepté |
| Même couple, autre `merchant_id` | accepté |
| Libellé 28 caractères | accepté (refusé avant 081) |
| Libellé 81 caractères | rejeté — borne à 80 respectée |
| Ré-application de `080.up` / `081.up` | no-op propre (`NOTICE ... already exists, skipping`), données préservées |
| `081.down` **avec** un libellé de 28 car. | **échec attendu** — `value too long for type character varying(25)` |
| `081.down` après purge des libellés longs | OK — `title` revenu à `varchar(25)` |
| `080.down` | OK — 5 `DROP TABLE` |
| État final | 186 tables, 0 table `import_*` — identique à l'état initial |

Commandes :

```bash
# Clone schéma-seul (lecture seule sur la base de dev)
docker exec welloresto-postgres-dev bash -c "
  psql -U welloresto -d postgres -c 'CREATE DATABASE import_mig_check;'
  pg_dump -U welloresto --schema-only --no-owner --no-privileges welloresto_dev > /tmp/schema.sql
  psql -U welloresto -d import_mig_check -v ON_ERROR_STOP=1 -f /tmp/schema.sql"

# Application
docker exec -i welloresto-postgres-dev psql -U welloresto -d import_mig_check -v ON_ERROR_STOP=1 \
  -f - < migrations/080_import_provider_mappings.up.sql
docker exec -i welloresto-postgres-dev psql -U welloresto -d import_mig_check -v ON_ERROR_STOP=1 \
  -f - < migrations/081_widen_configurable_attribute_options_title.up.sql

# Inspection
docker exec welloresto-postgres-dev psql -U welloresto -d import_mig_check -c "\d import_products_mapping"
docker exec welloresto-postgres-dev psql -U welloresto -d import_mig_check -c "\d configurable_attribute_options"

# Nettoyage
docker exec welloresto-postgres-dev psql -U welloresto -d postgres -c "DROP DATABASE import_mig_check;"
```

## 5. Ce que cette phase ne fait pas

- Aucun code Go : rien ne lit ni n'écrit encore ces tables.
- `04-schema-postgres-target.sql` non mis à jour (consigne explicite, baseline désynchronisée).
- Pas de FK, pas de trigger, pas de purge automatique des mappings.
- L'alignement du `status` produit à l'import (`'available'` / `'removed_from_menu'`) reste prévu en Phase 5 —
  cf. le commentaire posé sur `CreateProduct` en Phase 1.
