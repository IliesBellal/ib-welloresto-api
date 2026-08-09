# 082 — Correction de type `order_item_configuration.configuration_attribute_id`

## 0. Contexte — incident production

Log de production (Render, `POST /orders/create`, `2026-08-09T10:06:32Z`) :

```
ERROR: invalid input syntax for type integer: "attribute-4131d883-3370-426e-9f2a-7ed898fc2021" (SQLSTATE 22P02)
```

Stacktrace : `OrdersLifeCycleRepository.CreateOrder` (`repository.go:1189`) →
`insertExtrasWithoutsConfigs` → `BulkInsertConfigs`. Toute commande dont au moins un produit a une
configuration (attributs/options — ex. "Taille", "Sauce") échoue en 500 dès qu'elle atteint cette
étape, **après** que la commande de base (`orders`/`orderitems`) a déjà été insérée en base
(pas de transaction autour de `CreateOrder` — cf. commentaires `//tx.Rollback()` en commentaire
dans le code, non actifs).

## 1. Cause racine

`order_item_configuration.configuration_attribute_id` est de type `integer`, alors que
`configurable_attributes.id` (la colonne qu'elle référence fonctionnellement) est `varchar(64)`
et contient des IDs préfixés applicatifs (`attribute-<uuid>`, `helpers.AttributeIDPrefix`).

Ce n'est **pas** une régression de la migration MySQL → Postgres : le DDL MySQL d'origine avait
déjà `configuration_attribute_id int(11)` face à `configurable_attributes.id varchar(64)`
(voir `docs/migration-postgres/wello-resto-mysql-ddl.md:2110-2116` vs `:676`). L'incohérence était
déjà documentée en lecture seule dans
[15-fk-type-mismatch-audit.md §1.1](15-fk-type-mismatch-audit.md#11-cœur-commandes--cassent-le-fetcher-de-commandes)
et dans un commentaire de `internal/modules/stocks/postgres_integration_test.go` — mais jamais
corrigée.

En MySQL non-strict, `INSERT ... VALUES ('attribute-xxxx', ...)` dans une colonne `int(11)`
tronquait silencieusement la valeur à `0` (avec un warning ignoré). En Postgres, la même insertion
lève une erreur dure (`22P02`) : la tolérance qui masquait le bug a disparu avec la bascule vers
Postgres (069+), pas la cause.

**Portée du bug** : les deux chemins qui écrivent dans cette colonne sont impactés de façon
identique —
- `OrdersLifeCycleRepository.CreateOrder` → `insertExtrasWithoutsConfigs` → `BulkInsertConfigs`
  (`repository.go:2219`)
- `UpdateOrderItem` → bulk insert `order_item_configuration` (`repository.go:1555-1561`)

Le code Go n'a pas besoin d'être modifié : `models.ConfigInsert.AttributeID` et
`ConfigAttribute.ID` (`internal/models/orders_model.go:62`,
`internal/models/create_order_models.go:113`) sont déjà typés `string` côté Go. Seul le schéma
Postgres est fautif.

**Donnée historique perdue, sans impact** : les lignes déjà en base ont
`configuration_attribute_id = 0` (troncature MySQL silencieuse), une valeur non exploitable —
mais la relation réelle order_item → attribut reste disponible via
`configuration_attribute_option_id → configurable_attribute_options.configurable_attribute_id`
(colonne, elle, correctement typée `varchar(64)`). Aucun code Go ne relit jamais
`configuration_attribute_id` (vérifié par grep — uniquement écrite, jamais dans un `SELECT`), donc
aucune conséquence applicative au-delà de l'échec d'insertion lui-même.

## 2. Livrables

| Fichier | Contenu |
|---|---|
| [`migrations/082_fix_order_item_configuration_attribute_id_type.up.sql`](../../migrations/082_fix_order_item_configuration_attribute_id_type.up.sql) | `ALTER TABLE ... TYPE varchar(64) USING ...::varchar(64)` + commentaire de colonne |
| [`migrations/082_fix_order_item_configuration_attribute_id_type.down.sql`](../../migrations/082_fix_order_item_configuration_attribute_id_type.down.sql) | Rollback vers `integer`, avec requête de vérification pré-rollback (échouera si des lignes post-fix existent) |
| `internal/modules/orders/postgres_integration_test.go` | Seed `configuration_attribute_id` corrigé (`1` littéral → `'itest-attr-1'`, l'attribut réellement seedé juste avant) |
| `internal/modules/stocks/postgres_integration_test.go` | Seed `configuration_attribute_id` corrigé (`0` littéral → `attrID`, déjà seedé), commentaire de mismatch retiré (résolu) |

**Numéro `082`** : suite directe de la migration `081` (dernière présente dans `migrations/`).

**DDL Postgres uniquement**, conformément à la convention actée depuis la migration 069.

## 3. Choix de conception

**Cast `integer → varchar(64)` plutôt que l'inverse** : c'est `configurable_attributes.id` qui
porte la sémantique (ID préfixé applicatif généré par `helpers.GeneratePrefixedID`), pas
`order_item_configuration.configuration_attribute_id` — c'est cette dernière qui doit s'aligner,
comme documenté dans le rapport 15.

**`USING configuration_attribute_id::varchar(64)` explicite** : Postgres n'a pas de cast
implicite/assignment enregistré entre `integer` et `varchar` ; sans `USING`, l'`ALTER COLUMN TYPE`
échoue avec *"column ... cannot be cast automatically to type character varying"*.

**Aucune tentative de reconstruire la vraie valeur historique** (`0` → vrai
`configuration_attribute_id`) : c'est possible en théorie via
`configuration_attribute_option_id → configurable_attribute_options.configurable_attribute_id`,
mais hors périmètre de ce correctif (qui vise à débloquer l'écriture, pas à réparer la donnée
historique) — et sans bénéfice mesurable puisque la colonne n'est jamais relue.

**Pas de contrainte `NOT NULL` ni de FK ajoutée** : la colonne était déjà `NOT NULL` sans FK
(convention du dépôt — FK candidates commentées, jamais déclarées) ; ce correctif ne change que le
type, rien d'autre.

## 4. Statut d'exécution

| Environnement | Statut |
|---|---|
| Postgres dev (Docker `welloresto-postgres-dev`) | 🔴 non vérifiée — Docker Desktop indisponible dans cet environnement (`open //./pipe/dockerDesktopLinuxEngine: le fichier spécifié est introuvable`) |
| Postgres staging (Render, `welloresto_staging`) | ⏳ en attente de confirmation avant exécution (action sur base partagée) |
| **Production** | 🔴 **NON appliquée** — aucune URL de production dans l'environnement (seul `RENDER_STAGING_DATABASE_URL` est défini), même posture que les migrations 078/079 |

### Vérifications exécutées

| Test | Résultat |
|---|---|
| `go build ./...` | ✅ |
| `go vet ./internal/modules/order_life_cycle/... ./internal/modules/orders/... ./internal/modules/stocks/...` | ✅ |
| Application de l'`up`/`down` sur un Postgres réel | ⏳ non exécutée — voir ci-dessus |
| `go test -tags postgres_integration ./internal/modules/orders/... ./internal/modules/stocks/... ./internal/modules/order_life_cycle/...` | ⏳ non exécutée — nécessite le schéma déjà migré (staging ou dev) |

Le fichier reste dans `migrations/` tant qu'il n'a pas été appliqué en **production**, puis sera
déplacé vers `migrations/done/` par `git mv`.

## 5. Suite

- Faire confirmer l'application sur staging (accès disponible via `RENDER_STAGING_DATABASE_URL`),
  puis rejouer les tests d'intégration `postgres_integration` visés ci-dessus.
- Application en production : à faire manuellement (accès Render non disponible depuis cet
  environnement), en priorité vu l'incident en cours (`POST /orders/create` en échec pour toute
  commande avec configuration produit).
- Reconstruction éventuelle des lignes historiques `configuration_attribute_id = 0` via
  `configuration_attribute_option_id` : non traitée ici, à évaluer séparément si un besoin de
  lecture de cette colonne apparaît un jour.
