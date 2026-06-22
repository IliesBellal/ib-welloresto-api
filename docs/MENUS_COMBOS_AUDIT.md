# Audit — Menus / Combos
## Document de référence pour l'implémentation côté Kiosk

> Généré le 2026-06-22. Audit purement descriptif : aucun code de production n'a été ajouté ni modifié. Seul ce fichier est créé.

---

## 1. Modèle de données existant

### 1.1 Pas de table dédiée "combo" ou "menu composé"

Aucune table SQL nommée `combo`, `combos`, `menu_item`, `menu_combo`, `product_step` ou équivalent n'existe dans le repo. Recherche exhaustive :

- `Grep "combo|Combo|COMBO"` sur tout le repo → un seul résultat, sans rapport : `internal/middleware/permissions.go:114`, un commentaire français *"IsFullyVerified est un combo pour les responsables (Email + Tel)"* — aucune notion produit.
- `Grep "ProductConfigurationDto"` → **aucun résultat**. Le fichier `docs/POS_PRICING_AND_RECEIPT_PATTERNS.md` mentionné dans le brief **n'existe pas** dans ce repo (`Glob docs/POS_PRICING_AND_RECEIPT_PATTERNS.md` et `Glob docs/POS*.md` → aucun fichier trouvé). Il n'a donc pas pu être lu.
- `Grep "ProductStep"` → aucun résultat.
- Aucun fichier dans `migrations/done/` ni `migrations/todo/` ne crée de table `combo*`, `menu_item*`, `product_step*` ou similaire (`Glob migrations/**/*.sql` listé intégralement, 50+ fichiers inspectés par nom — aucun ne correspond).

**Conclusion** : la notion de "combo" telle que décrite dans le brief (produit parent → étapes de configuration → choix de sous-produits par étape, avec contraintes min/max et pricing dépendant des choix) **n'existe nulle part dans le code ou le schéma actuel**. Ce n'est pas une supposition — c'est une absence vérifiée par grep exhaustif et lecture des migrations.

### 1.2 Ce qui existe réellement : deux mécanismes distincts, tous deux partiels

#### a) `configurable_attributes` / `configurable_attribute_options` — modificateurs simples

Table **pré-existante avant ce repo de migrations** (aucune migration dans `migrations/` ne la crée — elle existe déjà en base, héritée d'un schéma antérieur). Utilisée intensivement par `internal/modules/menu/repository.go`. Structure déduite des requêtes SQL réelles :

```sql
-- internal/modules/menu/repository.go:202-205
SELECT id, attribute_type, name, title, min_options, max_options
FROM configurable_attributes
WHERE merchant_id = ? AND enabled = 1
```

```sql
-- internal/modules/menu/repository.go:236-240
SELECT cao.id, cao.configurable_attribute_id, cao.title, cao.max_quantity, cao.extra_price, cao.enabled
FROM configurable_attributes ca
INNER JOIN configurable_attribute_options cao ON cao.configurable_attribute_id = ca.id
WHERE ca.merchant_id = ? AND ca.enabled = 1 AND cao.enabled = 1
```

Table de liaison M:N produit ↔ attribut (un attribut comme "Sauce" peut être réutilisé sur plusieurs produits) :

```sql
-- internal/modules/menu/repository.go:828-831
INNER JOIN product_configurable_attribute pca on pca.product_id = p.product_id
INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
```

Colonnes confirmées par les requêtes SQL (pas de `CREATE TABLE` visible, donc reconstitué depuis les `SELECT`/`INSERT`/`UPDATE`) :
- `configurable_attributes` : `id`, `merchant_id`, `attribute_type`, `name`, `title`, `min_options`, `max_options`, `enabled`.
- `configurable_attribute_options` : `id`, `configurable_attribute_id`, `title`, `max_quantity`, `extra_price`, `enabled`.
- `product_configurable_attribute` : `product_id`, `configurable_attribute_id`, `enabled`, `num_order` (`internal/modules/menu/repository.go:3029-3030`).

**Il y a donc déjà, au niveau de l'attribut (= "étape" de configuration), un `min_options` et un `max_options`** — c'est-à-dire une contrainte "choisir entre N et M options" par groupe de modificateurs. C'est un fait important : le mécanisme existant **a déjà** la notion de contrainte min/max par étape, contrairement à ce qu'on pourrait supposer d'un simple "modificateur".

Le Go struct côté `menu` (`internal/modules/menu/models.go:188-204`) :
```go
type Attribute struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`  // attribute_type
	Name    string            `json:"name"`  // name
	Title   string            `json:"title"` // title
	Min     int               `json:"min"`   // min_options
	Max     int               `json:"max"`   // max_options
	Options []AttributeOption `json:"options"`
}

type AttributeOption struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Price       int    `json:"price"`        // extra_price
	MaxQuantity int    `json:"max_quantity"` // max_quantity
	Enabled     bool   `json:"enabled"`      // enabled
}
```

Le Go struct utilisé côté menu/commande (réponse menu, `internal/models/menu_models.go:178-198`) :
```go
type ConfigurableAttribute struct {
	ID            string               `json:"id"`
	ProductID     string               `json:"product_id"`
	OrderItemID   string               `json:"order_item_id"`
	Title         string               `json:"title"`
	MaxOptions    int                  `json:"max_options"`
	MinOptions    int                  `json:"min_options"`
	AttributeType string               `json:"attribute_type"`
	Options       []ConfigurableOption `json:"options"`
}

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

Et côté payload de commande envoyé par le client (`internal/models/create_order_models.go:108-120`) :
```go
type OrderConfigPayload struct {
	Attributes []ConfigAttribute `json:"attributes"`
}
type ConfigAttribute struct {
	ID      string                  `json:"id"`
	Options []ConfigAttributeOption `json:"options"`
}
type ConfigAttributeOption struct {
	ID       string `json:"id"`
	Quantity int    `json:"quantity"`
}
```
(rattaché en réalité via `models.ProductConfiguration`, `internal/models/request_objects.go:318-326`, structure équivalente : `Attributes []ConfigurationAttribute`, chaque `ConfigurationAttribute{ID, Name, Options}`, chaque `ConfigurationOption` portant un ID + quantité.)

C'est ce mécanisme — pas un struct nommé `ProductConfigurationDto` (introuvable) — qui joue le rôle attendu par le brief pour les "modificateurs simples côté POS".

**Limite structurelle** : une option (`AttributeOption`/`ConfigurableOption`) porte un `title`, un `extra_price`, un `max_quantity` — **jamais un `product_id`**. Une option n'est pas un produit du catalogue, c'est une simple ligne de texte avec un supplément de prix (ex. "Sauce piquante, +0€" ou "Extra fromage, +1,50€"). Il n'y a **aucune table de liaison option → produit** dans tout le schéma audité.

#### b) `products.is_product_group` / `by_product_of` / `sub_products` — hiérarchie d'affichage, pas de choix client

Colonnes réelles sur `products` (vues dans les `SELECT` de `internal/modules/menu/repository.go:644, 730, 1198, 1284`) : `is_product_group` (bool), `by_product_of` (FK textuelle vers le produit parent, nullable).

```sql
-- internal/modules/menu/repository.go:727-737 (étape "sub-products")
SELECT p.product_id, p.by_product_of, p.name, ..., p.is_product_group, p.is_available_on_sno, p.status, ...
FROM products p
...
WHERE p.merchant_id = ? AND p.by_product_of IS NOT NULL AND p.status not in ("removed_from_menu") AND p.enabled = 1
```

Côté Go (`internal/models/menu_models.go:59-62`) :
```go
IsProductGroup *bool          `json:"is_product_group,omitempty"`
...
SubProducts    []ProductEntry `json:"sub_products,omitempty"`
```

**Usage réel de ce mécanisme — vérifié dans `scannorder.service.go:269-286` et `kiosk/service.go:982-1010`** : c'est un filtre de **disponibilité**, pas un mécanisme de configuration de commande. La logique exacte (citée de `internal/modules/kiosk/service.go:982-1009`) :

```go
// flattenKioskProducts reproduit la logique de scannorder.ComputeGetMenu :
// un groupe de produits non disponible est remplacé par ses sous-produits
// disponibles ...
func flattenKioskProducts(products []models.ProductEntry, availability map[string]bool, orderType string) []KioskProduct {
	out := make([]KioskProduct, 0, len(products))
	var toAdd []models.ProductEntry
	for _, p := range products {
		isGroup := p.IsProductGroup != nil && *p.IsProductGroup
		if !isGroup && availability[p.ProductID] {
			out = append(out, mapProductEntryToKioskProduct(&p, orderType))
			continue
		}
		if len(p.SubProducts) > 0 {
			toAdd = append(toAdd, p.SubProducts...)
		}
	}
	for _, sp := range toAdd {
		if availability[sp.ProductID] {
			out = append(out, mapProductEntryToKioskProduct(&sp, orderType))
		}
	}
	return out
}
```

Autrement dit : si un produit "groupe" est marqué indisponible (ou si c'est un groupe — la condition `!isGroup` exclut systématiquement les groupes du résultat direct), on remonte ses sous-produits à la place, **chacun affiché et commandé indépendamment comme un produit normal**. Il n'y a **aucune notion de "choisir 2 sous-produits parmi N pour ce groupe"** — le client ne compose pas un panier en sélectionnant des sous-produits d'un groupe dans une même ligne de commande ; soit le groupe est affiché tel quel (rare), soit ses enfants sont affichés en tant que produits indépendants dans la liste du menu.

C'est confirmé par `OrdersService.ComputePricing` / `buildSelectedProducts` (`internal/modules/orders/service.go:479-543`) : chaque `OrderProductPayload` du panier est traité **indépendamment**, son prix vient de `dbp.Price`/`PriceTakeAway`/`PriceDelivery` (le prix du produit lui-même en base), sans jamais consulter `is_product_group`/`by_product_of`/`sub_products`. Ces deux derniers champs ne sont jamais lus pendant le calcul de prix — uniquement pendant la construction de l'affichage du menu.

### 1.3 Mécanisme de prix : fixe en base, pas de calcul "combo"

Le prix d'un produit (`products.price`/`price_take_away`/`price_delivery`/`price_uber_eats`/`price_deliveroo`) est une valeur fixe en base, sélectionnée selon le canal de commande (`internal/modules/orders/service.go:505-518`). Le seul ajustement dynamique de prix au moment de la commande est :
- `applyConfigurationOptionPrices` (`internal/modules/orders/service.go:545-593`) : ajoute `extra_price` de chaque `configurable_attribute_options.id` sélectionnée (table `configurable_attribute_options`, requête `internal/modules/orders/repository.go:923-934`).
- Suppléments libres `Extra []*OrderExtraPayload` (`component_id` + `price`, `internal/models/create_order_models.go:99-102`) — sert pour des composants ajoutés à la main (ex. "ajout bacon"), prix revalidé côté serveur via le même mécanisme anti-fraude.

Il n'existe **aucune** notion de "prix fixe pour un combo, qui dépend de la combinaison de sous-produits sélectionnés" (typiquement : Tacos M = prix fixe quel que soit le choix de viandes ; un supplément si on dépasse N choix). Le système actuel ne sait calculer qu'un prix de produit + somme de suppléments d'options — il ne sait pas dire "ce produit composite a un prix forfaitaire qui inclut déjà 2 choix gratuits, le 3e choix coûte +1€".

### 1.4 Choix obligatoire/optionnel et unique/multiple — partiellement modélisés

`configurable_attributes.min_options`/`max_options` permettent déjà, **au niveau d'un attribut** (un groupe de modificateurs comme "Sauce" ou "Boisson"), d'exprimer :
- Obligatoire : `min_options >= 1`.
- Optionnel : `min_options = 0`.
- Choix unique : `max_options = 1` (`attribute_type` semble distinguer aussi `CHECK`/`RADIO`, vu en commentaire `internal/modules/menu/models.go:304` : *"attribute_type (e.g., "CHECK", "RADIO")"*).
- Choix multiple : `max_options > 1`.

C'est donc **déjà capable d'exprimer "2 viandes parmi 3" au niveau d'un seul attribut** (`min_options=2, max_options=2` ou `min_options=1, max_options=2` selon le besoin). Ce qui manque structurellement (voir section 2) : faire porter cette contrainte sur des **sous-produits du catalogue** plutôt que sur des options textuelles sans `product_id`, et enchaîner plusieurs attributs en **étapes successives liées à un seul produit composite vendu à prix fixe**.

---

## 2. Mécanisme de configuration (vs modificateurs simples)

### 2.1 Le mécanisme `configurable_attributes`/`options` est le seul existant — peut-il porter un combo Tacos ?

**Partiellement, avec des limitations sérieuses.** Ce que le mécanisme permet déjà, tel qu'il est utilisé aujourd'hui par `menu`/`orders`/`scannorder`/`kiosk` :
- Un produit (`product_configurable_attribute`) peut être associé à plusieurs attributs (étapes) — donc un "Tacos" pourrait avoir l'attribut "Viande" et l'attribut "Sauce" et l'attribut "Boisson", chacun avec son `min_options`/`max_options`.
- Chaque attribut a son propre choix unique/multiple via `min_options`/`max_options`/`attribute_type`.
- Un supplément de prix par option (`extra_price`) est déjà géré et sécurisé côté serveur (`applyConfigurationOptionPrices`).

Ce que ce mécanisme **ne permet pas**, tel qu'il existe aujourd'hui :
1. **Une option n'est pas un produit du catalogue.** `AttributeOption`/`ConfigurableOption` n'a pas de `product_id`. Pour un Tacos où chaque "viande" est en réalité un vrai produit du menu (avec son propre nom, son allergènes, sa disponibilité, son image), il faudrait soit (a) dupliquer manuellement chaque viande comme une simple `AttributeOption` texte (perd la richesse du produit catalogue : allergènes, image, dispo, stock), soit (b) étendre le schéma pour que l'option référence un `product_id` — ce qui n'existe pas actuellement.
2. **Pas de prix forfaitaire ("combo") indépendant de la somme des composants.** Le système actuel calcule prix produit + somme des `extra_price` des options sélectionnées. Il ne sait pas représenter "ce produit composite vaut 9,90€ peu importe les choix faits dans la limite des quotas inclus, et seulement les choix au-delà du quota sont facturés en supplément" sans qu'on configure manuellement chaque `extra_price` à 0 pour les options "incluses" et un montant pour les options "en supplément" — ce qui est faisable en configuration produit par produit, mais reste fragile et ne représente pas un vrai forfait.
3. **Pas de relation hiérarchique entre attributs.** Toutes les étapes d'un produit sont au même niveau (`product_configurable_attribute`, M:N plat). Il n'y a aucune notion de dépendance entre étapes (ex. "si Boisson = Alcool, alors une étape supplémentaire 'vérification d'âge' apparaît") — non demandé dans le cas Tacos simple, mais à noter comme limite générale.
4. **Pas de gestion de la disponibilité par option liée au stock du sous-produit réel.** Puisque l'option n'est pas un produit, sa disponibilité ne suit pas `is_available_on_kiosk`/`is_available_on_sno`/`enabled` du catalogue produit — elle a son propre champ `enabled` indépendant (`configurable_attribute_options.enabled`), à gérer en double si on veut que "Viande indisponible en stock" se répercute aussi en configuration de combo.

### 2.2 Jusqu'où le mécanisme existant pourrait-il être étendu ?

**Étendre raisonnablement** : ajouter un `product_id` nullable sur `configurable_attribute_options` (ou une nouvelle table de liaison `configurable_attribute_option_products`) permettrait à une option de référencer un vrai produit du catalogue plutôt qu'un simple libellé texte. Cela résoudrait le point 1 ci-dessus sans casser l'existant (les options actuelles sans `product_id` resteraient de simples libellés texte, comportement inchangé).

**Devient fragile au-delà** : représenter un prix forfaitaire avec quota d'inclusions gratuites (point 2) et des sous-produits ayant eux-mêmes leurs propres modificateurs ("le burger du menu a lui-même un choix de cuisson") nécessiterait une refonte du modèle de pricing (`buildSelectedProducts`/`applyConfigurationOptionPrices`), pas seulement un ajout de colonne. Le système de pricing actuel raisonne ligne de commande par ligne de commande (`OrderProductPayload`), chacune indépendante ; un combo à étapes avec sous-produits configurables imbriqués demande une structure récursive/arborescente que rien dans `models.OrderProductPayload` ne porte aujourd'hui (`Config *ProductConfiguration` est plat, une seule liste d'attributs, pas de configuration imbriquée par sous-produit).

**Conclusion de cette section** : le mécanisme existant peut couvrir un Tacos à 3 étapes **si chaque "sous-produit" reste un simple libellé texte avec supplément** (perte d'information catalogue : pas d'allergène/image/dispo par viande) **et si le prix forfaitaire est simulé manuellement** (suppléments à 0 pour les choix inclus). Il ne peut pas couvrir nativement le cas où chaque "viande" est un vrai produit du catalogue avec son propre cycle de vie, ni un sous-produit ayant lui-même des modificateurs. Modéliser cela correctement nécessite une extension de schéma (a minima un lien option → produit) et potentiellement une refonte du calcul de prix pour des structures imbriquées — ce n'est pas une extension "gratuite" du mécanisme actuel, c'est un chantier de modélisation distinct, même s'il peut réutiliser certains éléments (le concept min/max par étape, la sécurisation des prix côté serveur).

---

## 3. Endpoints actuels et leurs contrats JSON

### 3.1 Routes confirmées dans `cmd/api/routes.go`

**Menu (back-office, protégé `authMiddleware`)** — `cmd/api/routes.go:634-723` :
```go
r.Route("/menu", func(r chi.Router) {
	r.Use(authMiddleware)
	r.Get("/", menuH.GetMenu)
	r.Get("/products", menuH.GetAllProducts)
	r.Get("/products/{product_id}", menuH.GetProduct)
	r.Patch("/products/{product_id}/attributes", menuH.UpdateProductAttributes)
	r.Get("/attributes", menuH.GetAttributes)
	r.Get("/attributes/{attribute_id}", menuH.GetAttribute)
	r.Post("/attributes", menuH.CreateAttribute)
	r.Patch("/attributes/{attribute_id}", menuH.UpdateAttribute)
	r.Delete("/attributes/{attribute_id}", menuH.DeleteAttribute)
	// ... (catégories, composants, allergènes, discounts, availabilities — hors sujet combo)
})
```

**ScanNOrder (public, QR code)** — `cmd/api/routes.go:567-586` :
```go
r.Route("/scannorder", func(r chi.Router) {
	r.Get("/{merchant_slug}/menu", scannHandler.GetMenu)
	r.Get("/{merchant_slug}/products/{product_id}", scannHandler.GetProduct)
	r.Post("/{merchant_slug}/pricing", scannHandler.GetPricingSNO)
	r.Post("/{merchant_slug}/orders", scannHandler.CreateOrderSNO)
	// ...
})
```

**Kiosk (device, protégé `middleware.KioskAuth`)** — `cmd/api/routes.go:1101-1121` :
```go
r.Route("/kiosk", func(r chi.Router) {
	r.Post("/auth/enroll", kioskHandler.EnrollDevice)
	r.Post("/auth/token/refresh", kioskHandler.RefreshDeviceToken)
	r.Group(func(r chi.Router) {
		r.Use(middleware.KioskAuth(kioskService))
		r.Get("/menu", kioskHandler.GetKioskMenu)
		r.Get("/products/{product_id}", kioskHandler.GetKioskProduct)
		r.Post("/pricing", kioskHandler.GetKioskPricing)
		r.Post("/orders", kioskHandler.CreateKioskOrder)
		r.Get("/orders/{order_id}", kioskHandler.GetKioskOrder)
		// ...
	})
})
```

### 3.2 `GET /menu` (et équivalents ScanNOrder/Kiosk) — que retourne le produit parent ?

`ProductEntry` (`internal/models/menu_models.go:24-83`) est la structure unique réutilisée par tous les canaux (back-office, ScanNOrder, Kiosk). Champs pertinents pour la question :
```go
type ProductEntry struct {
	ProductID      string               `json:"product_id"`
	Name           string               `json:"name"`
	Price          int64                `json:"price"`
	...
	IsProductGroup *bool                `json:"is_product_group,omitempty"`
	SubProducts    []ProductEntry       `json:"sub_products,omitempty"`
	Configuration  ConfigurableResponse `json:"configuration"`
	...
}
```

Donc **oui**, le menu retourne déjà, pour chaque produit, sa `Configuration.Attributes` (les modificateurs/étapes possibles, avec leurs options et `min_options`/`max_options`) **et** ses `SubProducts` (si c'est un groupe). Mais comme établi en section 1.2, `SubProducts` n'est qu'un mécanisme d'affichage de remplacement (groupe indisponible → afficher les enfants), pas une liste de choix pour composer une commande — un client/Kiosk qui voudrait "configurer un combo" en lisant `sub_products` se tromperait sur le sens réel de ce champ.

**Réponse à la question du brief** : `GET /menu` retourne donc la configuration complète du produit (attributs + options) telle qu'elle existe, mais cette configuration **ne référence jamais un sous-produit du catalogue** — uniquement des libellés textuels avec supplément de prix. Aucun champ ne permet aujourd'hui de dire "cette option correspond au produit catalogue X".

### 3.3 ScanNOrder — pattern de payload `POST /{merchant_slug}/orders`

Body = `models.PricingRequest` (alias du même type que `GetPricingSNO`), dont le cœur est `Order.Products []OrderProductPayload`. Exemple de structure d'un item avec modificateurs simples (basé sur `internal/models/create_order_models.go:80-120`) :

```json
{
  "product_id": "prod-tacos-m",
  "quantity": 1,
  "extra": [{ "component_id": "comp-bacon", "price": 150 }],
  "without": [{ "component_id": "comp-onion" }],
  "configuration": {
    "attributes": [
      { "id": "attr-sauce", "options": [{ "id": "opt-sauce-blanche", "quantity": 1 }] }
    ]
  }
}
```

C'est strictement le pattern "produit + modificateurs plats" — il n'y a aucune structure de payload existante pour "ce produit est un combo, voici le sous-produit choisi à l'étape Viande, voici celui choisi à l'étape Boisson" si ces sous-produits doivent être de vrais `product_id` du catalogue. Le seul moyen actuel de simuler ça serait via `configuration.attributes` avec des options purement textuelles (sans lien vers un `product_id` réel), ou en ajoutant plusieurs lignes indépendantes au panier (mais alors ce n'est plus "un Tacos configuré", c'est "trois produits distincts dans le panier", sans lien entre eux ni prix forfaitaire).

### 3.4 Kiosk — `GET /kiosk/menu`, manque-t-il des champs ?

`kiosk.Service.GetMenu` (`internal/modules/kiosk/service.go`) consomme `menuService.GetMenuFromMerchantIdWithMarketing` (le même `ProductEntry` que les autres canaux) puis filtre via `flattenKioskProducts` (citée en 1.2) sur `is_available_on_kiosk` au lieu de `is_available_on_sno`. **Aucune transformation supplémentaire n'est appliquée à `Configuration`/`SubProducts`** au-delà de ce filtre de disponibilité — donc le Kiosk reçoit exactement les mêmes informations (et les mêmes limites) que ScanNOrder concernant les combos : la `Configuration.Attributes` (modificateurs texte) est présente, mais rien ne permet de configurer un vrai combo à sous-produits catalogue.

`KioskProduct`/`KioskMenuResponse` (vus dans `internal/modules/kiosk/service.go`, non détaillés ici car identiques en substance à `ProductEntry` simplifié) n'introduisent aucun champ combo supplémentaire.

---

## 4. Cas concret : commander un combo Tacos

### 4.1 Hypothèse de départ

Tacos Menu : choix de 2 viandes parmi {Poulet, Bœuf, Merguez, Cordon Bleu}, choix d'1 sauce parmi {Blanche, Algérienne, Harissa}, choix d'1 boisson parmi {Coca, Eau, Fanta} — avec un prix forfaitaire (ex. 9,90€) quel que soit le choix dans ces limites.

### 4.2 Faisable aujourd'hui sans changement d'API ? Oui, mais avec une perte de fidélité importante

**Côté configuration produit (back-office)**, en utilisant le mécanisme `configurable_attributes` existant :
1. Créer un produit "Tacos Menu" à 9,90€ dans `products` (prix fixe, `price = 990`).
2. Créer trois attributs via `POST /menu/attributes` :
   - `{"type":"CHECK","name":"viandes","title":"Choisissez vos 2 viandes","min":2,"max":2}` avec options `{"title":"Poulet","price":0}`, `{"title":"Bœuf","price":0}`, `{"title":"Merguez","price":0}`, `{"title":"Cordon Bleu","price":150}` (supplément si la viande est "premium", par exemple).
   - `{"type":"RADIO","name":"sauce","title":"Choisissez votre sauce","min":1,"max":1}` avec 3 options à `price: 0`.
   - `{"type":"RADIO","name":"boisson","title":"Choisissez votre boisson","min":1,"max":1}` avec 3 options à `price: 0`.
3. Associer les trois attributs au produit "Tacos Menu" via `PATCH /menu/products/{product_id}/attributes`.

**Côté lecture menu** (`GET /kiosk/menu` ou `GET /scannorder/{slug}/menu`) — le produit "Tacos Menu" est retourné avec sa `Configuration.Attributes` peuplée des 3 étapes ci-dessus, **chacune portant son `min_options`/`max_options`** — donc l'écran Kiosk peut effectivement afficher "choisissez 2 viandes parmi 4", "choisissez 1 sauce", "choisissez 1 boisson" et **valider côté client** ces contraintes avant envoi (UX correcte).

**Côté commande** (`POST /kiosk/orders` ou `POST /scannorder/{slug}/orders`), payload JSON concret réalisable aujourd'hui :
```json
{
  "fulfillment_type": "DINE_IN",
  "idempotency_key": "kiosk-abc123",
  "payment_method": "pay_at_counter",
  "items": [
    {
      "product_id": "prod-tacos-menu",
      "quantity": 1,
      "configuration": {
        "attributes": [
          { "id": "attr-viandes", "options": [
            { "id": "opt-viande-poulet", "quantity": 1 },
            { "id": "opt-viande-boeuf", "quantity": 1 }
          ]},
          { "id": "attr-sauce", "options": [{ "id": "opt-sauce-blanche", "quantity": 1 }] },
          { "id": "attr-boisson", "options": [{ "id": "opt-boisson-coca", "quantity": 1 }] }
        ]
      }
    }
  ]
}
```

**Prix retourné** : `990` (prix du produit "Tacos Menu") + somme des `extra_price` des options sélectionnées (`0` pour Poulet/Bœuf/sauce/boisson dans cet exemple) = `990`. Recalculé et sécurisé côté serveur via `applyConfigurationOptionPrices`/`GetConfigurationOptionPrices` (`internal/modules/orders/repository.go:923-934`) — le client ne peut pas truquer le prix.

### 4.3 Ce qui manque réellement (limites concrètes du scénario ci-dessus)

- **Les "viandes" ne sont pas des produits du catalogue.** Si Poulet/Bœuf/Merguez/Cordon Bleu existent déjà comme produits indépendants dans le menu (avec allergènes, image, stock), il faut soit les dupliquer en libellés texte dans `configurable_attribute_options` (double saisie, désynchronisation possible si le produit "Poulet" change de nom ou devient indisponible), soit accepter que les "viandes" du combo n'aient ni image, ni allergène, ni lien avec le stock du composant correspondant.
- **Aucune validation de stock/disponibilité réelle du composant viande** au moment de la commande Tacos (l'option "Poulet" du combo a son propre `enabled` indépendant du produit "Poulet" du menu, s'il existe).
- **Le ticket cuisine/KDS** ne recevra "viandes : Poulet, Bœuf" que comme un texte de configuration attaché à la ligne "Tacos Menu" — pas comme deux lignes de production distinctes avec leurs propres statuts de production (`production_status`) ; ce comportement est probablement correct pour la cuisine («c'est un seul item du panier») mais à valider avec le métier.
- **Aucune limite structurelle anti-erreur côté serveur** au-delà de `extra_price` connu : si le client envoie 3 viandes au lieu de 2, **rien dans `ComputePricing`/`buildSelectedProducts` ne rejette la requête** — `applyConfigurationOptionPrices` se contente d'appliquer le prix de chaque option présente, sans jamais vérifier `min_options`/`max_options` côté serveur. Cette validation de contrainte (actuellement uniquement déclarative en base, jamais appliquée en commande) est un **trou de validation existant**, pas spécifique au Kiosk — vérifié par lecture complète de `applyConfigurationOptionPrices` (`internal/modules/orders/service.go:545-593`), qui ne fait que mapper les prix, sans aucune vérification de cardinalité.

**Conclusion section 4** : le scénario Tacos est **réalisable aujourd'hui sans aucun changement d'API**, à condition d'accepter (a) que les sous-produits du combo soient de simples libellés texte avec supplément, sans lien avec le catalogue produit réel, et (b) qu'aucune contrainte min/max ne soit validée côté serveur au moment de la commande (seulement déclarée en base et, dans le meilleur des cas, validée côté client Kiosk avant envoi).

---

## 5. Constats et recommandations

### (a) Faisable côté Kiosk avec l'API actuelle, sans rien changer
- Afficher un produit composite (ex. "Tacos Menu") avec ses étapes de configuration (`Configuration.Attributes`), chaque étape portant déjà `min_options`/`max_options` et un type choix unique/multiple (`attribute_type`).
- Envoyer une commande avec ces choix via `configuration.attributes` dans `POST /kiosk/orders`, avec un prix recalculé et sécurisé côté serveur (suppléments uniquement, prix de base fixe).
- Couvrir des cas de modificateurs simples très bien (sauce, taille, suppléments) — c'est le cas d'usage pour lequel le mécanisme a clairement été conçu.
- Simuler un combo à prix fixe en mettant `extra_price = 0` sur les options "incluses" — fonctionnel mais fragile et sans lien catalogue (voir limites 4.3).

### (b) Changements minimes (champs ajoutés à des réponses/requêtes existantes, sans nouvelle table)
- Ajouter une validation serveur de `min_options`/`max_options` au moment de `ComputePricing`/`buildSelectedProducts` (actuellement absente) — corrige un vrai trou de validation, utile même sans aller jusqu'au "vrai" combo.
- Exposer, en lecture seule, un champ optionnel `linked_product_id` sur la réponse `ConfigurableOption` si on décide de lier une option à un produit existant (nécessite tout de même une colonne en base, donc à la frontière avec (c) — listé ici parce que ça ne crée pas de nouvelle table, juste une colonne sur une table existante).

### (c) Changements majeurs (nouvelle table, nouvelle modélisation, nouveaux endpoints)
- **Lier une option à un vrai produit du catalogue** : nouvelle colonne `product_id` (nullable) sur `configurable_attribute_options`, ou nouvelle table de liaison dédiée — nécessite de revoir tout le pipeline (`GetAttributes`, `applyConfigurationOptionPrices`, mapping JSON côté menu/scannorder/kiosk) pour propager ce lien jusqu'à l'affichage (allergènes, image, dispo) et la commande.
- **Pricing forfaitaire réel pour un combo** (prix fixe incluant un quota de choix gratuits, avec règle de dépassement) : nécessite une refonte du modèle de pricing — aujourd'hui le prix est `prix produit + somme des extra_price`, point. Aucune notion de "quota inclus" formalisée.
- **Validation stricte des contraintes min/max côté serveur à la création de commande** — actuellement un trou de validation (voir 4.3), corrigible avec ou sans refonte combo, mais devient indispensable si on veut un vrai système de combo fiable (sinon un Kiosk compromis pourrait commander 5 viandes pour le prix de 2).
- **Sous-produits configurables eux-mêmes** (ex. "le burger du menu a sa propre cuisson") : nécessite une structure récursive/arborescente absente de `models.OrderProductPayload`/`ProductConfiguration` actuels (structures plates, un seul niveau d'attributs).

### (d) Recommandation : étendre l'existant, ne pas modéliser à part — mais avec un chantier de fond non trivial

**Étendre le mécanisme `configurable_attributes`/`options` plutôt que créer un système de combo entièrement séparé**, pour les raisons suivantes :
1. Le concept de contrainte min/max par étape de choix **existe déjà** et fonctionne (juste non validé côté serveur — un bug à corriger, pas un manque structurel).
2. Le pricing sécurisé (suppléments revalidés côté serveur, anti-fraude) **existe déjà** et est réutilisé par tous les canaux (ScanNOrder, Kiosk, POS implicitement via `orders.OrdersService`).
3. Créer un système de combo entièrement séparé (nouvelle table "menu composé", nouveau moteur de pricing dédié) dupliquerait cette logique de validation/sécurité déjà mûre, avec le risque de désynchronisation entre deux mécanismes de configuration de produit coexistants (lequel choisir pour un nouveau produit ? les deux se chevauchent-ils ?).

**Mais** : "étendre" ici signifie un chantier réel, pas un ajustement cosmétique — a minima ajouter le lien option → produit catalogue et la validation serveur des contraintes, potentiellement revoir le modèle de pricing pour un vrai prix forfaitaire. Ce n'est pas une fonctionnalité "presque prête" qu'il suffirait d'exposer côté Kiosk : **le Kiosk ne peut pas afficher/vendre un vrai combo Tacos avec sous-produits catalogue tant que ce chantier n'est pas fait**, quel que soit l'effort mis côté Kiosk seul.

---

## 6. Plan d'action proposé (par ordre d'effort croissant)

1. **Corriger le trou de validation existant** : ajouter la vérification de `min_options`/`max_options` côté serveur dans `OrdersService.buildSelectedProducts`/`applyConfigurationOptionPrices` (ou une étape dédiée juste avant), rejeter une commande qui ne respecte pas les contraintes déclarées sur `configurable_attributes`. Effort faible, bénéfice immédiat pour tous les canaux (pas seulement Kiosk), corrige un risque de fraude/erreur déjà présent aujourd'hui.
2. **Documenter/valider le mécanisme `is_product_group`/`sub_products`** auprès du métier pour confirmer qu'il s'agit bien d'un mécanisme d'affichage de repli (pas un futur socle de combo) — éviter toute confusion future qui ferait construire des fonctionnalités Kiosk dessus par erreur.
3. **Simuler un premier combo "MVP" avec le mécanisme existant tel quel** (options textuelles avec `extra_price`, prix fixe sur le produit composite) pour valider l'expérience Kiosk réelle (UX de sélection multi-étapes, affichage du prix) avant d'investir dans un chantier de modélisation plus profond — permet de tester le besoin produit à coût quasi nul.
4. **Ajouter le lien option → produit catalogue** (`configurable_attribute_options.product_id` nullable, ou table de liaison dédiée) si le besoin de cohérence catalogue (allergènes, image, stock par "sous-produit" du combo) est confirmé comme bloquant pour le lancement — chantier moyen, touche `menu`, `orders`, `scannorder`, `kiosk`.
5. **Concevoir un vrai modèle de pricing forfaitaire** (quota de choix inclus + règle de dépassement) si le besoin business de "prix combo indépendant de la somme des options" est confirmé — chantier le plus lourd, touche le cœur du calcul de prix partagé par tous les canaux de commande. À ne lancer qu'après validation produit ferme (voir questions ouvertes), car il a un rayon d'impact sur tout le système de commande, pas seulement Kiosk.

---

## Questions ouvertes

1. **Les "sous-produits" d'un combo doivent-ils être de vrais produits du catalogue** (avec allergènes/image/stock/disponibilité propres) **ou de simples libellés de modificateur** (comme aujourd'hui) ? Cette décision détermine si l'étape 4 du plan d'action est nécessaire ou non.
2. **Le prix d'un combo doit-il être strictement forfaitaire** (ex. toujours 9,90€ peu importe la combinaison dans les quotas) **ou peut-il rester une somme produit + suppléments** (ce que le système actuel sait déjà faire, en mettant les suppléments "inclus" à 0) ? Si la seconde option est acceptable, l'étape 5 du plan d'action devient inutile.
3. **Faut-il valider les contraintes min/max côté serveur dès maintenant**, indépendamment de tout projet combo (corrige un trou de sécurité déjà présent), ou attendre que le projet combo soit cadré pour le faire en une seule fois ?
4. **Le ticket cuisine/KDS a-t-il besoin de voir chaque "sous-produit" d'un combo comme une ligne de production distincte** (avec son propre statut `production_status`), ou un seul texte de configuration attaché à la ligne "Tacos Menu" suffit-il ? Cette réponse impacte fortement la complexité du chantier de modélisation (étape 4/5).
5. **Quel est le périmètre réel attendu pour le MVP Kiosk** : juste des modificateurs simples (déjà couverts à 100% aujourd'hui), ou de vrais combos à étapes avec sous-produits catalogue (nécessite les chantiers 4 et 5) ? Cette priorisation produit doit être tranchée avant tout travail d'implémentation Kiosk sur ce sujet, pour éviter de construire une UI de configuration qui anticipe des capacités serveur qui n'existent pas encore.
6. **Le fichier `docs/POS_PRICING_AND_RECEIPT_PATTERNS.md` mentionné dans le brief existe-t-il ailleurs** (autre repo, document non versionné) ? Il n'a pas été trouvé dans ce repo — si un tel document existe et documente un `ProductConfigurationDto` plus riche que ce qui a été trouvé ici, il faudrait le confronter à cet audit avant de trancher les recommandations ci-dessus.
