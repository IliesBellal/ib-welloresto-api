# Audit — Images sur les options de configuration produit
### Préparation, lecture seule — aucune modification de code dans cette session

Généré le : 2026-06-24.

**Objectif** : auditer l'état actuel de `configurable_attribute_options` / `configurable_attributes` avant d'envisager l'ajout d'une image par option (ex. variantes visuelles "Coca / Sprite / Fanta" dans un wizard de personnalisation produit, affichées aussi en pills compactes).

---

## 1. Schéma actuel

### Constat structurant : pas de migration trouvée pour ces deux tables

`grep -rl "configurable_attribute" migrations/` ne retourne **aucun résultat**. Ces deux tables sont des tables **legacy**, créées avant l'introduction du système de migration séquentiel du projet (comme `upsell_suggestions`, déjà documenté comme cas similaire dans un audit précédent). **Aucun DDL exact n'est donc disponible dans le repo** — ce qui suit est reconstruit par déduction à partir des colonnes effectivement lues/écrites en SQL brut dans le code Go (`internal/modules/menu/repository.go`), pas copié depuis un fichier de migration.

### `configurable_attributes` — colonnes déduites du code

| Colonne | Type déduit | Usage observé |
|---|---|---|
| `id` | `VARCHAR` (UUID applicatif, `helpers.GeneratePrefixedID(helpers.AttributeIDPrefix)`) | PK |
| `merchant_id` | `VARCHAR` | Scoping multi-tenant |
| `attribute_type` | `VARCHAR`/`ENUM` (valeurs vues : `"CHECK"`, `"QUANTITY"` côté back-office TS) | Type de sélection |
| `name` | `VARCHAR` | Nom interne (visible staff uniquement) |
| `title` | `VARCHAR` | Titre affiché au client |
| `min_options` | `INT` | Min de sélections |
| `max_options` | `INT` | Max de sélections |
| `enabled` | `TINYINT(1)`/`BOOLEAN` | Soft delete (convention `enabled`, confirmée par `UPDATE configurable_attributes SET enabled = 0 WHERE id = ?` dans `DeleteAttribute`) |

Source : [internal/modules/menu/repository.go:198-205](internal/modules/menu/repository.go#L198-L205) (SELECT), [internal/modules/menu/repository.go:333-344](internal/modules/menu/repository.go#L333-L344) (INSERT), [internal/modules/menu/repository.go:415-424](internal/modules/menu/repository.go#L415-L424) (UPDATE).

### `configurable_attribute_options` — colonnes déduites du code

| Colonne | Type déduit | Usage observé |
|---|---|---|
| `id` | `VARCHAR` (UUID, `helpers.GeneratePrefixedID(helpers.AttributeOptionIDPrefix)`) | PK |
| `configurable_attribute_id` | `VARCHAR` | FK applicative vers `configurable_attributes.id` (pas de contrainte FK SQL stricte confirmée — cohérent avec la convention "pas de FK vers tables historiques" documentée dans `ARCHITECTURE_API.md` §6.2) |
| `title` | `VARCHAR` | Libellé option |
| `extra_price` | `INT` (centimes) | Supplément de prix |
| `max_quantity` | `INT` | Quantité max sélectionnable pour cette option |
| `enabled` | `TINYINT(1)`/`BOOLEAN` | Soft delete |

Source : [internal/modules/menu/repository.go:236-240](internal/modules/menu/repository.go#L236-L240) (SELECT avec JOIN), [internal/modules/menu/repository.go:375-396](internal/modules/menu/repository.go#L375-L396) (INSERT), [internal/modules/menu/repository.go:465-484](internal/modules/menu/repository.go#L465-L484) (UPDATE).

**Aucune colonne `image_url`/`photo_url`/`icon_url` n'existe sur `configurable_attribute_options`** — confirmé par la liste exhaustive des colonnes lues/écrites ci-dessus (aucune requête ne sélectionne ni n'écrit un champ de ce type).

**Index / contraintes** : non vérifiables sans accès DB direct (pas de `SHOW CREATE TABLE` possible dans cet environnement, pas de migration de référence). Seule certitude : une jointure `configurable_attribute_options.configurable_attribute_id = configurable_attributes.id` est utilisée partout, donc un index sur cette colonne est probable mais non confirmé.

⚠️ **Action recommandée avant toute migration réelle** : exécuter `SHOW CREATE TABLE configurable_attribute_options;` et `SHOW CREATE TABLE configurable_attributes;` en base pour confirmer types exacts, index et l'éventuelle présence de contraintes non visibles depuis le code Go.

---

## 2. Structs Go actuelles

Trois structs Go distinctes représentent la même paire de tables, pour trois usages différents — **aucune des trois n'a de champ image** :

### a) `models.ConfigurableOption` — struct partagée runtime (menu client, panier, commande)

[internal/models/menu_models.go:193-202](internal/models/menu_models.go#L193-L202) :
```go
type ConfigurableOption struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	ExtraPrice        int    `json:"extra_price"`
	MaxQuantity       int    `json:"max_quantity"`
	ConfigAttributeID string `json:"configurable_attribute_id"`
	OrderItemID       string `json:"order_item_id"`
	Quantity          int    `json:"quantity"`
	Selected          bool   `json:"selected"`
}
```
Imbriquée dans `ConfigurableAttribute` ([menu_models.go:182-191](internal/models/menu_models.go#L182-L191)) elle-même dans `ConfigurableResponse`, porté par `ProductEntry.Configuration` ([internal/models/menu_models.go:63](internal/models/menu_models.go#L63), champ `Configuration ConfigurableResponse`).

### b) `menu.Attribute` / `menu.AttributeOption` — struct CRUD back-office (admin)

[internal/modules/menu/models.go:188-204](internal/modules/menu/models.go#L188-L204) :
```go
// Temporaire, à remplacer par les vraies struct
type Attribute struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Title   string            `json:"title"`
	Min     int               `json:"min"`
	Max     int               `json:"max"`
	Options []AttributeOption `json:"options"`
}

type AttributeOption struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Price       int    `json:"price"`
	MaxQuantity int    `json:"max_quantity"`
	Enabled     bool   `json:"enabled"`
}
```
Le commentaire `// Temporaire, à remplacer par les vraies struct` (présent dans le code source, pas ajouté ici) suggère que ce duplicata était déjà identifié comme dette technique avant cet audit.

Payloads de mutation associés : `UpdateAttributePayload` / `UpdateAttributeOptionPayload` ([internal/modules/menu/models.go:296-314](internal/modules/menu/models.go#L296-L314)) — pas de champ image non plus.

### c) `kiosk.KioskModifierGroup` / `kiosk.KioskModifierOption` — struct d'affichage Kiosk

[internal/modules/kiosk/models.go:296-312](internal/modules/kiosk/models.go#L296-L312) :
```go
type KioskModifierGroup struct {
	ID            string                `json:"id"`
	Title         string                `json:"title"`
	MinOptions    int                   `json:"min_options"`
	MaxOptions    int                   `json:"max_options"`
	AttributeType string                `json:"attribute_type"`
	Options       []KioskModifierOption `json:"options"`
}

type KioskModifierOption struct {
	ID                      string `json:"id"`
	Title                   string `json:"title"`
	ExtraPrice              int    `json:"extra_price"`
	MaxQuantity             int    `json:"max_quantity"`
	ConfigurableAttributeID string `json:"configurable_attribute_id"`
	Selected                bool   `json:"selected,omitempty"`
}
```
Le commentaire en tête de fichier référence explicitement `docs/KIOSK_VS_SCANNORDER_STRUCTS.md` (écart #5) comme document existant traitant des divergences structurelles entre Kiosk et ScanNOrder pour ces modifiers — **pertinent à relire avant de toucher au contrat JSON Kiosk**.

### Pourquoi `image_url` est absent aujourd'hui

Aucune des trois structs ne porte de champ image, et aucune des requêtes SQL listées en section 1 ne sélectionne de colonne de ce type — l'absence est cohérente de bout en bout (DB → Go → JSON), pas un oubli de mapping isolé.

### Endpoints exposant ces structs

| Endpoint | Struct exposée | Module |
|---|---|---|
| `GET /menu/attributes` | `[]menu.Attribute` | back-office (admin) |
| `GET /menu/attributes/{attribute_id}` | `menu.Attribute` | back-office (admin) |
| `POST /menu/attributes` | `menu.Attribute` (en retour) | back-office (admin) |
| `PATCH /menu/attributes/{attribute_id}` | — | back-office (admin) |
| Tout endpoint retournant un produit avec sa configuration (menu complet, détail produit, panier, commande — `ProductEntry.Configuration`) | `models.ConfigurableResponse` → `models.ConfigurableAttribute` → `models.ConfigurableOption` | `menu`, `orders`, `scannorder`, `order_life_cycle` (consommateurs de `ProductEntry`) |
| `GET /kiosk/menu`, `GET /kiosk/products/{id}` (modifiers du produit) | `kiosk.KioskModifierGroup` → `kiosk.KioskModifierOption` | `kiosk` |

`GET /menu` en tant que tel n'est pas un endpoint isolé trouvé dans `routes.go` sous ce nom exact — la configuration produit circule comme **sous-objet** de toute réponse produit (`ProductEntry.Configuration`), pas comme une ressource autonome listée séparément (hors back-office `/menu/attributes`).

---

## 3. Back-office actuel

### Endpoint existant — CRUD complet déjà en place

Contrairement à l'hypothèse de départ du brief ("si non, documenter qu'il faut créer ce point d'entrée"), **un point d'entrée existe déjà et est complet** :

[cmd/api/routes.go:671-675](cmd/api/routes.go#L671-L675) :
```go
r.Get("/attributes", menuH.GetAttributes)
r.Get("/attributes/{attribute_id}", menuH.GetAttribute) // used by: back-office
r.Post("/attributes", menuH.CreateAttribute)            // used by: back-office
r.Patch("/attributes/{attribute_id}", menuH.UpdateAttribute)
r.Delete("/attributes/{attribute_id}", menuH.DeleteAttribute)
```

Handlers : [internal/modules/menu/handler.go:243-369](internal/modules/menu/handler.go#L243-L369) (`GetAttributes`, `GetAttribute`, `CreateAttribute`, `UpdateAttribute`, `DeleteAttribute`).

**Champs acceptés en POST/PATCH** (`UpdateAttributePayload`, voir section 2b) : `type`, `name`, `title`, `min`, `max`, `options[].{id?, title, price, max_quantity?, enabled?, extra_price?}`. Pas de champ image.

**Logique de mutation des options** (`UpdateAttribute`, [internal/modules/menu/repository.go:438-510](internal/modules/menu/repository.go#L438-L510)) : toutes les options existantes sont d'abord désactivées (`enabled = 0`), puis chaque option du payload est soit mise à jour (si `opt.ID` fourni) soit recréée — **un ajout de champ `image_url` devra suivre exactement ce même chemin update-or-insert** pour ne pas être silencieusement perdu à la prochaine sauvegarde.

### Front-end React — page existante

`wello-back-office/src/pages/Attributes.tsx`, consommant le hook `useAttributesData` ([wello-back-office/src/hooks/useAttributesData.ts](wello-back-office/src/hooks/useAttributesData.ts)), lui-même appelant `menuService.getAttributes/createAttribute/updateAttribute` ([wello-back-office/src/services/menuService.ts:769-806](wello-back-office/src/services/menuService.ts#L769-L806)).

Types TS associés ([wello-back-office/src/types/menu.ts:88-124](wello-back-office/src/types/menu.ts#L88-L124)) : `AttributeOption`, `AttributeOptionDetail`, `Attribute` — **aucun champ image** non plus côté front, cohérent avec l'API.

**Conclusion section 3** : il n'y a **rien à créer** côté point d'entrée CRUD — l'endpoint et la page existent déjà et sont fonctionnels. Le travail à prévoir est une **extension** (payload + page existante), pas une création ex nihilo.

---

## 4. Endpoints R2 existants — pattern à réutiliser

### Référence directe : `UploadProductImage` (upload produit)

[internal/modules/menu/handler.go:1195-1292](internal/modules/menu/handler.go#L1195-L1292), route `PUT /products/{product_id}/image` ([cmd/api/routes.go:664](cmd/api/routes.go#L664)). Séquence exacte (à reproduire à l'identique pour une option) :

1. Auth (`helpers.ExtractToken` + `middleware.UserFromContext`).
2. `r.ParseMultipartForm(5 << 20)` (5 Mo max pour un produit — à recalibrer pour une option, probablement plus petit, voir section 6).
3. `r.FormFile("photo")` — nom de champ `"photo"` (convention observée, pas générique `"file"`).
4. Validation MIME via `r2.ValidateImageType(contentType)` — JPEG/PNG/WebP uniquement.
5. Récupération de l'ancienne URL (`GetProductImageURL`) pour suppression différée.
6. Génération de clé déterministe via une fonction `r2.GenerateXxxKey(...)` dédiée.
7. Suppression best-effort de l'ancien fichier R2 (`h.r2Client.DeleteFile`, erreur loguée mais non bloquante).
8. Upload du nouveau fichier (`h.r2Client.UploadFile(ctx, key, file, contentType)` → URL publique).
9. Persistance de l'URL en base (`UpdateProductImage`).
10. Réponse `{ "status": "success", "photo_url": "..." }`.

### Fonctions génératrices de clé existantes (pattern par usage, pas générique)

[internal/infrastructure/r2/client.go](internal/infrastructure/r2/client.go) — **une fonction dédiée par type d'entité**, toutes préfixées par le même bucket logique :

```go
func GenerateProductKey(merchantID, productID, ext string) string
// → "wello_resto_images_storage/merchants/{merchant_id}/products/{product_id}{ext}"

func GenerateScanNOrderKey(merchantID, imageType, ext string) string
// → "wello_resto_images_storage/merchants/{merchant_id}/scannorder/{logo|banner}{ext}"

func GenerateKioskKey(merchantID, imageType, ext string) string
// → "wello_resto_images_storage/merchants/{merchant_id}/kiosk/{logo|idle|idle_video}{ext}"

func GenerateUserAvatarKey(userID, ext string) string
// → "wello_resto_images_storage/users/{user_id}/avatar{ext}"
```

**Convention de nommage de clé** : `wello_resto_images_storage/<scope>/<id>/<usage>{ext}`, toujours **déterministe** (un nouvel upload écrase l'ancien fichier au même chemin — pas de versioning, pas de suffixe horodaté). Une éventuelle `GenerateConfigOptionKey(merchantID, optionID, ext)` suivrait : `wello_resto_images_storage/merchants/{merchant_id}/config_options/{option_id}{ext}`.

**Bucket** : pas de nom de bucket en dur dans ces fonctions — c'est un **préfixe de clé** (`wello_resto_images_storage/...`) à l'intérieur d'un bucket configuré globalement via `R2_PRIVATE_BUCKET` (variable d'environnement requise, voir CLAUDE.md). Le bucket physique est unique pour tout le projet ; la "séparation" entre produits/scannorder/kiosk/users se fait uniquement par préfixe de clé, pas par bucket distinct.

**Validation** : `r2.ValidateImageType` (JPEG/PNG/WebP) et `r2.GetExtensionFromContentType`/`GetContentTypeFromExtension` sont génériques, réutilisables sans modification pour les options.

### Front-end React — pattern d'upload existant

`wello-back-office/src/services/menuService.ts:1108` (`uploadProductImage(productId, file)`) construit un `FormData` et appelle `fetch` directement (pas le client Axios générique du reste de l'app) — **pattern à reproduire à l'identique** pour un upload d'image d'option.

---

## 5. Plan d'implémentation proposé

### A. Migration SQL — `ADD COLUMN image_url`

**Fichiers à toucher** : nouveau `migrations/todo/NNN_add_image_url_to_configurable_attribute_options.up.sql` + `.down.sql` (numéro à déterminer au moment de l'implémentation — vérifier le dernier numéro réellement utilisé dans `migrations/todo/` ET `migrations/done/`, l'un peut être en avance sur l'autre).

```sql
ALTER TABLE configurable_attribute_options
ADD COLUMN IF NOT EXISTS image_url VARCHAR(500) NULL DEFAULT NULL AFTER extra_price;
```

**Dépendances** : aucune — la table existe déjà et est en usage actif, pas de prérequis.

**Effort estimé** : trivial (15 min), mais ⚠️ **point ouvert** : comme documenté en section 1, aucune migration de création n'existe pour cette table dans le repo — un `ALTER TABLE` suppose que la table existe réellement en base de production (ce qui est le cas vu l'usage actif en code), mais ce nouveau fichier de migration sera le premier à référencer cette table dans l'historique de migrations. Cohérent avec le traitement déjà appliqué à `upsell_suggestions` dans une session précédente.

### B. Extension des structs Go et endpoints menu/kiosk

**Fichiers à toucher** :
- `internal/models/menu_models.go` — ajouter `ImageURL *string` (json:`"image_url,omitempty"`) à `ConfigurableOption` (section 2a).
- `internal/modules/menu/models.go` — ajouter le même champ à `AttributeOption`, `UpdateAttributeOptionPayload`, et `UpdateAttributePayload` reste inchangé (porte juste la liste d'options).
- `internal/modules/menu/repository.go` — étendre les 4 requêtes touchées : `GetAttributes` (SELECT ligne 236-240), `GetAttribute` (SELECT ligne 297-300), `CreateAttribute` (INSERT ligne 375-396), `UpdateAttribute` (UPDATE ligne 465-484 **et** INSERT ligne 502+ pour les nouvelles options).
- `internal/modules/kiosk/models.go` — ajouter `ImageURL string` (json:`"image_url,omitempty"`) à `KioskModifierOption`, **si** le besoin d'affichage Kiosk (wizard + pills) est confirmé pour ce canal (cf. brief initial qui mentionne explicitement le Kiosk en section D).
- `internal/modules/kiosk/repository.go:615` — la requête `SELECT id, configurable_attribute_id FROM configurable_attribute_options WHERE id IN (...)` devra aussi sélectionner `image_url`.
- Vérifier `internal/modules/orders/orders_fetcher_builder.go:216-223` et `internal/modules/scannorder/repository.go:957` — ces requêtes peuplent aussi `ConfigurableOption` pour l'affichage côté commande/panier ScanNOrder ; à étendre **seulement si** l'image doit apparaître au-delà du wizard de sélection (ex. sur un ticket de commande — peu probable mais à trancher, voir section 6).

**Dépendances** : la migration (A) doit être appliquée avant que ces requêtes ne référencent la colonne.

**Effort estimé** : modéré (2-3h) — surtout du fait du nombre de points de lecture dispersés (4 modules consommateurs de la même paire de tables).

### C. Back-office — endpoint d'upload + UI

**Pas de nouvelle page à créer** (corrige l'hypothèse du brief) — `Attributes.tsx` existe déjà et gère déjà la liste d'options par attribut.

**À ajouter** :
- API Go : nouvel endpoint `POST /menu/attributes/{attribute_id}/options/{option_id}/image` (ou route plus plate `POST /menu/attribute_options/{option_id}/image`, à trancher selon convention RESTful préférée — la route produit utilise `PUT /products/{product_id}/image`, donc un `PUT /menu/attribute_options/{option_id}/image` serait plus cohérent), reproduisant exactement `UploadProductImage` (section 4) avec une nouvelle fonction `r2.GenerateConfigOptionKey(merchantID, optionID, ext)`.
- `internal/modules/menu/handler.go` : nouveau handler `UploadAttributeOptionImage`, calqué sur `UploadProductImage`.
- `internal/modules/menu/service.go`/`repository.go` : `UpdateAttributeOptionImage(ctx, token, optionID, url)` + `GetAttributeOptionImageURL(ctx, token, optionID)` (pour la suppression de l'ancienne image), symétriques aux méthodes produit existantes.
- `cmd/api/routes.go` : enregistrement de la route dans le groupe `/menu` déjà protégé par l'auth/permission existante (`middleware.HasMenuAccess` ou équivalent — à vérifier au moment de l'implémentation, non confirmé dans cet audit).
- Front-end : dans `Attributes.tsx`, ajouter un bouton/zone d'upload par ligne d'option (reproduisant le pattern `uploadProductImage` de `menuService.ts:1108`), plus une nouvelle fonction `menuService.uploadAttributeOptionImage(optionId, file)`.

**Dépendances** : B (structs Go) doit être en place pour que la réponse `GET /menu/attributes` renvoie l'URL après upload.

**Effort estimé** : modéré-élevé (4-6h, dont une bonne partie en UI React pour l'intégration visuelle dans la liste d'options existante).

### D. Flutter Kiosk — affichage dans le wizard et les pills

**Hors périmètre de cet audit** (le brief le mentionne mais le scope de session est API uniquement) — à auditer séparément côté `wello-kiosk` (working directory disponible : `lib/presentation/widgets`, `lib/presentation/screens`) pour localiser le wizard de personnalisation et le composant "pill" de sélection rapide. Pas de fichier Flutter lu dans cette session.

**Dépendances** : B (le champ doit être exposé par `GET /kiosk/menu`/`GET /kiosk/products/{id}` via `KioskModifierOption.ImageURL`) avant tout travail Flutter.

**Effort estimé** : non évaluable sans audit du repo Flutter Kiosk — à chiffrer séparément.

---

## 6. Questions ouvertes

1. **Taille max de l'image** — `UploadProductImage` utilise 5 Mo pour un produit (image potentiellement grande, plein écran). Une vignette d'option (pill/wizard) est un usage plus petit visuellement : faut-il une limite plus basse (ex. 1-2 Mo) pour éviter des photos disproportionnées par rapport à l'usage réel ?
2. **Format accepté** — reprendre JPEG/PNG/WebP (`r2.ValidateImageType`, déjà générique) semble suffisant ; pas de besoin identifié de SVG/icônes vectorielles pour ce cas d'usage, mais à confirmer si le besoin réel est plus proche d'un pictogramme que d'une photo produit.
3. **Fallback si pas d'image** — actuellement, **aucune** option n'a d'image : le wizard/pills devra avoir un comportement de repli pour la transition (toutes les options existantes resteront `image_url = NULL` après la migration). Faut-il une image par défaut générique, ou simplement masquer la zone image quand absente (texte seul, comme aujourd'hui) ?
4. **Ratio d'affichage pills vs wizard** — le brief mentionne deux contextes d'affichage (wizard de personnalisation, pills rapides). Une seule image par option suffit-elle pour les deux contextes (ratio carré recommandé pour couvrir les deux), ou faut-il prévoir un recadrage/deux tailles distinctes ? Non tranchable sans maquette.
5. **Portée Kiosk vs autres canaux** — le brief ne mentionne explicitement que le Kiosk pour l'affichage, mais `ConfigurableOption` (struct partagée, section 2a) est aussi utilisée par ScanNOrder et potentiellement le POS Flutter. L'image doit-elle apparaître **uniquement** sur Kiosk, ou aussi sur ScanNOrder (client final sur son téléphone) et le POS staff ? Ça change le périmètre de la section B (combien de structs/requêtes étendre réellement) et de la section D (Flutter Kiosk seul, ou aussi `wello_resto_flutter`/`wello-kiosk`).
6. **Convention de route REST** — `PUT /menu/attribute_options/{option_id}/image` (cohérent avec `PUT /products/{product_id}/image`) vs un sous-chemin de l'attribut parent (`POST /menu/attributes/{attribute_id}/options/{option_id}/image`, plus explicite sur la hiérarchie mais plus long) — à trancher avant l'implémentation, pas de précédent strictement identique dans le code actuel (les uploads existants sont tous à plat sur l'entité elle-même, jamais nichés sous un parent).
7. **Numéro de migration réel** — confirmer au moment de l'implémentation le dernier numéro effectivement utilisé dans `migrations/todo/` (045 au moment de cet audit, voir sessions précédentes) avant d'attribuer le numéro à la nouvelle migration de cette section A.
