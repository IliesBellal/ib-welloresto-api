# 079 — Lien ingrédient sur les options d'attributs configurables

Le back-office ("Options & Suppléments", `wello-back-office/src/pages/Attributes.tsx`) permet de lier un
composant de stock ("ingrédient") + une quantité + une unité à chaque option d'un groupe d'attributs (ex.
option "Grande" de l'attribut "Taille Pizza" → composant "Pâte à pizza 300g"), et calcule un coût prévisionnel
affiché à l'écran. Le front envoyait déjà `component_id`, `quantity`, `unit_of_measure_id` dans le payload de
sauvegarde — mais côté API, rien ne persistait ces champs : ni colonne DB, ni champ de struct, ni SQL d'INSERT/
UPDATE/SELECT ne les touchait. Le lien saisi disparaissait donc au rechargement de la page.

**Ce ticket couvre uniquement la persistance** du lien option ↔ ingrédient (stockage + restitution via l'API).
Il ne branche **pas** ce lien sur la déduction de stock à la commande (`ConsumeOrderStock`,
`internal/modules/stocks/repository.go`) — chantier distinct, non traité ici.

## 1. Livrables

| Fichier | Contenu |
|---|---|
| [`migrations/079_configurable_attribute_options_ingredient_link.up.sql`](../../migrations/079_configurable_attribute_options_ingredient_link.up.sql) | `ALTER TABLE configurable_attribute_options ADD COLUMN` (3 colonnes) + commentaires |
| [`migrations/079_configurable_attribute_options_ingredient_link.down.sql`](../../migrations/079_configurable_attribute_options_ingredient_link.down.sql) | `DROP COLUMN` des 3 colonnes |
| [`04-schema-postgres-target.sql`](04-schema-postgres-target.sql) | Bloc `configurable_attribute_options` mis à jour (~ligne 782) |
| `internal/modules/menu/models.go` | `AttributeOption` (réponse) + `UpdateAttributeOptionPayload` (requête) : 3 nouveaux champs |
| `internal/modules/menu/repository.go` | `GetAttributes`, `GetAttribute`, `CreateAttribute`, `UpdateAttribute` : lecture/écriture des 3 colonnes |
| `internal/modules/menu/postgres_integration_test.go` | Extension du scénario existant : lien posé à la création, effacé à l'update, créé sur une nouvelle option |
| `internal/modules/menu/models_test.go` | Test unitaire verrouillant le comportement "clé JSON omise = zero-value" |

**Numéro `079`** : vérifié libre dans `migrations/` (max `078`) et dans `migrations/done/` (max `068`).

**DDL Postgres uniquement**, conformément à la convention actée depuis la migration 069 (la base réelle tourne
sur Postgres).

## 2. Schéma

Avant :
```
configurable_attribute_options: id, configurable_attribute_id, title, max_quantity, extra_price, image_url, enabled
```

Après (3 colonnes ajoutées, toutes NULL-ables) :
```
component_id      integer            -- ingrédient (components.component_id) lié. NULL = aucun.
quantity           double precision   -- quantité consommée par sélection. NULL si component_id est NULL.
unit_of_measure    integer            -- unit_of_measure.id de la quantité. NULL si component_id est NULL.
```

### Choix de conception

**NULL-able plutôt que valeurs par défaut** : la grande majorité des options n'ont pas d'ingrédient lié (ex.
"Sans glaçons", "Grande taille" sans surcoût matière). NULL est le seul état représentant correctement "aucun
ingrédient", contrairement à `requires.component_id` qui utilise `NOT NULL` + coercion `"0"` héritée de MySQL
(non pertinent ici, cette table est native Postgres).

**Nommage aligné sur la table sœur `requires`** (`component_id` + `quantity` + `unit_of_measure`, pas de
suffixe `_id`) côté colonnes DB, alors que le JSON expose `unit_of_measure_id` — même écart déjà présent entre
`components.unit_of_measure` (colonne) et `ComponentBasic.UnitOfMeasureID` (JSON) ailleurs dans ce module.

**Aucune FK créée**, conformément à la convention du dépôt (FK candidates commentées, jamais créées) :
- `component_id -> components.component_id`
- `unit_of_measure -> unit_of_measure.id`

**Pas d'index** : la table ne contient qu'une poignée de lignes par attribut, et rien n'interroge encore *par*
`component_id` (ce sens de lecture ne concernerait que le futur branchement stock, hors périmètre de ce ticket).

**Champs requête non-pointeurs** (`UpdateAttributeOptionPayload.ComponentID/Quantity/UnitOfMeasureID` sont
`string`/`float64`/`string`, pas `*string`/`*float64`) : `UpdateAttribute` traite déjà `payload.Options` comme
l'état complet et faisant autorité à chaque sauvegarde (désactive toutes les options existantes puis
réinsère/upsert chacune — ce n'est pas un patch partiel), exactement comme `Title`/`Price`/`MaxQuantity` sur ce
même payload. Un `*string` aurait réintroduit une ambiguïté "clé absente vs valeur vide" sans bénéfice. Côté
SQL, `UPDATE` utilise `component_id = ?` en clair (pas `COALESCE(?, component_id)` comme pour `image_url`) :
le front envoie ces 3 champs à chaque sauvegarde, un `COALESCE` aurait empêché d'effacer un lien déjà posé.

**Conséquence vérifiée** : le bouton "Aucun" du back-office (`Attributes.tsx:330-339`) envoie `undefined` pour
ces 3 champs, que `JSON.stringify` élimine de la requête HTTP. Comme les champs Go sont non-pointeurs, une clé
JSON absente et une clé envoyée explicitement vide décodent vers le même zero-value (`""`/`0`) — le flux
d'effacement fonctionne donc correctement sans aucune modification du front. Verrouillé par
`TestUpdateAttributeOptionPayloadUnmarshalOmittedComponentFieldsDecodeToZeroValue` (`models_test.go`).

## 3. Statut d'exécution

| Environnement | Statut |
|---|---|
| Postgres dev (Docker `welloresto-postgres-dev`, `localhost:5433`) | ✅ appliquée et vérifiée le 2026-08-05 |
| Postgres staging (Render, `welloresto_staging`) | ✅ appliquée et vérifiée le 2026-08-05 |
| **Production** | 🔴 **NON appliquée** — aucune URL de production présente dans l'environnement (seul `RENDER_STAGING_DATABASE_URL` est défini), même posture que la migration 078 |

### Vérifications exécutées (Postgres 16)

| Test | Résultat |
|---|---|
| Application de l'`up` (dev) | ✅ `ALTER TABLE` + 3 `COMMENT` |
| Rejeu de l'`up` (idempotence, dev) | ✅ `NOTICE ... already exists, skipping`, aucune erreur |
| `down` puis contrôle (dev) | ✅ `\d` confirme les 3 colonnes absentes |
| Re-`up` après `down` (dev) | ✅ |
| Application de l'`up` (staging) | ✅ `ALTER TABLE` + 3 `COMMENT` |
| `\d configurable_attribute_options` (staging) | ✅ 3 colonnes nullable confirmées |
| `go build ./...` | ✅ |
| `go test ./internal/modules/menu/...` (unitaires, dont le nouveau cas JSON) | ✅ |
| `go test -tags postgres_integration ./internal/modules/menu/...` (dev) | ✅ — couvre insert-avec-lien, insert-sans-lien, update-effacement-du-lien, update-nouvelle-option-avec-lien |

Le fichier reste dans `migrations/` tant qu'il n'a pas été appliqué en **production**, puis sera déplacé vers
`migrations/done/` par `git mv`.

Pour appliquer sur un autre environnement :

```bash
docker exec -i -e PGURL="<url>" welloresto-postgres-dev sh -c 'psql -v ON_ERROR_STOP=1 "$PGURL"' \
  < migrations/079_configurable_attribute_options_ingredient_link.up.sql
```

## 4. Constat annexe — drift du conteneur Postgres dev

En exécutant le test d'intégration étendu, `GetMenu` échouait avec `column pc.image_url does not exist` —
sans lien avec ce ticket : la migration [075](../../migrations/075_categories_image_url.up.sql)
(`productcateg.image_url` / `marketing_categories.image_url`), déjà mergée et appliquée sur staging, n'avait
jamais été rejouée sur le conteneur Postgres de dev local. Appliquée séparément pour débloquer la vérification
(`ALTER TABLE ... ADD COLUMN image_url`, résultat vérifié : `TestMenuRepository_Postgres` passe intégralement
après coup). Un vrai bug de test a été corrigé au passage : `t.Fatalf` déréférençait `menu.Status` même quand
`GetMenu` retournait `(nil, err)`, ce qui masquait l'erreur réelle derrière un panic nil-pointer — scindé en
deux `if` distincts (`err != nil` puis `menu.Status != "ok"`).

Aucun autre correctif de drift (069-078 restants) n'a été tenté : seule la colonne bloquant la vérification de
**ce** ticket a été rattrapée, le reste est hors périmètre.

## 5. Suite

Persistance en place. Le branchement sur la déduction de stock (`ConsumeOrderStock`) — pour que sélectionner
une option liée à un ingrédient déduise réellement le stock du composant — reste un chantier séparé, à traiter
sur demande explicite.
