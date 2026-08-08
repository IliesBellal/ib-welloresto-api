# 63 — Réconciliation de `04-schema-postgres-target.sql` avec l'état réel de Render staging

Date : 2026-08-07
Branche : `staging`

## Objectif

Mettre `docs/migration-postgres/04-schema-postgres-target.sql` en correspondance **exacte** avec le
schéma réel de l'instance Postgres de staging Render, en repartant d'un `pg_dump --schema-only` de
cette instance. Documenter chaque écart trouvé et corrigé.

**Aucune donnée réelle n'est citée dans ce rapport, et aucune information de connexion (hôte, port,
identifiants) n'y figure.** Seul `04-schema-postgres-target.sql` a été modifié ; rien n'a été
commité, et **aucune modification n'a été faite sur l'instance de staging** (opérations en lecture
seule côté Render).

## 0. Note de méthode

Même protocole d'accès qu'aux [rapports 51 §0](51-render-staging-chunked-load.md),
[58 §0](58-render-staging-schema-sync.md) et [62 §0](62-staging-fresh-data-rehearsal.md) : la chaîne
de connexion a été lue **une seule fois** depuis `.vscode/launch.json` (fichier local couvert par
`.gitignore`) et écrite dans un fichier temporaire hors dépôt ; toutes les commandes suivantes n'ont
référencé que le **chemin** de ce fichier. Fichier supprimé en fin de session (§8).

`pg_dump` n'est pas installé localement ; il a été exécuté via Docker. L'instance de staging tourne
en **PostgreSQL 18.4** — l'image `postgres:16` disponible localement a refusé le dump (un `pg_dump`
plus ancien ne lit pas un serveur plus récent), `postgres:18` a été récupérée pour l'occasion. Aucun
export manuel n'a donc été nécessaire de la part de l'utilisateur.

## 1. Méthode de comparaison : deux dumps produits par le même sérialiseur

Comparer un fichier écrit à la main à un dump généré n'est pas fiable : les différences d'écriture
(`varchar(64)` vs `character varying(64)`, ordre des clauses, indentation) se mélangent aux vraies
divergences. Le protocole retenu neutralise cela :

1. `pg_dump --schema-only --no-owner --no-privileges --schema=public` de **staging** ;
2. chargement de `04-schema-postgres-target.sql` dans un **PostgreSQL 18 jetable** (conteneur
   Docker), puis `pg_dump` avec **exactement les mêmes options** ;
3. comparaison des deux instances via **cinq inventaires de catalogue identiques** interrogés des
   deux côtés (colonnes, contraintes, index, types énumérés, commentaires), plus une comparaison
   normalisée dump-à-dump.

Les deux côtés passent ainsi par le même sérialiseur : toute différence restante est structurelle.

Contrôle de fidélité du dump : la liste des tables extraite du dump de staging est **strictement
identique** à celle lue en direct dans `information_schema` (192 lignes, diff vide).

## 2. Écart de périmètre constaté d'entrée : 192 tables, pas 187

La consigne annonçait 187 tables et 3 FK. Mesure réelle au moment de ce chantier :

| Élément | Annoncé | Constaté |
|---|---|---|
| Tables (`BASE TABLE`, schéma `public`) | 187 | **192** |
| Clés étrangères | 3 | **3** ✅ |

Les 5 tables supplémentaires sont les `import_*_mapping` posées par
`migrations/080_import_provider_mappings.up.sql`, appliquées sur staging **entre le rapport 62
(ce matin) et ce chantier**. C'est la même dérive que celle relevée au [rapport 62 §1](62-staging-fresh-data-rehearsal.md)
(181 → 184 → 187 → 192) : le nombre de tables avance plus vite que les rapports qui le citent.
**Le chiffre n'est donc jamais à reprendre d'un rapport antérieur, il est à remesurer.**

Le compte de FK, lui, était exact : 3, inchangées.

## 3. État initial du fichier de suivi : 185 tables

Le fichier chargé dans le Postgres jetable produisait **185 tables**, contre 192 sur staging. Aucune
table présente dans le fichier n'était absente de staging — la dérive est à sens unique, le fichier
était en retard.

## 4. Écarts trouvés et corrigés

### 4.1 Sept tables absentes du fichier de suivi

| Table | Origine | Notes |
|---|---|---|
| `import_products_mapping` | `migrations/080` | Nées directement en PostgreSQL, |
| `import_categories_mapping` | `migrations/080` | aucune contrepartie MySQL |
| `import_tags_mapping` | `migrations/080` | |
| `import_attributes_mapping` | `migrations/080` | |
| `import_attribute_options_mapping` | `migrations/080` | |
| `outbound_messages` | `migrations/072` | Jamais reportée dans le fichier |
| `planning_published_shift_snapshots` | `migrations/073` | Jamais reportée dans le fichier |

Ajoutées avec leurs colonnes, clés primaires, index (2 par table `import_*`, 2 pour
`outbound_messages`) et commentaires de catalogue.

### 4.2 Huit colonnes absentes du fichier de suivi

| Table | Colonne | Type réel sur staging | Origine |
|---|---|---|---|
| `employees` | `display_order` | `integer NOT NULL DEFAULT 0` | `migrations/done/068` |
| `kiosks` | `device_id` | `varchar(191) DEFAULT NULL` | `migrations/done/062` |
| `marketing_categories` | `image_url` | `varchar(512)` | `migrations/075` |
| `productcateg` | `image_url` | `varchar(512)` | `migrations/075` |
| `planning_settings` | `planning_sms_notifications_enabled` | `boolean NOT NULL DEFAULT false` | `migrations/073` |
| `planning_settings` | `sunday_multiplier` | `numeric(4,2) NOT NULL DEFAULT 1.00` | `migrations/076` |
| `planning_settings` | `premium_cumulation_mode` | `varchar(16) NOT NULL DEFAULT 'highest'` | `migrations/077` |
| `planning_settings` | `night_sunday_combined_multiplier` | `numeric(4,2)` | `migrations/076` |

Toutes déclarées **en fin de table**, position qu'elles occupent réellement sur staging : posées par
`ALTER TABLE ADD COLUMN`, elles ne peuvent qu'être en dernière position en PostgreSQL.

Cas notable — `employees.display_order` : la migration MySQL d'origine écrit
`ADD COLUMN display_order INT ... AFTER position_id`, ce qui la placerait en 8ᵉ position. PostgreSQL
n'a pas de clause `AFTER` : sur staging elle est en 37ᵉ (dernière) position. **Le fichier suit
staging, pas la migration MySQL.**

### 4.3 Un index absent

`idx_employees_merchant_display_order (merchant_id, display_order)` — posé avec la colonne
`display_order`, jamais reporté.

### 4.4 Une largeur de colonne divergente

`kiosks.os_version` : `varchar(50)` dans le fichier, **`varchar(120)` sur staging**. Corrigé sur la
valeur de staging (voir §5, aucune migration ne porte cet élargissement).

### 4.5 Une valeur d'énumération absente

`planning_shifts_status_enum` : 4 valeurs dans le fichier, **5 sur staging** (`draft` en plus).
Reproduite dans le fichier — avec un avertissement explicite (voir §5).

### 4.6 Ordre de colonnes divergent sur deux tables

| Table | Colonnes | Position dans le fichier | Position réelle |
|---|---|---|---|
| `discounts` | `discount_scope` | 7 | **22** |
| `orders` | `cart_discount_id`, `cart_discount_code`, `cart_discount_amount` | 26-28 | **46-48** |

Ces colonnes avaient été insérées « logiquement » (à côté des colonnes voisines par le sens) par le
[rapport 57](57-discount-redemptions-schema.md). Sur staging elles ont été posées par
`ALTER TABLE ADD COLUMN` et sont donc en fin de table. Déplacées en fin de déclaration, ce qui
réaligne aussi les 19 positions ordinales décalées par ricochet.

L'ordre ordinal n'est sans conséquence ni pour le chargeur de données (il émet des listes de
colonnes explicites) ni pour les requêtes Go (aucune ne fait `SELECT *` sur ces tables), mais c'est
une divergence mesurable entre le fichier de suivi et la réalité : elle est corrigée.

### 4.7 Quatorze commentaires de catalogue absents

Cinq `COMMENT ON TABLE` et huit `COMMENT ON COLUMN` sur les tables `import_*`, plus le
`COMMENT ON COLUMN configurable_attribute_options.title` de la migration 081. Ajoutés à l'identique.

## 5. Quatre anomalies réelles de staging, reproduites mais signalées

Le fichier de suivi doit refléter staging : ces quatre points y ont donc été reproduits **tels
quels**, chacun accompagné d'un commentaire d'avertissement dans le SQL. Ce sont des anomalies de
l'instance, pas du fichier — **elles se corrigent sur staging, pas ici.**

### 5.1 `migrations/081` appliquée à moitié

`migrations/081_widen_configurable_attribute_options_title.up.sql` contient deux instructions :

```sql
ALTER TABLE configurable_attribute_options ALTER COLUMN title TYPE varchar(80);
COMMENT ON COLUMN configurable_attribute_options.title IS '... Elargi de 25 a 80 caracteres ...';
```

Sur staging, **seul le `COMMENT` a pris effet** : la colonne est restée en `varchar(25)` tout en
portant un commentaire qui annonce 80. L'instance se contredit elle-même.

C'est l'anomalie la plus conséquente du lot : la migration 081 existe précisément parce que
`varchar(25)` fait échouer l'import de catalogue en dur (erreur Postgres 22001 sur un libellé
d'option un peu long, là où MySQL tronquait silencieusement) — c'est-à-dire exactement le chemin de
code en cours de développement sur cette branche. **L'import de produits en masse reste donc exposé
au défaut que la migration 081 était censée supprimer.** Correctif : rejouer l'`ALTER` de la
migration 081 sur staging, puis passer `title` à `varchar(80)` dans le fichier de suivi.

### 5.2 `planning_shifts_status_enum` porte une valeur `draft` illégitime

`draft` n'existe ni dans le DDL MySQL source (`planning_shifts.status` y est
`enum('planned','confirmed','done','cancelled')`), ni dans aucun fichier de `migrations/`, ni dans
aucune documentation. La valeur `draft` appartient à **`planning_weeks_status_enum`**
(`enum('draft','published','locked')`), qui la porte légitimement, et le code Go ne l'utilise que
pour `planning_weeks`.

Diagnostic : `ALTER TYPE ... ADD VALUE 'draft'` appliqué directement sur staging, très
vraisemblablement par confusion entre les deux énumérations. Aucun code ne lit ni n'écrit cette
valeur sur `planning_shifts`. À retirer de staging plutôt qu'à adopter — sachant que PostgreSQL ne
sait pas retirer une valeur d'un `ENUM` : il faudra recréer le type et remapper la colonne.

### 5.3 `kiosks.os_version` élargie hors migration versionnée

`varchar(50)` dans le DDL MySQL source et dans `migrations/done/037_kiosk_module.up.sql`,
**`varchar(120)` sur staging**. Aucun fichier du dépôt ne porte cet élargissement. Contrairement aux
élargissements des migrations 066/069/070/081, celui-ci n'a pas de trace écrite.

Conséquence pratique faible (un élargissement ne casse rien), mais il manque une migration : une
instance reconstruite à partir de `migrations/` obtiendrait `varchar(50)` et pourrait rejeter des
valeurs que staging accepte.

### 5.4 Deux tables hors convention `timestamptz`

`outbound_messages` (`sent_at`, `updated_at`) et `planning_published_shift_snapshots`
(`published_at`, `created_at`, plus `start_time`/`end_time` en `time`) sont en **`timestamp` sans
fuseau**, alors que tout le reste du schéma est en `timestamptz` — règle de conversion posée en
en-tête du fichier de suivi. C'est l'état réel de staging, hérité du texte des migrations 072 et
073, qui ont été écrites en style MySQL.

Reproduit tel quel. L'alignement sur `timestamptz` demande une migration dédiée, hors périmètre de
ce chantier de réconciliation.

### 5.5 Point mineur — un `COMMENT ON INDEX` de la migration 080 absent

`migrations/080` se termine par un `COMMENT ON INDEX uq_import_products_mapping_provider_external`
qui n'existe pas sur staging, alors que tout le reste du fichier 080 y est. Signalé en commentaire
dans le fichier de suivi, non reproduit (le fichier suit staging). Deuxième cas d'application
partielle d'une migration, après le 5.1.

## 6. Revalidation

### 6.1 Parité de catalogue : 0 différence sur 2 733 entrées

Après correction, le fichier rechargé dans le PostgreSQL 18 jetable donne :

| Inventaire | Staging | Fichier cible | Différences |
|---|---|---|---|
| Colonnes (type, longueur, précision, nullabilité, défaut, identity, position) | 1 926 | 1 926 | **0** |
| Contraintes (`pg_get_constraintdef` complet, hors `NOT NULL`) | 202 | 202 | **0** |
| Index (`indexdef` complet) | 313 | 313 | **0** |
| Types énumérés (valeurs et ordre) | 14 | 14 | **0** |
| Commentaires de catalogue | 178 | 178 | **0** |
| Vues (définition normalisée) | 1 | 1 | **0** |
| **Total** | **2 634** | **2 634** | **0** |

Tables : **192 / 192**. Clés étrangères : **3 / 3**.

### 6.2 Comparaison dump-à-dump

Les deux `pg_dump` normalisés (commentaires de fichier, `SET` de session et jetons
`\restrict` retirés) :

```
instructions staging=792  fichier cible=792
seulement_staging=0  seulement_cible=0
```

**Les deux schémas sérialisent à l'identique, instruction par instruction.**

### 6.3 Validation `pglast`

Parseur PostgreSQL réel (`pglast` v8.2, binding de `libpg_query`), même contrôle qu'aux
[rapports 13 §4](13-merchant-id-schema-update.md) et [18 §4](18-order-id-schema-update.md) :

```
pglast v8.2 - parsing OK
instructions = 512
  CreateStmt             192
  CommentStmt            178
  IndexStmt              125
  CreateEnumStmt         14
  TransactionStmt        2
  ViewStmt               1
```

**Fichier entier reparsé sans erreur.** Les 192 `CreateStmt` et 14 `CreateEnumStmt` recoupent les
comptages catalogue ; les 2 `TransactionStmt` sont le `BEGIN`/`COMMIT` encadrant le fichier.

Le fichier se charge par ailleurs sans erreur dans un PostgreSQL 18 vierge avec
`psql -v ON_ERROR_STOP=1` — validation plus forte qu'un simple parsing, puisqu'elle vérifie aussi la
résolution des types, l'ordre de déclaration et la validité des index.

## 7. Ampleur de la correction

```
docs/migration-postgres/04-schema-postgres-target.sql | 183 insertions(+), 6 deletions(-)
```

Un seul fichier modifié.

## 8. Nettoyage

Supprimés en fin de session : les deux dumps `--schema-only`, les inventaires de catalogue et
fichiers de diff, les fichiers de requêtes SQL temporaires, **le fichier contenant la chaîne de
connexion**, et le conteneur PostgreSQL 18 jetable. L'image `postgres:18` récupérée est conservée
dans le cache Docker local (elle resservira aux prochains contrôles de schéma).

## 9. Synthèse

| Point | Résultat |
|---|---|
| Dump `--schema-only` de staging | ✅ via Docker `postgres:18` (serveur en 18.4), fidélité vérifiée contre `information_schema` |
| Méthode de comparaison | Deux dumps par le **même** sérialiseur + 6 inventaires de catalogue |
| Tables absentes du fichier | **7** ajoutées (5 `import_*_mapping`, `outbound_messages`, `planning_published_shift_snapshots`) |
| Colonnes absentes | **8** ajoutées |
| Index absent | **1** ajouté |
| Largeur divergente | **1** corrigée (`kiosks.os_version`) |
| Valeur d'énumération absente | **1** ajoutée (`planning_shifts_status_enum.draft`, signalée comme illégitime) |
| Ordre de colonnes divergent | **2 tables** réalignées (`discounts`, `orders`) |
| Commentaires de catalogue absents | **14** ajoutés |
| Anomalies de staging signalées, non « corrigées » dans le fichier | **5** (§5) — dont migration 081 appliquée à moitié |
| Parité finale | ✅ **0 différence sur 2 634 entrées de catalogue**, 192/192 tables, 3/3 FK |
| Comparaison dump-à-dump | ✅ 792/792 instructions, 0 écart |
| Validation `pglast` | ✅ 512 instructions, parsing sans erreur |
| Modifications sur l'instance de staging | **Aucune** (lecture seule) |
| Fichiers commités | Aucun |

**`04-schema-postgres-target.sql` reflète désormais exactement l'état réel de Render staging.** Deux
suites sont à traiter hors de ce chantier : rejouer l'`ALTER` de la migration 081 sur staging (§5.1,
le plus urgent — il bloque le chemin d'import en cours de développement), et statuer sur les trois
autres anomalies d'instance (§5.2 à §5.4).
