# 57 — Intégration `discount_redemptions` + colonnes `discount_scope`/`max_redemptions*`/`cart_discount_*`

Contexte : le [rapport 56](56-haccp-traceability-integration.md) (§ Partie 3, vérification élargie)
a balayé l'intégralité des `CREATE TABLE` de `migrations/done/` et trouvé une troisième table
manquante de `04-schema-postgres-target.sql`, en plus de `planning_day_comments` (rapport 26) et
`haccp_traceability_records`/`_photos` (rapport 56) : **`discount_redemptions`**, créée par
[`migrations/done/041_cart_discounts.up.sql`](../../migrations/done/041_cart_discounts.up.sql) (déjà
exécutée), avec deux `ALTER TABLE` associés sur `discounts` et `orders` dans la même migration.

Différence avec les deux précédentes : recherche exhaustive dans `internal/` (déjà faite au rapport
56 §Partie 3, reconfirmée ici) — **aucun repository Go ne lit ni n'écrit** cette table ni ces
colonnes. Ce chantier suit donc la même méthode que le rapport 56, à l'exception du point « vivante »
(§3) et de la conversion de code (§4, sans objet). Aucune donnée réelle citée ci-dessous. **Rien
commité.**

---

## 1. Traduction DDL — `04-schema-postgres-target.sql`

### Source MySQL (`migrations/done/041_cart_discounts.up.sql`, déjà exécutée)

```sql
ALTER TABLE discounts
    ADD COLUMN discount_scope ENUM('PRODUCT', 'ORDER_TOTAL') NOT NULL DEFAULT 'PRODUCT' AFTER discount_code,
    ADD COLUMN max_redemptions INT NULL DEFAULT NULL,
    ADD COLUMN max_redemptions_per_customer INT NULL DEFAULT NULL;

CREATE TABLE discount_redemptions (
    id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    discount_id          VARCHAR(64) NOT NULL,
    order_id             BIGINT UNSIGNED NOT NULL,
    merchant_id          VARCHAR(64) NOT NULL,
    customer_id          VARCHAR(64) NULL,
    amount_applied_cents INT NOT NULL,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_discount_order (discount_id, order_id),
    KEY idx_discount_redemptions_discount (discount_id),
    KEY idx_discount_redemptions_customer (discount_id, customer_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE orders
    ADD COLUMN cart_discount_id VARCHAR(64) NULL DEFAULT NULL AFTER HT,
    ADD COLUMN cart_discount_code VARCHAR(64) NULL DEFAULT NULL AFTER cart_discount_id,
    ADD COLUMN cart_discount_amount INT NOT NULL DEFAULT 0 AFTER cart_discount_code;
```

La migration porte elle-même ce commentaire, conservé ici car il justifie une décision de traduction
importante (§1.4) :

> Pas de FOREIGN KEY vers `discounts`/`orders`/`orders.order_id` : ces tables sont legacy (créées
> avant cet outil de migration), leurs types exacts de colonnes ne sont pas garantis ici. On sécurise
> via une UNIQUE KEY + des index simples.

### 1.1 Nouveau type ENUM

`discount_scope` traduit selon la règle `ENUM(...) -> CREATE TYPE <table>_<colonne>_enum` — inséré en
ordre alphabétique dans le bloc `TYPES ENUM` en tête de fichier, entre `cleaning_surfaces_frequency_unit_enum`
et `employees_role_enum` :

```sql
CREATE TYPE discounts_discount_scope_enum AS ENUM ('PRODUCT', 'ORDER_TOTAL');
```

### 1.2 `discounts` — 3 colonnes ajoutées

Insérées à la même position relative que la migration MySQL (`discount_scope` juste après
`discount_code`, comme `AFTER discount_code` l'indique ; `max_redemptions`/`max_redemptions_per_customer`
en fin de table — sans `AFTER` explicite dans la migration, MySQL les ajoute en queue de table au
moment de leur clause, après `creation_date`) :

```sql
CREATE TABLE discounts (
    discount_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    discount_name varchar(50) NOT NULL,
    discount_desc varchar(100) NOT NULL,
    prefered_order integer NOT NULL DEFAULT 0,
    discount_code varchar(20),
    discount_scope discounts_discount_scope_enum NOT NULL DEFAULT 'PRODUCT',
    discount_order_type varchar(40),
    ...
    creation_date timestamptz NOT NULL DEFAULT now(),
    max_redemptions integer,
    max_redemptions_per_customer integer,
    PRIMARY KEY (discount_id)
);
```

`INT NULL DEFAULT NULL` → `integer` nullable (le `DEFAULT NULL` est implicite en Postgres pour une
colonne nullable, omis comme ailleurs dans le fichier). Pas de `CHECK (>= 0)` : ces deux colonnes sont
`INT` signé (pas `UNSIGNED`) côté MySQL — aucune contrainte perdue à compenser.

### 1.3 `orders` — 3 colonnes ajoutées

Insérées juste après `HT` (chaîne `AFTER HT` → `AFTER cart_discount_id` → `AFTER cart_discount_code`,
donc les trois consécutives à cet endroit précis), avant `delivery_fees` :

```sql
CREATE TABLE orders (
    ...
    TVA integer NOT NULL,
    HT integer NOT NULL,
    cart_discount_id varchar(64),
    cart_discount_code varchar(64),
    cart_discount_amount integer NOT NULL DEFAULT 0,
    delivery_fees integer NOT NULL DEFAULT 0,
    ...
);
```

### 1.4 Nouvelle table `discount_redemptions`

```sql
CREATE TABLE discount_redemptions (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL CHECK (id >= 0),
    discount_id varchar(64) NOT NULL,
    order_id bigint NOT NULL CHECK (order_id >= 0),
    merchant_id varchar(64) NOT NULL,
    customer_id varchar(64),
    amount_applied_cents integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_discount_redemptions_uq_discount_order ON discount_redemptions (discount_id, order_id);
CREATE INDEX idx_discount_redemptions_idx_discount_redemptions_discount ON discount_redemptions (discount_id);
CREATE INDEX idx_discount_redemptions_idx_discount_redemptions_customer ON discount_redemptions (discount_id, customer_id);
```

Règles appliquées (identiques au reste du fichier) :

- `BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY` → `bigint GENERATED ALWAYS AS IDENTITY NOT NULL
  CHECK (id >= 0)` — même motif que `product_ratings.id`/`product_ratings.order_rating_id` déjà
  présents dans le fichier (perte du `UNSIGNED`, compensée par un `CHECK`).
- `order_id BIGINT UNSIGNED NOT NULL` → `bigint NOT NULL CHECK (order_id >= 0)` (colonne `*_id`,
  règle standard).
- Noms d'index : `idx_/uq_<table>_<nom_mysql>` — les trois noms MySQL préfixés du nom de table
  restent sous 63 caractères sans troncature ni dédoublonnage nécessaire ici (41/58/58 caractères).
- **Aucune contrainte `FOREIGN KEY` créée** — décision **volontairement alignée sur la migration
  MySQL elle-même**, qui n'en crée aucune non plus (commentaire cité plus haut). Trois relations
  candidates identifiées mais non matérialisées :
  - `discount_id` → `discounts.discount_id` : types compatibles (`varchar(64)` vs `varchar(50)`
    côté cible, plus large — aucun risque de troncature), simple candidate non créée par choix du
    migrateur d'origine.
  - `order_id` → `orders.order_id` : **incohérence de type réelle**. `discount_redemptions.order_id`
    est `bigint` (issu de `BIGINT UNSIGNED`) alors que `orders.order_id` cible est `integer` (source
    MySQL : `int(11)` signé, `wello-resto-mysql-ddl.md:2022`). Une vraie FK échouerait à la création
    (`bigint` référençant une PK `integer` exige une conversion implicite que Postgres autorise pour
    les entiers, contrairement à un mismatch `varchar`/`integer`, mais reste un signal que les types
    n'ont jamais été alignés délibérément — cohérent avec l'aveu du commentaire de la migration).
  - `customer_id` → `customer.customer_id` : **incohérence de type plus marquée**.
    `discount_redemptions.customer_id` est `varchar(64)` alors que `customer.customer_id` cible est
    `integer` (source MySQL : `int(11)`, `wello-resto-mysql-ddl.md:731`). Une vraie FK serait ici
    **impossible** à créer telle quelle (types incompatibles). Écart du même type que ceux catalogués
    au [rapport 15](15-fk-type-mismatch-audit.md), mais non ajouté à ce rapport (hors périmètre —
    voir §2 : aucune jointure Go vivante ne traverse cette colonne, donc pas un cas « vivant
    critique » au sens du rapport 15).
  - `merchant_id` → note candidate standard (liste habituelle des tables `*.merchant_id`).
- **Collation non explicite** : contrairement au reste du fichier, la migration ne précise aucun
  `COLLATE` (`ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;` seul). Sans impact sur la traduction : quelle
  que soit la collation `utf8mb4` réellement appliquée par défaut par le serveur à la création
  (`utf8mb4_unicode_ci`, `utf8mb4_general_ci`, ou la collation par défaut du schéma), la règle du
  fichier les replie toutes sur la collation PG par défaut.

### Emplacement dans le fichier

`discount_redemptions` insérée entre `device_link` et `discounts`, par ordre lexicographique strict
(`'_' < 's'` en comparaison caractère par caractère : `discount_redemptions` < `discounts`) — cohérent
avec l'ordre alphabétique de bout en bout annoncé au rapport 26, **sauf** qu'un examen du fichier
montre que ce tri n'est pas garanti à 100 % partout (ex. `orders` apparaît avant `order_changes_log`
alors qu'un tri lexicographique pur donnerait l'inverse — écart préexistant, non introduit par ce
chantier, non corrigé). Le choix retenu ici (tri lexicographique strict, reproductible) est documenté
pour que le prochain chantier sache pourquoi cette insertion précise a été faite à cet endroit plutôt
qu'après `discounts_schedules`.

---

## 2. Audit `ON UPDATE CURRENT_TIMESTAMP`

Vérification des 3 nouvelles définitions de colonnes de la migration pour une clause
`ON UPDATE CURRENT_TIMESTAMP` (ou équivalent) :

- `discount_redemptions.created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP` — pas de `ON UPDATE`.
- `discounts.discount_scope`/`max_redemptions`/`max_redemptions_per_customer` — aucune n'est un
  timestamp, `ON UPDATE` sans objet.
- `orders.cart_discount_id`/`cart_discount_code`/`cart_discount_amount` — idem, aucune n'est un
  timestamp.

**Non applicable** : aucune des colonnes ajoutées par ce chantier ne porte de clause
`ON UPDATE CURRENT_TIMESTAMP` en MySQL. Aucune entrée ajoutée à
[05-on-update-timestamp-audit.md](05-on-update-timestamp-audit.md) — les entrées existantes
(`discounts.valid_from`, ligne 4, `CONFIRMÉ`) ne sont pas concernées par ces nouvelles colonnes et
restent inchangées. `orders` n'a et n'avait aucune colonne `ON UPDATE` répertoriée dans ce document
(vérifié : `grep "orders\."` sur le fichier ne retourne aucune ligne) — les 3 nouvelles colonnes ne
changent pas cet état.

---

## 3. Inventaire des tables — `03-table-usage-audit.md`

Ajoutée à la section « Menu / Produits… / Discounts » (juste après `discounts_schedules`), avec un
statut **distinct** de « vivante » — c'est le point demandé explicitement par la consigne :

```
| `discount_redemptions` ⁽³⁾ | *(aucun — schéma prêt, non câblé côté Go)* |
```

Note ⁽³⁾ ajoutée après ⁽¹⁾ (`planning_day_comments`)/⁽²⁾ (`haccp_traceability_*`), avec la nuance
explicite :

> **Contrairement** à `planning_day_comments`/`haccp_traceability`, ce n'est **pas une table
> vivante** : recherche exhaustive dans `internal/` sans résultat pour une quelconque requête SQL sur
> `discount_redemptions` ou les colonnes ci-dessus — seuls des champs de DTO Go existent
> (`CartDiscountID`/`CartDiscountCode`/`CartDiscountAmount` dans
> `internal/models/create_order_models.go`, `internal/models/orders_model.go`,
> `internal/models/request_objects.go` ; `DiscountScope`/`MaxRedemptions`/`MaxRedemptionsPerCustomer`
> dans `internal/modules/discounts/models.go`), jamais lus ni écrits par
> `internal/modules/discounts/repository.go` (qui gère pourtant déjà `discounts` pour ses autres
> colonnes) ni par aucun autre repository. Statut retenu : **schéma prêt, non câblé côté Go**.

**Méthode de vérification** (reconfirmation de celle du rapport 56 §Partie 3, élargie à
`internal/modules/discounts/models.go` qui n'avait pas été explicitement cité) :

```
grep -rn "discount_redemptions\|CartDiscountID\|CartDiscountCode\|CartDiscountAmount\|discount_scope\|max_redemptions" internal/ --include=*.go \
  | grep -v _test.go | grep -v internal/models/
→ 9 lignes, toutes dans internal/modules/discounts/models.go (champs de struct), aucune dans
  repository.go/service.go/handler.go de ce module ni d'aucun autre.
```

Pas de modification à `07-module-inventory.md` : aucune fonction Go n'existe pour ces
tables/colonnes, donc aucun score de module n'est affecté (contrairement au rapport 56, qui avait dû
recompter le module `haccp` suite à du code déjà écrit).

---

## 4. Code Go

**Aucune conversion nécessaire — rien n'existe encore à convertir**, confirmé par la recherche du §3 :
zéro repository, zéro service, zéro test touchant `discount_redemptions` ou les 6 colonnes ajoutées.
Ce chantier est donc purement un rattrapage de schéma Postgres pour une fonctionnalité MySQL déjà en
production mais pas encore branchée côté application — si/quand le module `discounts` (ou un autre)
implémente la persistance de ces champs, le schéma cible sera déjà prêt, dans les deux dialectes.

---

## 5. Revalidation `pglast`

Même méthode que les rapports précédents :

```
python3 -c "
import pglast
with open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8') as f:
    sql = f.read()
stmts = pglast.parse_sql(sql)
print('PARSE OK -', len(stmts), 'statements')
"
→ PARSE OK - 467 statements
```

462 (dernier compte, rapport 56) + 5 nouveaux (1 `CREATE TYPE` + 1 `CREATE TABLE` + 3 `CREATE INDEX` —
les 3 `ALTER TABLE ADD COLUMN` sur `discounts`/`orders` ne comptent pas comme statements séparés
puisque les colonnes ont été insérées directement dans les `CREATE TABLE` existants, pas ajoutées via
un `ALTER TABLE` séparé dans ce fichier) = 467. Cohérent.

---

## 6. Rechargement complet du Postgres Docker de dev

Conteneur `welloresto-postgres-dev` (Postgres 16, `localhost:5433`, déjà démarré et chargé une
première fois lors du rapport 56) — rechargement **propre** demandé explicitement, donc schéma
public entièrement supprimé puis recréé avant de recharger le fichier complet (pas un ajout
incrémental par-dessus l'ancien schéma) :

```
docker exec -i welloresto-postgres-dev psql -U welloresto -d welloresto_dev -v ON_ERROR_STOP=1 \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
→ NOTICE: drop cascades to 197 other objects ; DROP SCHEMA ; CREATE SCHEMA

docker exec -i welloresto-postgres-dev psql -U welloresto -d welloresto_dev -v ON_ERROR_STOP=1 \
  < docs/migration-postgres/04-schema-postgres-target.sql
→ exit code 0, 467 lignes de sortie (une par instruction), aucune ligne ERROR/WARNING,
  se termine par COMMIT
```

Vérification structurelle des objets concernés directement dans la base rechargée :

```
\d discount_redemptions
                      Table "public.discount_redemptions"
       Column         |           Type           | Nullable |     Default
----------------------+--------------------------+----------+------------------
 id                   | bigint                   | not null | generated always as identity
 discount_id          | character varying(64)    | not null |
 order_id             | bigint                   | not null |
 merchant_id          | character varying(64)    | not null |
 customer_id          | character varying(64)    |          |
 amount_applied_cents | integer                  | not null |
 created_at           | timestamp with time zone | not null | now()
Indexes:
    "discount_redemptions_pkey" PRIMARY KEY, btree (id)
    "idx_discount_redemptions_idx_discount_redemptions_customer" btree (discount_id, customer_id)
    "idx_discount_redemptions_idx_discount_redemptions_discount" btree (discount_id)
    "uq_discount_redemptions_uq_discount_order" UNIQUE, btree (discount_id, order_id)
Check constraints:
    "discount_redemptions_id_check" CHECK (id >= 0)
    "discount_redemptions_order_id_check" CHECK (order_id >= 0)
```

`discounts.discount_scope`/`max_redemptions`/`max_redemptions_per_customer` et
`orders.cart_discount_id`/`cart_discount_code`/`cart_discount_amount` confirmés présents avec les
types attendus par un `\d discounts`/`\d orders` direct sur la base rechargée. Aucune `FOREIGN KEY`
listée pour `discount_redemptions` (conforme au choix du §1.4). `go build ./...` : OK (aucun fichier
`.go` modifié par ce chantier).

---

## Résumé

| Étape | Fichier(s) | Statut |
|---|---|---|
| Traduction DDL | `04-schema-postgres-target.sql` | ✅ 1 table + 1 type ENUM + 6 colonnes sur 2 tables existantes ; aucune FK réelle créée (aligné sur le choix de la migration MySQL) |
| Audit `ON UPDATE` | `05-on-update-timestamp-audit.md` | ✅ non applicable, aucune colonne concernée, aucune entrée ajoutée |
| Inventaire tables | `03-table-usage-audit.md` | ✅ ajoutée avec statut distinct **« schéma prêt, non câblé côté Go »** (pas « vivante ») |
| Code Go | — | ✅ aucune conversion nécessaire, rien n'existe à convertir |
| Validation `pglast` | `04-schema-postgres-target.sql` | ✅ 467 statements, aucune erreur |
| Rechargement Postgres dev | conteneur `welloresto-postgres-dev` | ✅ schéma public purgé puis rechargé intégralement, 0 erreur, structures vérifiées |

**Aucun fichier `.go` modifié. Rien commité.** Reste ouvert, hors périmètre explicite de ce
chantier : les incohérences de type `discount_redemptions.order_id`/`customer_id` vs
`orders.order_id`/`customer.customer_id` (§1.4) ne sont pas un risque actif tant qu'aucun code ne les
traverse, mais mériteraient d'être revues si la fonctionnalité « code promo panier » est un jour
câblée côté repository — à ce moment-là, ce chantier permettra au moins de ne pas re-découvrir une
table manquante en plus d'un éventuel problème de type.
