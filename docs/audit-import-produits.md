# Audit — Feature « Importer des produits » (état des lieux, lecture seule)

Date : 2026-08-06 · Branche : `staging` · Périmètre : `ib-welloresto-api` + `wello-back-office`

Ce document est **factuel** : il décrit l'existant, sans proposer de solution ni de code.
Les points incertains sont explicitement marqués **[À CLARIFIER]**.

> Note préalable sur les sources SQL : `docs/migration-postgres/04-schema-postgres-target.sql`
> est le **schéma cible de la migration Postgres** et sert de baseline. Les migrations
> `migrations/062_*` → `079_*` s'appliquent **par-dessus** et ajoutent des colonnes qui
> n'apparaissent pas dans le fichier baseline (ex. `productcateg.image_url` via `075`).
> Les deux dialectes cohabitent (`internal/database/mysql.go` et `postgres.go`,
> abstraction `internal/database/dbx`).

---

## 1. API — `ib-welloresto-api`

### 1.1 Modèle Produit

**Fichiers**
| Rôle | Chemin |
|---|---|
| DTO création | [internal/modules/menu/models.go:212-250](internal/modules/menu/models.go#L212-L250) (`CreateProductPayload`) |
| DTO update | [internal/modules/menu/models.go:260-288](internal/modules/menu/models.go#L260-L288) (`ProductUpdatePayload`) |
| DTO réponse | [internal/models/menu_models.go:25-84](internal/models/menu_models.go#L25-L84) (`models.ProductEntry`) |
| SQL | [docs/migration-postgres/04-schema-postgres-target.sql:3001-3042](docs/migration-postgres/04-schema-postgres-target.sql#L3001-L3042) |

**Table `products`** (PK composite `(product_id, merchant_Id)`)

| Colonne | Type | Null | Default | Notes |
|---|---|---|---|---|
| `product_id` | integer IDENTITY | non | auto | **Pas d'ID préfixé** — entier auto-généré |
| `by_product_of` | integer | oui | — | produit parent (groupes) |
| `merchant_Id` | varchar(64) | non | — | identifiant mixed-case, replié `merchant_id` en PG |
| `name` | varchar(255) | non | — | |
| `product_desc` | text | oui | — | |
| `img` | text | oui | — | legacy base64 |
| `image_url` | text | oui | — | |
| `bg_color` | varchar(11) | non | `'#ffffff'` | |
| `production_color` | varchar(11) | oui | — | |
| `display_order` | integer | non | `0` | |
| `price` | integer | non | — | **centimes**, sur place |
| `price_take_away` | integer | non | `0` | centimes |
| `price_delivery` | integer | non | `0` | centimes |
| `price_uber_eats` | integer | non | `0` | centimes |
| `price_deliveroo` | integer | non | `0` | centimes |
| `available_in` | boolean | non | `true` | |
| `available_take_away` | boolean | non | `true` | |
| `available_delivery` | boolean | non | `true` | |
| `tva_in_id` | integer | non | `0` | → `tva_categories.tva_id` |
| `tva_delivery_id` | integer | non | `0` | → `tva_categories.tva_id` |
| `tva_take_away_id` | integer | non | `0` | → `tva_categories.tva_id` |
| `category` | varchar(30) | non | — | → `productcateg.merchant_categ_id` (**pas** `categ_id`) |
| `status` | varchar(20) | non | `'1'` | voir §1.1.c |
| `is_product_group` | boolean | non | `false` | |
| `is_available_on_sno` | boolean | non | `true` | Scan'N'Order |
| `is_available_on_kiosk` | boolean | non | `true` | Borne |
| `sync_deliveroo` | boolean | non | `true` | |
| `sync_uber_eats` | boolean | non | `true` | |
| `available` | boolean | non | `true` | « disponible sur la carte » |
| `enabled` | boolean | non | `true` | **soft delete** (`enabled = 0`) |
| `is_popular` | boolean | oui | `false` | |
| `creation_date` | timestamptz | non | `now()` | |

**Aucune FK déclarée** (convention du dépôt : « FK candidate (non creee) » dans les commentaires du schéma).

**a) Prix** — entiers en centimes, 5 colonnes. `CreateProduct` calcule `maxPrice = max(price, price_take_away, price_delivery)` et l'affecte à `price_uber_eats` / `price_deliveroo` sauf override explicite ([repository.go:2357-2383](internal/modules/menu/repository.go#L2357-L2383)).

**b) TVA** — 3 colonnes `integer` référençant `tva_categories.tva_id`. La table `tva_categories` ([04-schema…sql:3726-3737](docs/migration-postgres/04-schema-postgres-target.sql#L3726-L3737)) est **globale, non scopée merchant** :

| Colonne | Type | Notes |
|---|---|---|
| `tva_id` | integer IDENTITY | PK |
| `delivery_type` | varchar(20) | `0 => in, 1 => delivery, 3 => take away` (commentaire SQL) |
| `tva_title` | varchar(30) | |
| `tva_desc` | varchar(150) | |
| `tva_rate` | real | **en pourcentage** (5 ⇒ 5 %) |
| `show_in_report` | boolean | default `true` |
| `enabled` | boolean | default `true` |

Exposition : `GET /pos/tva_rates` → [internal/modules/pos/repository.go:289-355](internal/modules/pos/repository.go#L289-L355), qui joint `labels` (`label_type='order_type'`, `lang='FR'`) sur `tva_categories.delivery_type` et renvoie des groupes `ConsumptionType{ id, name, delivery_type, rates[] }`.

**c) Statut produit — pas d'enum Go.** Aucune constante, aucun type. Les valeurs sont des chaînes littérales éparpillées :

| Valeur | Où | Sens |
|---|---|---|
| `'1'` | default colonne SQL | actif (legacy) |
| `'0'` | [orders/repository.go:396-400](internal/modules/orders/repository.go#L396-L400) | inactif (legacy) |
| `'available'` | back-office [ProductsTable.tsx:42](../../wello-back-office/src/components/menu/ProductsTable.tsx) | Disponible |
| `'out_of_stock'` | [mapper_ubereats.go:166](internal/modules/menu/mapper_ubereats.go#L166), [mapper_deliveroo.go:113](internal/modules/menu/mapper_deliveroo.go#L113), `orders`, `order_life_cycle` | Rupture de stock — **bloque la commande** |
| `'not_available'` | [service.go:513](internal/modules/menu/service.go#L513) (écrit par le toggle POS) | Indisponible |
| **`'removed_from_menu'`** | [repository.go:950](internal/modules/menu/repository.go#L950), [repository.go:1033](internal/modules/menu/repository.go#L1033) — filtre `p.status NOT IN ('removed_from_menu')` | **« Retiré de la carte »** |
| `'unavailable_today'` | uniquement [postgres_integration_test.go:629](internal/modules/menu/postgres_integration_test.go#L629) | non géré ailleurs |

➡️ **La valeur exacte « retiré de la carte » est `removed_from_menu`.** Elle n'est définie nulle part comme constante : elle vit dans 2 clauses SQL côté API et dans 3 `SelectItem` côté back-office ([SimpleProductSheet.tsx:1047,1742](../../wello-back-office/src/components/menu/SimpleProductSheet.tsx), [ProductsTable.tsx:45](../../wello-back-office/src/components/menu/ProductsTable.tsx)). `SetProductStatus` ([repository.go:2819](internal/modules/menu/repository.go#L2819)) écrit la chaîne **sans validation**.

**d) Colonnes d'import externe : AUCUNE.** Ni `provider`, ni `external_id`, ni `sku` sur `products`, `productcateg`, `tags`, `configurable_attributes`. Le seul précédent de liaison externe est un jeu de **tables de mapping dédiées** par plateforme :

- `integration_uber_eats_products_mapping` (`item_id` varchar(50) ↔ `product_id` varchar(50), `merchant_id`, `creation_date`, `deletion_date`, `enabled`)
- `integration_uber_eats_attributes_mapping` (UNIQUE `(merchant_id, modifier_group_id)`)
- `integration_uber_eats_options_mapping` (UNIQUE `(merchant_id, item_id)`)
- `integration_uber_eats_components_mapping`
- idem `integration_deliveroo_*_mapping` ([04-schema…sql:1666-1733](docs/migration-postgres/04-schema-postgres-target.sql#L1666-L1733))

---

### 1.2 Multi-canal

WelloResto distingue **5 canaux**, avec un niveau de support inégal :

| Canal | Prix dédié | TVA dédiée | Activation par produit |
|---|---|---|---|
| Sur place (`in`) | `price` | `tva_in_id` | `available_in` |
| À emporter (`take_away`) | `price_take_away` | `tva_take_away_id` | `available_take_away` |
| Livraison (`delivery`) | `price_delivery` | `tva_delivery_id` | `available_delivery` |
| Uber Eats | `price_uber_eats` | ❌ (aucune) | `sync_uber_eats` |
| Deliveroo | `price_deliveroo` | ❌ (aucune) | `sync_deliveroo` |
| Scan'N'Order | ❌ | ❌ | `is_available_on_sno` |
| Borne / Kiosk | ❌ | ❌ | `is_available_on_kiosk` |

**Tout est en colonnes plates sur `products`** — pas de table dédiée, pas de JSON. Le seul objet structuré est côté DTO : `models.ProductIntegrations{ UberEats, Deliveroo }` avec `{ enabled, price_override }` ([internal/models/menu_models.go:86-94](internal/models/menu_models.go#L86-L94)), aplati vers les colonnes `sync_*` / `price_*` par `SyncProductIntegrations` ([repository.go:3692](internal/modules/menu/repository.go#L3692)).

Désactivation d'un canal pour un produit donné : `available_in` / `available_take_away` / `available_delivery` (booléens). Il n'existe **pas** de mécanisme pour désactiver la TVA d'un canal : les 3 `tva_*_id` sont `NOT NULL` et exigés à la création.

Deux axes globaux se superposent aux canaux : `available` (bool, « sur la carte ») et `status` (varchar, voir §1.1.c) — pilotés par deux endpoints distincts (`PATCH .../availability` et `PATCH .../status`). **[À CLARIFIER]** la sémantique exacte de `available` vs `status='removed_from_menu'` n'est documentée nulle part.

---

### 1.3 Modèle Catégorie

**Table `productcateg`** ([04-schema…sql:2984-2994](docs/migration-postgres/04-schema-postgres-target.sql#L2984-L2994) + migration `075`)

| Colonne | Type | Null | Default |
|---|---|---|---|
| `categ_id` | integer IDENTITY | non | auto (PK) |
| `merchant_id` | varchar(64) | non | — |
| `merchant_categ_id` | varchar(20) | non | — (**l'ID métier exposé**) |
| `categ_name` | text | non | — |
| `categ_order` | integer | non | — |
| `bg_color` | varchar(9) | non | `'#ffffff'` |
| `available` | boolean | non | `true` |
| `enabled` | boolean | non | `true` (soft delete) |
| `image_url` | varchar(512) | oui | — (ajoutée par `migrations/075`) |

**Particularité importante** : `merchant_categ_id` est renseigné en **deux temps** par `CreateProductCategory` ([repository.go:3476-3542](internal/modules/menu/repository.go#L3476-L3542)) — `INSERT` avec `''`, puis `UPDATE merchant_categ_id = categ_id`. Les deux requêtes ne sont **pas dans une transaction explicite**. `categ_order` = `MAX(categ_order)+1` sur le merchant. Le nom est passé par `capitalizeFirst`.

**Relation produit ↔ catégorie : 1-N**, portée par `products.category` (varchar) = `productcateg.merchant_categ_id` (varchar). Un produit a **exactement une** catégorie caisse. Validation à la création : la catégorie doit exister et être `enabled` ([repository.go:2285-2296](internal/modules/menu/repository.go#L2285-L2296)).

**Unicité du nom : AUCUNE contrainte** (ni index unique SQL, ni vérification applicative). Deux catégories homonymes sur le même merchant sont acceptées.

**Second axe : catégories marketing** (`marketing_categories` + `product_marketing_categories`), utilisé par Scan'N'Order / borne :
- `marketing_categories` a un **UNIQUE `(merchant_id, name)`** ([04-schema…sql:2053](docs/migration-postgres/04-schema-postgres-target.sql#L2053))
- `product_marketing_categories` a pour **PK `product_id` seul** ⇒ **1 catégorie marketing max par produit**
- Renseigné hors transaction, en best-effort, par `MenuService.CreateProduct` ([service.go:293-296](internal/modules/menu/service.go#L293-L296)) — un échec n'annule pas la création du produit

---

### 1.4 Modèle Tag

**L'entité Tag existe.** Module dédié : `internal/modules/tags/` (handler / service / repository / models).

**Table `tags`** ([04-schema…sql:3621-3628](docs/migration-postgres/04-schema-postgres-target.sql#L3621-L3628))

| Colonne | Type | Null | Default |
|---|---|---|---|
| `tag_id` | varchar(42) | non | — (PK) — ID préfixé `tag-<uuid>` (40 car.) |
| `merchant_id` | varchar(64) | non | — |
| `name` | varchar(50) | non | — |
| `color` | varchar(9) | non | `'#ffffff'` |
| `display_order` | integer | non | `0` |

**Table de jointure `product_tags`** — M-N, PK `(product_id, tag_id)`, colonnes `product_id varchar(255)`, `tag_id varchar(255)`. **Pas de colonne `enabled`** : suppression physique.

**Absences notables :**
- **Pas d'index unique `(merchant_id, name)`** → doublons de nom possibles. Le code appelle bien `dbx.IsDuplicateEntry` ([tags/repository.go:91](internal/modules/tags/repository.go#L91)) mais rien ne peut le déclencher.
- **Pas de soft delete** : `DeleteTag` fait un `DELETE` physique ([tags/repository.go:127](internal/modules/tags/repository.go#L127)).
- Le commentaire « *Also cascades to product_tags due to FK constraint* » ([tags/repository.go:108](internal/modules/tags/repository.go#L108)) décrit une FK **qui n'existe pas dans le schéma cible** (`product_tags` n'a aucune FK déclarée). **[À CLARIFIER]** — la FK existe-t-elle réellement en base MySQL de prod ? Sinon, `DeleteTag` laisse des lignes orphelines dans `product_tags`.

Génération d'ID : `helpers.GeneratePrefixedID(helpers.TagIDPrefix)`, dans le **service** ([tags/service.go:41](internal/modules/tags/service.go#L41)), pas dans le repository.

---

### 1.5 Système de modificateurs (Attribut / Option)

#### a) `configurable_attributes` ([04-schema…sql:764-779](docs/migration-postgres/04-schema-postgres-target.sql#L764-L779))

| Colonne | Type | Null | Default | Notes |
|---|---|---|---|---|
| `id` | varchar(64) | non | — | PK, ID préfixé `attribute-<uuid>` (46 car.) |
| `product_id` | integer | **non** | — | **Colonne héritée, jamais utilisée** : `CreateAttribute` insère `0` en dur ([repository.go:464-478](internal/modules/menu/repository.go#L464-L478)) |
| `merchant_id` | varchar(64) | non | — | |
| `brand` | varchar(20) | non | `'WELLO_RESTO'` | origine : `WELLO_RESTO` \| `UBER_EATS` |
| `attribute_type` | varchar(20) | non | `'CHECK'` | commentaire SQL : `CHECK \| QUANTITY` |
| `name` | varchar(50) | non | — | nom interne |
| `title` | varchar(80) | non | — | libellé affiché |
| `max_options` | integer | **non** | **aucun défaut** | |
| `is_required` | boolean | non | `true` | **jamais écrit par le module menu** — uniquement par les webhooks Deliveroo/UberEats |
| `min_options` | integer | non | `0` | |
| `enabled` | boolean | non | `true` | soft delete |

Le DTO Go est `menu.Attribute` / `menu.UpdateAttributePayload` ([models.go:189-198, 365-372](internal/modules/menu/models.go#L189-L198)) — champs `type`, `name`, `title`, `min`, `max`, `options[]`, `product_count`.

**Divergence de vocabulaire sur `attribute_type`** : le commentaire SQL dit `CHECK | QUANTITY`, le commentaire Go dit `"CHECK", "RADIO"` ([models.go:366](internal/modules/menu/models.go#L366)), et le back-office envoie **toujours `'CHECK'`** en dur ([Attributes.tsx:77](../../wello-back-office/src/pages/Attributes.tsx)), la distinction Radio/Checkbox étant **dérivée** de `min === max === 1` ([Attributes.tsx:63](../../wello-back-office/src/pages/Attributes.tsx)). **[À CLARIFIER]** quelle est la liste de valeurs faisant autorité.

#### b) `configurable_attribute_options` ([04-schema…sql:787-803](docs/migration-postgres/04-schema-postgres-target.sql#L787-L803) + migration `079`)

| Colonne | Type | Null | Default | Notes |
|---|---|---|---|---|
| `id` | integer IDENTITY | non | auto | **Pas d'ID préfixé** (`AttributeOptionIDPrefix` existe dans `helpers/ids.go:19` mais **n'est plus utilisé** — cf. commentaire [repository.go:521-525](internal/modules/menu/repository.go#L521-L525)) |
| `configurable_attribute_id` | varchar(64) | non | — | → `configurable_attributes.id` (index simple) |
| `title` | **varchar(25)** | non | — | ⚠️ **très court** |
| `max_quantity` | integer | non | `1` | |
| `extra_price` | integer | non | `0` | **centimes** — c'est le « supplément » |
| `image_url` | varchar(500) | oui | — | |
| `enabled` | **integer** | non | `1` | reste un 0/1 entier (pas boolean) |
| `component_id` | integer | oui | — | migration `079` — lien ingrédient |
| `quantity` | double precision | oui | — | migration `079` |
| `unit_of_measure` | integer | oui | — | migration `079` |

**Relation attribut ↔ option : 1-N** via `configurable_attribute_options.configurable_attribute_id`. Pas de colonne d'ordre explicite (ordre = ordre d'insertion / `id`).

#### c) La matrice : `product_configurable_attribute` ([04-schema…sql:3061-3067](docs/migration-postgres/04-schema-postgres-target.sql#L3061-L3067))

| Colonne | Type | Null | Default |
|---|---|---|---|
| `product_id` | varchar(64) | non | — |
| `configurable_attribute_id` | varchar(64) | non | — |
| `num_order` | integer | non | `0` |
| `enabled` | boolean | non | `true` |

PK `(configurable_attribute_id, product_id)`. **Aucun champ de configuration par produit** : pas d'override de prix, pas de `is_required` par produit, pas de min/max par produit. La config est portée **entièrement** par l'attribut lui-même.

Deux chemins d'écriture coexistent :
- `SyncProductAttributes` ([repository.go:3760](internal/modules/menu/repository.go#L3760)) — `DELETE` total puis `INSERT` (utilisé à la création de produit)
- `UpdateProductAttributes` ([repository.go:4845](internal/modules/menu/repository.go#L4845)) — `UPDATE enabled=FALSE` puis upsert (`ON DUPLICATE KEY` / `ON CONFLICT`), utilisé par `PATCH /menu/products/{id}/attributes` — c'est **celui que la matrice back-office appelle**

---

### 1.6 Service de création produit

**Chaîne complète**

```
POST /menu/products                          cmd/api/routes.go:711
  → MenuHandler.CreateProduct                internal/modules/menu/handler.go:405
  → MenuService.CreateProduct                internal/modules/menu/service.go:281
  → MenuRepository.CreateProduct             internal/modules/menu/repository.go:2260
```

**Signatures**

```go
func (s *MenuService) CreateProduct(ctx, token string, req *CreateProductPayload) (*models.ProductEntry, error)
func (r *MenuRepository) CreateProduct(ctx, p *CreateProductPayload) (string, error)  // retourne le product_id
```

**Handler** — aucune validation, décodage JSON brut puis délégation.

**Service** — injecte `req.MerchantID` depuis `middleware.UserFromContext(ctx).MerchantID`, appelle le repo, puis :
1. `AssignProductMarketingCategory` **en best-effort hors transaction** (erreur ignorée)
2. `invalidateMenuCache(ctx, merchantID)`
3. re-`GetProduct` pour renvoyer l'entité complète

**Repository — 5 validations séquentielles** ([repository.go:2263-2355](internal/modules/menu/repository.go#L2263-L2355)) :
1. `tva_in_id` / `tva_delivery_id` / `tva_take_away_id` non vides **et numériques** (`menuNumericID`)
2. La catégorie existe (`productcateg.merchant_categ_id` + `merchant_id` + `enabled = TRUE`)
3. `COUNT(*) = 3` sur `tva_categories` où `enabled` et `tva_id IN (…)`
4. Vérification individuelle des 3 taux
5. **Unicité du nom produit** : `LOWER(name)` + `enabled = TRUE` sur le merchant

⚠️ **La validation 5 n'est pas un simple refus** — c'est un mécanisme de **double-appel avec confirmation Redis** :
- 1er appel avec un nom déjà pris → pose `SetNX(GetMenuProductNameConfirmKey(merchant, name), "1", 5 min)` et retourne `ErrProductNameAlreadyExistsWithRetry`
- 2e appel **identique** → la clé existe, elle est supprimée et la création est **acceptée malgré le doublon**
- Sans Redis (`r.redis == nil`) → refus sec `ErrProductNameAlreadyExists`

Le même mécanisme existe pour les attributs (`GetMenuAttributeNameConfirmKey`, [repository.go:446-458](internal/modules/menu/repository.go#L446-L458)) et les composants.

**Transaction** — `dbutils.RunInTx` ([internal/utils/dbutils/run_in_tx.go](internal/utils/dbutils/run_in_tx.go)), **réentrant** (si une transaction est déjà dans le contexte, il exécute simplement la closure). Dans la transaction :
1. `INSERT INTO products` (colonnes optionnelles ajoutées dynamiquement pour préserver les defaults) → `InsertReturningID`
2. `SyncProductAttributes` si `Configuration` non vide
3. `SyncProductComponents` si `Components` non vide
4. `SyncProductTags` si `Tags` non vide
5. `SyncProductAllergens` si `Allergens` non vide
6. `setMenuUpdated(merchantID)`

Les intégrations (`sync_*`, `price_uber_eats`, `price_deliveroo`) sont écrites **directement dans l'INSERT**, pas via `SyncProductIntegrations`.

**Batch : il n'existe aucune création en lot de produits.** Seule création alternative : `CreateExternalProductTx` ([repository.go:2508](internal/modules/menu/repository.go#L2508)) — insert minimal avec `category = 'UBER_EATS_TEMP'` et TVA en dur `(5, 9, 3)`, utilisé par l'intégration Uber Eats.

Les opérations en lot existantes portent uniquement sur des **rattachements** ou des **prix**, jamais sur la création :
`BulkAssignProductsToCategory`, `BulkAssignProductsToTag`, `BulkAssignAllergen`, `BulkAssignProductsToMarketingCategory`, `BulkUpdateProductPrices`.

---

### 1.7 Infrastructure réutilisable

#### a) Génération d'ID — [internal/helpers/ids.go](internal/helpers/ids.go)

```go
func GeneratePrefixedID(prefix string) string { return prefix + "-" + uuid.New().String() }
```

| Entité | Préfixe | Constante | Usage réel |
|---|---|---|---|
| Produit | — | *aucune* | ❌ `product_id` = integer IDENTITY |
| Catégorie caisse | — | *aucune* | ❌ `categ_id` IDENTITY, `merchant_categ_id` = copie textuelle |
| Catégorie marketing | — | *aucune* | ❌ **[À CLARIFIER]** `marketing_categories.category_id` — vérifier son type |
| Tag | `tag` | `TagIDPrefix` (ids.go:17) | ✅ `tags.tag_id varchar(42)` |
| Attribut | `attribute` | `AttributeIDPrefix` (ids.go:18) | ✅ `configurable_attributes.id varchar(64)` |
| Option d'attribut | `attribute-option` | `AttributeOptionIDPrefix` (ids.go:19) | ❌ **constante déclarée mais plus utilisée** — l'ID est un IDENTITY |

#### b) Redis — [internal/infrastructure/redis/](internal/infrastructure/redis/), [internal/helpers/redis_helpers.go](internal/helpers/redis_helpers.go), [internal/models/redis_models.go](internal/models/redis_models.go)

API du client : `Get`, `Set(key, value, ttl)`, `SetNX(key, value, ttl)`, `Delete`, `InvalidateMerchantMenuCaches(merchantID)`, `ScanDeleteByPattern(pattern)`.

Constructeurs de clés existants (tous dans `redis_helpers.go`) : `GetRedisOrderKey`, `GetWebhookUberEventKey`, `GetWebhookDeliverooEventKey`, `GetWebhookDeliverooLocationKey`, `GetMFACacheKey`, `GetVerificationCacheKey`, `GetMenuProductNameConfirmKey`, `GetMenuComponentNameConfirmKey`, `GetMenuAttributeNameConfirmKey`.

TTL déclarés (`redis_models.go`) : `UserCacheTTL 60m`, `ScannorderKioskMerchantMenuTTL 10m`, `ScannorderMerchantTTL 2m`, `OrdersCacheTTL 5m`, `WebhookdeliveroolocationTTL 24h`, `WebhookUberEatsEventTTL 3h`, `PINLockoutTTL 1h`, `OTPCacheTTL 5m`, `VerificationCacheTTL 15m`, `PasswordResetIPThrottleTTL 1h`, `MenuNameConfirmTTL 5m`.

➡️ Les primitives pour stocker un résultat de preview sous token temporaire existent (`Set` + TTL), **mais aucun helper de « job » / « session de travail » n'existe**.

#### c) R2 / object storage — [internal/infrastructure/r2/](internal/infrastructure/r2/)

```go
func (c *Client) UploadFile(ctx, key string, file io.Reader, contentType string) (string, error)  // → URL publique
func (c *Client) UploadPrivateFile(ctx, key string, file io.Reader, contentType string) (string, error)
func (c *Client) GenerateSignedURL(ctx, key string, ttl time.Duration) (string, error)
func (c *Client) DeleteFile(ctx, key string) error
func (c *Client) GetKeyFromURL(url string) string
func (c *Client) PublicURL(key string) string
```

Générateurs de clé : `GenerateProductKey`, `GenerateProductCategoryKey`, `GenerateMarketingCategoryKey`, `GenerateConfigOptionKey`, `GenerateScanNOrderKey`, `GenerateKioskKey`, `GenerateUserAvatarKey`, `GenerateHACCPTraceabilityKey`.
Validation : `ValidateImageType` (JPEG/PNG/WebP), `ValidateVideoType`, `GetExtensionFromContentType`, `GetContentTypeFromExtension`.

➡️ **Aucun générateur de clé générique « document »/« fichier importé »**, et aucun helper pour un fichier non-image.

#### d) Upload multipart

**Pas de middleware partagé.** Le pattern est dupliqué handler par handler :

| Chemin | Champ form | Limite |
|---|---|---|
| [menu/handler.go:1320](internal/modules/menu/handler.go#L1320) (catégorie produit) | `photo` | `5 << 20` |
| [menu/handler.go:1447](internal/modules/menu/handler.go#L1447) (catégorie marketing) | `photo` | `5 << 20` |
| [menu/handler.go:1576](internal/modules/menu/handler.go#L1576) (option d'attribut) | `photo` | `5 << 20` |
| [integrations/handler.go:96](internal/modules/integrations/handler.go#L96) | `photo` | `maxImageSize` |
| [haccp/handler.go:299](internal/modules/haccp/handler.go#L299) | `file` | `maxSize` |
| [kiosk/admin_handler.go:380,473](internal/modules/kiosk/admin_handler.go#L380) | `file` | var. |

Séquence type : `ParseMultipartForm` → `FormFile` → détection content-type → `r2.Validate*Type` → `Generate*Key` → `UploadFile` → persistance URL.

#### e) Scoping merchant

Uniforme dans tout le dépôt :
- Middleware d'auth (valide le token contre Redis) injecte `*auth.UserLoginRow` dans le contexte
- Couche **service** : `middleware.UserFromContext(ctx)` → `user.MerchantID` ([internal/middleware/auth.go:133](internal/middleware/auth.go#L133))
- Couche **repository** : reçoit `merchantID string` en paramètre explicite et l'ajoute au `WHERE`
- Les `Sync*` vérifient systématiquement l'ownership (`SELECT COUNT(1) FROM products WHERE product_id = ? AND merchant_id = ?`)

**Observation permissions** : le bloc `r.Route("/menu", …)` ([cmd/api/routes.go:684-768](cmd/api/routes.go#L684-L768)) n'applique que `authMiddleware`. Aucun `middleware.RequirePermission(middleware.HasMenuAccess)` — alors que `HasMenuAccess` existe ([internal/middleware/permissions.go:44](internal/middleware/permissions.go#L44)) et que d'autres modules (HACCP, planning, users, kiosk) l'utilisent.

---

### 1.8 Import existant

**Aucun mécanisme d'import de fichier dans l'API.** Vérifications effectuées :
- `go.mod` ne contient **aucune bibliothèque Excel/xlsx**. Seuls formats manipulés : `encoding/csv` (**export** TVA uniquement, [pos/accounting/service.go:406](internal/modules/pos/accounting/service.go#L406)) et `gofpdf` (affiche allergènes).
- Aucun endpoint `/import`, aucun handler d'import.

**Précédents les plus proches (ingestion de catalogue externe → entités Wello) :**

| Quoi | Où |
|---|---|
| Mapping menu Wello → payload Uber Eats | [internal/modules/menu/mapper_ubereats.go](internal/modules/menu/mapper_ubereats.go) (255 l.) |
| Mapping menu Wello → payload Deliveroo | [internal/modules/menu/mapper_deliveroo.go](internal/modules/menu/mapper_deliveroo.go) (200 l.) |
| Webhook résultat d'upload menu Deliveroo | [internal/webhook/deliveroo_menu/](internal/webhook/deliveroo_menu/) |
| Création produit depuis source externe | `MenuService.CreateProductFromExternal` ([service.go:802](internal/modules/menu/service.go#L802)) → `CreateExternalProductTx` |
| Persistance ID externe ↔ ID Wello | `internal/webhook/ubereats/repository/attribute_mapping_repo.go` + tables `integration_*_mapping` |

⚠️ Ces mappers vont **dans le sens sortant** (Wello → plateforme). Il n'existe pas de mapper entrant fichier → Wello.

---

## 2. Back-office — `wello-back-office`

### 2.1 Gestion produits

| Composant | Chemin | Lignes | Rôle |
|---|---|---|---|
| `ProductCreateSheet` | `src/components/menu/ProductCreateSheet.tsx` | 676 | Création (formulaire zod + react-hook-form) |
| `SimpleProductSheet` | `src/components/menu/SimpleProductSheet.tsx` | 2218 | Fiche produit complète (édition, onglets) |
| `GroupProductSheet` | `src/components/menu/GroupProductSheet.tsx` | 255 | Produits groupés |
| `ProductEditModal` | `src/components/menu/ProductEditModal.tsx` | 437 | |
| `ProductDetailsSheet` | `src/components/menu/ProductDetailsSheet.tsx` | 673 | |
| `ProductsTable` | `src/components/menu/ProductsTable.tsx` | 382 | Tableau + badges de statut |
| Onglets | `ProductCompositionTab.tsx`, `ProductOptionsTab.tsx`, `ProductTagsTab.tsx` | | |

**Schéma de création** (`ProductCreateSheet.tsx:48-54`) — champs requis :
`name`, `price`, `price_take_away`, `price_delivery` (saisis **en euros**, convertis en centimes par `Math.round(x * 100)` ligne 97-99), `category_id`, `tva_on_site`, `tva_takeaway`, `tva_delivery`.

Payload envoyé (`ProductCreateSheet.tsx:96-107`) : ajoute `tva_in_id` / `tva_take_away_id` / `tva_delivery_id` et force `available_in`/`available_take_away`/`available_delivery` à `true`.

Type TS : `ProductCreatePayload` ([src/types/menu.ts:290-317](../../wello-back-office/src/types/menu.ts)) — miroir du `CreateProductPayload` Go, y compris les champs optionnels `configuration[]`, `components[]`, `tags[]`, `allergens[]`, `integrations`, `status`, `bg_color`, `production_color`, `is_available_on_sno`.

TVA : `menuService.getTvaRates()` → `GET /pos/tva_rates` → `TvaRateGroup[] { id, name, delivery_type, rates[] }`.

### 2.2 Gestion catégories & tags

| Page / composant | Chemin | Lignes |
|---|---|---|
| Catégories caisse | `src/pages/CategoriesTable.tsx` | 627 |
| Catégories marketing | `src/pages/MarketCategoriesTable.tsx` | — |
| Catégories d'ingrédients | `src/pages/ComponentCategoriesTable.tsx` | — |
| Tags | `src/pages/TagsTable.tsx` | 549 |
| Sheet de gestion catégorie | `src/components/menu/CategoryManagementSheet.tsx` | 303 |
| Sheet tags | `src/components/menu/TagsSheet.tsx` | 195 |

Endpoints tags consommés : `getTags`, `createTag(name, color)`, `updateTag(id, name, color, displayOrder)`, `updateTagOrder`, `deleteTag`, `bulkAssignProductsToTag`.

### 2.3 La matrice attribut ↔ produit

**[src/components/menu/AttributesMatrixDialog.tsx](../../wello-back-office/src/components/menu/AttributesMatrixDialog.tsx)** (345 lignes) — c'est la pièce réutilisée pour le rattachement post-import.

Fonctionnement :
- Props : `products[]`, `attributes[]`, `categories[]`, `onProductsUpdated(updates)`
- État interne : `Map<productId, Set<attributeId>>`, snapshot figé **à l'ouverture** (`useEffect` sur `[open]`) pour ne pas écraser les modifications en cours
- Lecture des attributs déjà rattachés : `getAttributeIds(product)` gère **3 formes** de données (`configuration.attributes[]` objets, `configuration[]` IDs, `product.attributes[]`) — signe d'une normalisation incomplète côté API
- Tableau croisé : lignes = produits (triés par rang de catégorie puis `display_order`), colonnes = attributs, en-têtes pivotés à −45°, sticky
- Sauvegarde : `menuService.updateProductAttributes(productId, attributeIds)` → **`PATCH /menu/products/{product_id}/attributes`**, **une requête par produit modifié**, pool de 4 workers concurrents (`SAVE_CONCURRENCY = 4`)
- **Aucune atomicité** : les lignes en échec restent `dirty` et affichent une erreur par ligne ; toast « Enregistrement partiel »
- Produits exclus : `is_product_group` / `is_group`

Composants sœurs, même patron : `AllergensMatrixDialog.tsx` (274 l.), `TagsMatrixDialog.tsx` (233 l.), `BulkAssignTagsDialog.tsx` (266 l.).

### 2.4 Patterns réutilisables pour un wizard d'import

| Besoin | Existant ? | Détail |
|---|---|---|
| Stepper / wizard | ❌ **aucun** | Aucun composant `Stepper`/`Wizard` dans `src/` |
| Modale riche réutilisable | ✅ | Les 3 `*MatrixDialog` + primitive `src/components/ui/dialog.tsx` ; aussi `sheet.tsx`, `responsive-sheet.tsx`, `drawer.tsx` |
| Composant d'upload de fichier | ❌ **aucun** | 3 `<input type="file">` bruts : `SimpleProductSheet.tsx`, `settings/ProfileTab.tsx`, `team/tabs/DocumentsTab.tsx`. Pas de dropzone, pas de composant partagé |
| Barre de progression | ✅ primitive | `src/components/ui/progress.tsx` |
| Table | ✅ | `src/components/ui/table.tsx` |
| Client API | ✅ | `src/services/apiClient.ts` — enveloppe `WelloApiResponse<T> { id, data }`, gestion MFA, compteur de loading global, masquage des champs sensibles dans les logs, `USE_MOCK_DATA` / `withMock()` |
| react-query | ⚠️ **partiel** | `src/lib/queryKeys.ts` (`qk`) couvre users, planning, printers, kiosks, reservations. **`menu` n'y figure pas.** `useMenuData.ts` est en `useState` + `useEffect` + `Promise.all` manuel, sans cache ni `invalidateQueries` |
| Gestion du doublon de nom | ✅ | `src/hooks/useDuplicateNameConfirm.ts` — rejoue **la requête à l'identique** après confirmation de l'utilisateur ; reconnaît `product_name_already_exists_with_retry`, `component_…`, `attribute_…` |
| Toasts | ✅ | `use-toast.ts` (shadcn) + `sonner` — les deux coexistent |

### 2.5 Navigation — l'entrée existe déjà

**[src/pages/Menu.tsx:244-254](../../wello-back-office/src/pages/Menu.tsx)** — le point d'entrée est **déjà présent en placeholder**, dans le dropdown accolé au bouton « Nouveau Produit » :

```tsx
<DropdownMenuItem onClick={() => toast.info('Création multiple - à implémenter')}>
  <CopyPlus className="w-4 h-4 mr-2" /> Créer plusieurs produits
</DropdownMenuItem>
<DropdownMenuItem onClick={() => toast.info('Import de produits - à implémenter')}>
  <Upload className="w-4 h-4 mr-2" /> Importer des produits
</DropdownMenuItem>
```

Emplacements alternatifs : deux configurations de navigation coexistent (⚠️ à maintenir en parallèle) —
- `src/config/navConfig.ts` (`navConfig`, groupe `menu`, clé `title`/`href`) — consommé par `NavMenuContent.tsx`, `BottomNav.tsx`
- `src/config/navigationConfig.ts` (`NavigationItem`, clé `label`/`path`) — consommé par `SidebarItem.tsx`, `NavigationItems.tsx`, `CollapsibleItem.tsx`, `useSidebar.ts`

Sous-entrées actuelles du groupe Menu : Produits, Catégories caisse, Tags, Ingrédients, Catégories d'ingrédients *(navConfig seul)*, Grille de prix, Options & Suppléments, Promotions & Disponibilités.

---

## 3. Écarts & risques

### 3.1 Entité Tag — ✅ existe, mais fragile

L'entité existe (table `tags`, module `internal/modules/tags`, M-N via `product_tags`, UI `TagsTable.tsx`). Le mapping « Zelty Tag (suivants) → Tag Wello » est donc **direct**. Écarts :

- **Aucune contrainte d'unicité `(merchant_id, name)`** → un import répété créera des tags homonymes en silence. La déduplication devra être **entièrement applicative** (et le `dbx.IsDuplicateEntry` du repo est du code mort).
- Pas de soft delete (`DELETE` physique) ; **FK cascade documentée mais absente du schéma cible** → **[À CLARIFIER]** vérifier l'état réel en base.
- `tags.name` est `varchar(50)` — plus court que `products.name` (255).

### 3.2 Statut produit — ✅ valeur identifiée, ❌ pas d'enum

- **`removed_from_menu`** est bien la valeur « retiré de la carte ».
- **Aucune constante Go, aucune contrainte SQL, aucune validation** : `status` est un `varchar(20)` avec default `'1'`, et `SetProductStatus` écrit ce que le client envoie.
- **Cinq vocabulaires cohabitent** : legacy numérique (`'0'`/`'1'`), textuel POS (`not_available`), textuel back-office (`available`, `out_of_stock`, `removed_from_menu`), et `unavailable_today` (tests uniquement). `mapWelloStatusToAvailability` ([service.go:504](internal/modules/menu/service.go#L504)) ne connaît **pas** `removed_from_menu` → un produit passé à ce statut **ne déclenche aucune synchro Uber Eats / Deliveroo**.
- **[À CLARIFIER]** quel statut l'import doit poser sur les produits créés : le default colonne (`'1'`) ? `'available'` ? Le mapping Zelty ne dit rien à ce sujet, et le produit doit-il être créé actif ou en `removed_from_menu` en attendant validation ?

### 3.3 Prix / TVA — ✅ multi-canal, mais indirect

Le schéma est **bien multi-canal** sur les 3 canaux visés (sur place / emporté / livraison), avec prix **et** TVA par canal. Écarts sur le mapping Zelty :

- La TVA est stockée en **`tva_id` (entier, FK logique)**, pas en taux. Un fichier Zelty qui fournit des **taux** (5,5 %, 10 %, 20 %) devra être **résolu** en `tva_id` via `tva_categories`, en respectant le `delivery_type` (`0`=in, `1`=delivery, `3`=take away). La table est **globale** (pas de `tva_categories.merchant_id`) : les 3 IDs à choisir dépendent uniquement du couple (taux, canal).
- Un taux Zelty sans correspondance dans `tva_categories` **fera échouer** la création (validations 3 et 4). Aucune création à la volée de taux n'existe.
- Les prix sont en **centimes entiers** côté Wello, en euros décimaux dans le formulaire (et probablement dans Zelty) → arrondi à décider.
- `price_uber_eats` / `price_deliveroo` sont dérivés (`max` des 3 prix) et non couverts par le mapping Zelty.

### 3.4 Divergences entre le mapping Zelty cible et le schéma réel

| Mapping visé | Réalité Wello | Écart |
|---|---|---|
| Zelty Tag (1er) → **Catégorie** | `productcateg` — 1 par produit, obligatoire, `products.category` = `merchant_categ_id` (varchar) | ✅ compatible. ⚠️ pas d'unicité de nom → dédup applicative obligatoire. ⚠️ `merchant_categ_id` est `varchar(20)`, renseigné en 2 requêtes non transactionnelles |
| Zelty Tag (suivants) → **Tag** | `tags` + `product_tags` | ✅ compatible. ⚠️ voir §3.1 |
| Zelty Produit → **Produit** | `products` | ✅ nom/prix/3 TVA couverts. ❌ `name` doit passer la validation d'unicité **à double appel Redis** (§3.6) |
| Zelty Option → **Attribut non rattaché** | `configurable_attributes` + `product_configurable_attribute` séparés | ✅ le découplage existe. ⚠️ `product_id` est `NOT NULL` sans défaut sur `configurable_attributes` → doit être forcé à `0`. ⚠️ `max_options` est `NOT NULL` **sans défaut** : l'import doit fournir une valeur (le fichier Zelty la contient-il ? **[À CLARIFIER]**). ⚠️ `is_required` a un défaut `true` et n'est jamais écrit par le module menu |
| Zelty Option Value → **Option (prix = supplément)** | `configurable_attribute_options.extra_price` (centimes) | ✅ sémantique correcte. ⚠️ **`title` est `varchar(25)`** → troncature très probable sur des libellés Zelty réels. ⚠️ `enabled` reste un `integer` 0/1 sur cette table |
| Rattachement post-import via la matrice | `AttributesMatrixDialog` → `PATCH /menu/products/{id}/attributes`, 1 requête par produit | ✅ réutilisable. ⚠️ non atomique, échecs partiels par ligne |

**Non couvert par le mapping et pourtant obligatoire à la création** : `category_id` (validé), `tva_in_id` + `tva_take_away_id` + `tva_delivery_id` (validés, `NOT NULL`).

**Non couvert par le schéma** : `configurable_attributes` n'a **ni description, ni ordre d'affichage** ; `configurable_attribute_options` n'a **pas de colonne d'ordre** explicite.

### 3.5 Absence totale de traçabilité d'import

Aucune colonne `provider` / `external_id` / `sku` sur `products`, `productcateg`, `tags`, `configurable_attributes`, `configurable_attribute_options`.

Conséquences directes :
- **Aucune idempotence possible** : réimporter le même fichier recrée tout (produits en doublon acceptés après confirmation, catégories et tags homonymes sans contrainte).
- **Aucun rollback post-commit** : impossible d'identifier « ce qui vient de l'import n°X ».
- **Aucune reprise sur erreur** : un commit partiellement échoué ne laisse aucune trace exploitable.

Le seul patron existant pour ce besoin est celui des tables `integration_*_mapping` (Uber Eats / Deliveroo), qui portent `merchant_id`, l'ID externe, l'ID Wello, `creation_date`, `deletion_date`, `enabled`, avec index unique `(merchant_id, external_id)` pour les attributs et les options.

### 3.6 Ce qui manque pour le pipeline preview → commit atomique

**Côté atomicité**
1. `dbutils.RunInTx` est **réentrant** ✅ — une transaction englobante est techniquement possible.
2. Mais `MenuRepository.CreateProduct` **ouvre sa propre transaction par produit** et exécute ses 5 validations **avant** (hors transaction). Réutiliser tel quel le service de création pour N produits donne **N transactions**, pas une seule — sauf à envelopper l'ensemble dans un `RunInTx` externe, auquel cas les validations lisent dans la transaction et le comportement du mécanisme Redis change.
3. **Le mécanisme de confirmation Redis rend `CreateProduct` non idempotent et non « batchable »** : le 1er appel sur un nom en doublon **échoue** et pose une clé TTL 5 min. Un commit d'import contenant un produit homonyme d'un existant échouera au 1er passage, et ne réussira qu'au 2e appel **strictement identique**. Idem pour `CreateAttribute`.
4. `CreateProductCategory` fait `INSERT` puis `UPDATE merchant_categ_id` **sans transaction explicite**, et l'erreur du second `UPDATE` **n'est pas retournée** ([repository.go:3531-3541](internal/modules/menu/repository.go#L3531-L3541)).
5. `CreateAttribute` n'ouvre **aucune transaction** : l'attribut et ses options sont insérés en N+1 requêtes séparées (réentrant si l'appelant fournit une transaction).
6. `MenuService.CreateProduct` fait `AssignProductMarketingCategory` **hors transaction en best-effort** et re-lit le produit après coup.
7. `setMenuUpdated` et `invalidateMenuCache` sont appelés **par entité**, pas en fin de lot.

**Côté infrastructure**
8. **Aucune bibliothèque Excel dans `go.mod`** → décision à prendre : parsing serveur (nouvelle dépendance) vs parsing navigateur (le back-office n'a pas non plus de lib xlsx — **[À CLARIFIER]**, non vérifié dans `package.json`).
9. **Aucun endpoint multipart générique** ni middleware d'upload : le patron est copié-collé, limité à 5 Mo et à des images (`ValidateImageType`).
10. **Aucun stockage de job / preview** : Redis fournit `Set`+TTL et `SetNX`, mais il n'existe ni structure de job, ni endpoint de polling, ni convention de clé pour ça.
11. **Le pool MySQL est plafonné à 1 connexion ouverte + 1 idle**, durée de vie 3 min (contrainte Hostinger, [internal/database/mysql.go](internal/database/mysql.go)) → toute parallélisation serveur est illusoire, et un import volumineux devient une requête HTTP longue tenant l'unique connexion. Aucune infrastructure de job asynchrone hors cron (`internal/tasks/`, `cmd/api/tasks.go`).

**Côté back-office**
12. Pas de stepper, pas de composant d'upload, pas de react-query sur le menu (`useMenuData` est manuel) → l'écran de preview/mapping est à construire intégralement, en réutilisant au mieux la primitive `dialog` et le patron des `*MatrixDialog`.
13. Le rattachement post-import via `AttributesMatrixDialog` est **non atomique** (N requêtes `PATCH`, 4 en parallèle, échecs par ligne).

**Côté sécurité / gouvernance**
14. Les routes `/menu` ne sont **pas protégées** par `RequirePermission(HasMenuAccess)` alors que le helper existe. Un endpoint d'import y hériterait de la même absence de contrôle RBAC.
15. Il existe une piste d'audit (`audit_logs` avec chaînage de hash — [internal/modules/audit/repository.go](internal/modules/audit/repository.go)) et un logger de requêtes (`api_request_logs` — [internal/middleware/request_logger/logger.go:131](internal/middleware/request_logger/logger.go#L131)), mais **rien n'y est câblé pour le menu** aujourd'hui.

---

## 4. Récapitulatif des points à clarifier

| # | Point |
|---|---|
| 1 | FK `product_tags → tags` : existe-t-elle réellement en base MySQL de prod ? (le code compte dessus, le schéma cible ne la déclare pas) |
| 2 | Liste faisant autorité pour `configurable_attributes.attribute_type` : `CHECK\|QUANTITY` (SQL) vs `CHECK\|RADIO` (Go) vs `CHECK` seul (back-office) |
| 3 | Statut à poser sur les produits créés par import : `'1'` (default), `'available'`, ou `'removed_from_menu'` en attente de validation ? |
| 4 | Sémantique `products.available` vs `products.status` vs `products.enabled` — trois axes, aucune doc |
| 5 | Le fichier Zelty fournit-il `min_options` / `max_options` par option ? (`max_options` est `NOT NULL` sans défaut) |
| 6 | Type réel de `marketing_categories.category_id` (préfixé ou IDENTITY ?) |
| 7 | `package.json` du back-office : présence d'une lib xlsx (non vérifié dans cet audit) |
| 8 | Comportement attendu face au mécanisme de confirmation Redis sur les noms en doublon, dans un contexte d'import de masse |
| 9 | Volume attendu d'un fichier Zelty typique (contrainte 1 connexion DB / requête HTTP longue) |

---

*Audit en lecture seule — aucun fichier existant modifié, aucune migration, aucune implémentation.*
