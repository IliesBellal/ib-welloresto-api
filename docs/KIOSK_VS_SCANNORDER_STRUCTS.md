# Comparaison structs kiosk vs scannorder
## Document de référence pour l'alignement

> **⚠️ Document gelé / historique.** Suite à `docs/KIOSK_DECISIONS.md`,
> Incrément 6 ("alignement complet des contrats pricing/commande sur
> scannorder"), les structs `KioskPricingRequest`, `KioskPricingResponse`,
> `CreateKioskOrderRequest`, `CreateKioskOrderResponse`, `KioskPricingItem` et
> `KioskOrderItem` mentionnées ci-dessous **ont été supprimées** du module
> Kiosk. `POST /kiosk/pricing` et `POST /kiosk/orders` consomment désormais
> directement `models.PricingRequest`/`models.PricingResponse`/
> `models.RequestObject`/`models.CreateOrderResult`, exactement comme
> `scannorder`. Les écarts documentés ci-dessous (notamment les sections
> "Pricing request/response" et "Create order request/response") décrivent
> donc un état du code antérieur, conservé pour archive — voir
> `KIOSK_DECISIONS.md` Incrément 6 pour l'état courant.

> Sources lues : `docs/ARCHITECTURE_API.md`, `docs/KIOSK_DECISIONS.md` (incréments 1 à 5),
> `internal/modules/scannorder/{handler,service,repository,models}.go`,
> `internal/modules/kiosk/{handler,service,repository,models,admin_handler,config}.go`,
> `internal/models/{menu_models.go,create_order_models.go,request_objects.go}`,
> `internal/modules/order_life_cycle/service.go` (`CreateOrder`),
> `internal/modules/orders/service.go` (`ComputePricing`, `buildSelectedProducts`, `applyConfigurationOptionPrices`),
> `cmd/api/routes.go` (déclarations de routes réelles).
>
> État du repo au moment de la lecture : `internal/models/menu_models.go`,
> `internal/modules/menu/models.go` et `internal/modules/menu/repository.go` ont des
> modifications non commitées (visibles via `git status`) — lues telles qu'elles sont
> sur disque. `ProductEntry` (la struct partagée la plus importante pour ce document)
> vit dans `internal/models/menu_models.go`, donc l'état lu est l'état courant non commité.

## Résumé exécutif

> **Mise à jour post-audit (Ilies)** : la qualification "bloquant" de l'écart ci-dessous était une
> erreur d'appréciation de l'auteur de cet audit. Ce n'est **pas** un bug — c'est l'un des seuls
> comportements **volontairement différents** entre Kiosk et ScanNOrder (le Kiosk n'a, à ce stade,
> que le paiement comptoir, donc pas de notion de paiement en ligne déjà encaissé au moment de la
> création). Dans les deux cas, la commande finit `ACCEPTED` — côté Kiosk après confirmation du
> paiement comptoir (`ConfirmCounterPayment`), côté ScanNOrder `IN` immédiatement à la création. Le
> tableau et le détail ci-dessous sont conservés pour la traçabilité de l'audit initial, mais ce
> point ne doit plus être traité comme un correctif à appliquer (voir aussi
> `docs/KIOSK_DECISIONS.md`, Incrément 11, Correction 1).

| Flux | Écarts identifiés | Bloquant | Majeur | Mineur |
|---|---|---|---|---|
| 1 — Menu | 9 | 0 | 4 | 5 |
| 2 — Pricing | 6 | 0 | 3 | 3 |
| 3 — Création de commande | 7 | **0** (voir note ci-dessus) | 4 | 2 |
| **Total** | **22** | **0** | **11** | **10** |

**Ancien "écart bloquant" (requalifié non-bloquant)** : `kiosk.Service.CreateKioskOrder` posait
`orderReq.MerchantApproval = "ACCEPTED"` avant d'appeler `ordersLifeCycleSvc.CreateOrder`, ce qui à
l'époque de l'audit semblait contredire le reste du module (commentaire de la fonction elle-même,
`ConfirmCounterPayment`, `CancelKioskOrder`, le statut renvoyé `"pending_counter_payment"`). En
réalité, les commandes Kiosk aboutissent toutes à `ACCEPTED` (que ce soit immédiatement ou après
confirmation du paiement comptoir) — ce n'est qu'une divergence de timing avec ScanNOrder, pas un
état incohérent. Voir Flux 3 / Plan de correction pour le détail historique de cet écart.

Les écarts majeurs concernent principalement : l'absence de validation anti-fraude des prix
identique à `scannorder.validateAndCleanPricingPayload` (kiosk valide l'existence/disponibilité
en amont mais ne revalide pas explicitement les `option_id` inconnus avec le même niveau
d'exhaustivité avant pricing), la divergence de vocabulaire `order_type`/`fulfillment_type` entre
les deux flux Menu et Pricing/Commande côté Kiosk, et l'absence de gestion `DELIVERY` (volontaire,
non un écart à corriger).

---

## Flux 1 — Menu

### Requête — écarts

**scannorder** : `GET /scannorder/{merchant_slug}/menu?order_type=...`
```go
func (h *Handler) GetMenu(w http.ResponseWriter, r *http.Request) {
	qr := chi.URLParam(r, "merchant_slug")
	orderType := r.URL.Query().Get("order_type")
	resp, err := h.service.GetMenu(ctx, qr, orderType)
	...
}
```
`order_type` est un paramètre **query**, optionnel, transmis tel quel (`""` possible) à
`ComputeGetMenu`, qui ne le valide ni ne lui donne de défaut — une chaîne vide ou inconnue tombe
dans le `default` du switch de prix (`cleanProductPricesForSNO`, prix "IN" appliqué).

**kiosk** : `GET /kiosk/menu?order_type=...`
```go
orderType := r.URL.Query().Get("order_type")
if orderType == "" {
	orderType = models.OrderTypeIn
}
resp, err := h.service.GetMenu(ctx, authenticatedKiosk.MerchantID, orderType)
```

**Écarts** :
1. **[MINEUR]** kiosk applique un défaut explicite (`models.OrderTypeIn`) côté handler, scannorder
   laisse la chaîne vide traverser jusqu'au service où elle tombe implicitement dans le même
   comportement par défaut ("IN"). Résultat fonctionnel identique, mais l'absence de symétrie rend
   la comparaison de comportement moins évidente à l'audit. Pas une vraie divergence de
   comportement — listé par souci d'exhaustivité (faux positif probable).
2. **[MAJEUR]** kiosk n'accepte **aucune valeur `DELIVERY`** pour `order_type` — son vocabulaire
   se limite à `IN`/`TAKE_AWAY` (le commentaire de `GetMenu` le documente explicitement : "le menu
   Kiosk n'a pas de notion de DELIVERY"). C'est **un choix produit volontaire et documenté**, pas
   un oubli — donc listé ici pour la traçabilité, mais **ne doit pas être corrigé** (le brief
   "order_type for kiosk" du dernier commit visait justement à brancher ce paramètre, pas à ajouter
   DELIVERY).
3. **[MINEUR]** scannorder logge l'appel (`log.Info("ScannOrder.GetMenu qr:..."`)) avant l'appel
   service ; kiosk ne logge l'entrée de `GetKioskMenu` (seulement les erreurs). Cohérence de
   logging à aligner si on veut un debug symétrique entre canaux, mais sans impact fonctionnel.
4. **[MINEUR]** kiosk supporte `If-None-Match` (ETag, 304) — fonctionnalité absente côté
   scannorder. Ce n'est pas un écart "à corriger côté kiosk" (c'est une amélioration kiosk-only,
   scannorder n'en a pas besoin pour son flux QR code à courte durée de vie).

### Réponse Product — écarts

**scannorder** (`models.ProductEntry`, nettoyé par `cleanProductForSNO`) — champ par champ
pertinents pour l'affichage menu après nettoyage :
```go
type ProductEntry struct {
	ProductID         string                 `json:"product_id"`
	Name              string                 `json:"name"`
	HasImage          bool                   `json:"has_image,omitempty"`
	ImageURL          *string                `json:"image_url,omitempty"`
	IsPopular         bool                   `json:"is_popular,omitempty"`
	Components        []ComponentUsage       `json:"components,omitempty"`
	Description       *string                `json:"description,omitempty"`
	Price             int64                  `json:"price"`
	PriceTakeAway     *int64                 `json:"price_take_away,omitempty"`
	PriceDelivery     *int64                 `json:"price_delivery,omitempty"`
	TVARate           *float64               `json:"tva_rate,omitempty"`
	CategoryID        *string                `json:"category_id,omitempty"`
	CategoryName      *string                `json:"category_name,omitempty"`
	Status            string                 `json:"status"`
	Configuration     ConfigurableResponse   `json:"configuration"`
	DisplayOrder      *int                   `json:"display_order"`
	Tags              []TagEntry             `json:"tags"`
	Allergens         []AllergenEntry        `json:"allergens"`
	Integrations      ProductIntegrations    `json:"integrations,omitempty"`
	// + ~20 autres champs (audit/lifecycle order, non pertinents pour un affichage menu :
	// OrderID, OrderItemID, ProductionStatus, Quantity, PaidQuantity, IsPaid, etc. — ProductEntry
	// est une struct générique partagée order+menu, scannorder ne filtre pas ces champs au JSON,
	// ils restent simplement à leur zero-value/omitempty).
}
```
scannorder **renvoie directement `models.ProductEntry`** (la struct partagée), juste nettoyée de
quelques champs internes via `cleanProductForSNO` (mise à `nil` de `BgColor`, `Category`, `TVAIn`,
`TVADelivery`, `TVATakeAway`, `IsAvailableOnSNO`, `IsProductGroup`, `SubProducts`, `SyncUberEats`,
`SyncDeliveroo`, `MerchantID`, `Available`, `AvailableIn`, `AvailableDelivery`,
`AvailableTakeAway`, `MarginPercent`, `FoodCostPercent`, `IsDistributed`, `ProductionColor`).
**IsPopular**, **HasImage**, **Status**, **TVARate**, **DisplayOrder**, **Integrations**,
**CategoryID/CategoryName** sont donc bien présents côté scannorder (ils ne sont pas dans la liste
de nettoyage).

**kiosk** (`KioskProduct`, struct dédiée — pas `models.ProductEntry` directement) :
```go
type KioskProduct struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Description      string               `json:"description,omitempty"`
	PriceCents       int64                `json:"price_cents"`
	ImageURL         string               `json:"image_url,omitempty"`
	Available        bool                 `json:"available"`
	AvailableOnKiosk bool                 `json:"available_on_kiosk"`
	Allergens        []string             `json:"allergens,omitempty"`
	Tags             []string             `json:"tags,omitempty"`
	ModifierGroups   []KioskModifierGroup `json:"modifier_groups,omitempty"`
}
```

**Écarts** :
5. **[MAJEUR]** **`is_popular` absent** côté `KioskProduct` — scannorder l'expose
   (`IsPopular bool`), kiosk ne le reprend nulle part dans `mapProductEntryToKioskProduct`. Si le
   Kiosk doit un jour afficher un badge "populaire" ou trier par popularité (cohérent avec le
   moteur upsell déjà branché côté Kiosk), le champ manque.
6. **[MAJEUR]** **`tva_rate` absent** — scannorder expose `TVARate *float64`, kiosk ne le renvoie
   jamais dans `KioskProduct`. Pour un kiosque qui affiche potentiellement un détail de prix
   TTC/HT à l'écran (ou pour un export comptable local), l'information n'est pas disponible sans
   second appel.
7. **[MINEUR]** **`category_id`/`category_name` sur le produit lui-même absents** — non bloquant
   car kiosk les porte déjà au niveau `KioskCategory` (voir section suivante), donc l'information
   existe, juste à un niveau de nesting différent (cohérent avec la structure category→products de
   kiosk).
8. **[MAJEUR]** **Nommage des champs prix incohérent** : scannorder expose `price` (int64, le
   nom de champ JSON brut de `ProductEntry`), kiosk expose `price_cents` (même valeur, nom
   différent). Ce n'est pas un bug fonctionnel (les deux représentent bien des centimes — voir
   `cleanProductPricesForKiosk`/`cleanProductPricesForSNO`, identiques en unité), mais c'est une
   incohérence de contrat d'API entre les deux canaux qui complique un éventuel client partagé
   (ex. composant Flutter réutilisé SNO+Kiosk).
9. **[MINEUR]** **`has_image` absent côté kiosk** — kiosk expose seulement `image_url` (chaîne vide
   si absente), scannorder expose en plus `has_image bool`. Information dérivable côté client
   (`image_url != ""`), donc impact très faible.
10. **[MINEUR]** **`integrations` (UberEats/Deliveroo enabled+price_override) absent côté
    kiosk** — non pertinent pour un device Kiosk (qui n'orchestre pas les marketplaces), absence
    volontaire et correcte, listé pour exhaustivité uniquement.

### Réponse Category — écarts

**scannorder** (`models.ProductCategory`, renvoyée telle quelle dans `MenuData.ProductTypes`) :
```go
type ProductCategory struct {
	Category     string         `json:"category"`
	CategoryName string         `json:"category_name"`
	CategoryID   *string        `json:"category_id"`
	Order        int            `json:"order"`
	BgColor      *string        `json:"bg_color,omitempty"`
	Available    bool           `json:"available"`
	Products     []ProductEntry `json:"products"`
}
```

**kiosk** (`KioskCategory`) :
```go
type KioskCategory struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	SortOrder int            `json:"sort_order"`
	ImageURL  string         `json:"image_url,omitempty"`
	Products  []KioskProduct `json:"products"`
}
```

**Écarts** :
11. **[MAJEUR]** **`available` (disponibilité de la catégorie) absent côté kiosk** — scannorder
    expose `Available bool` au niveau catégorie (alimenté par `menu.MenuService`, non visible dans
    ce fichier mais présent dans le struct partagé). Une catégorie peut être désactivée en bloc côté
    back-office (ex. "Desserts indisponibles ce midi") ; kiosk ne reprend jamais cette information,
    donc une catégorie marquée indisponible par le restaurateur **continue de s'afficher sur la
    borne** si elle contient au moins un produit `is_available_on_kiosk = TRUE` — un vrai écart de
    comportement, pas seulement de structure (`docs/KIOSK_DECISIONS.md` section B mentionne
    justement ce besoin de flag catégorie comme un "complément recommandé" jamais implémenté).
12. **[MINEUR]** `image_url` existe côté kiosk (`KioskCategory.ImageURL`) mais pas côté scannorder
    (`ProductCategory.BgColor` est une couleur, pas une image) — kiosk a un champ en plus, pas un
    écart à corriger (amélioration ou besoin spécifique borne).
13. **[MINEUR]** Nommage : `category`/`category_name`/`category_id`/`order` (scannorder) vs
    `id`/`name`/`sort_order` (kiosk) — kiosk n'a pas de second champ "code catégorie" distinct du
    nom (scannorder a `Category` ET `CategoryName`, deux champs différents — `Category` semble être
    un slug/code legacy, `CategoryName` le libellé). Si `Category` (le code) est utilisé ailleurs
    (filtrage, i18n), kiosk n'a pas d'équivalent.

### Réponse Options/Attributs — écarts

**scannorder** — réutilise directement `models.ConfigurableResponse` / `ConfigurableAttribute` /
`ConfigurableOption` (via `ProductEntry.Configuration`, non nettoyé par `cleanProductForSNO`) :
```go
type ConfigurableResponse struct {
	Attributes []ConfigurableAttribute `json:"attributes"`
}
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

**kiosk** (`KioskModifierGroup` / `KioskModifierOption`, mappées dans
`mapProductEntryToKioskProduct`) :
```go
type KioskModifierGroup struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Min      int                   `json:"min"`
	Max      int                   `json:"max"`
	Required bool                  `json:"required"`
	Options  []KioskModifierOption `json:"options"`
}
type KioskModifierOption struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PriceDeltaCents int    `json:"price_delta_cents"`
}
```

**Écarts** :
14. **[MAJEUR]** **`max_quantity` absent côté `KioskModifierOption`** — scannorder expose
    `MaxQuantity int` (quantité max sélectionnable pour une option, ex. "jusqu'à 3 suppléments
    fromage"). Sans ce champ, l'écran Kiosk ne peut pas savoir qu'une option supporte une quantité
    > 1 — il ne peut proposer qu'une sélection binaire (cochée/non cochée), ce qui est un écart de
    **fonctionnalité produit**, pas seulement de structure : un restaurateur configurant "max 3" sur
    une option verra son réglage ignoré côté borne.
15. **[MINEUR]** `attribute_type` absent côté `KioskModifierGroup` — scannorder l'expose
    (`AttributeType string`, probablement "single"/"multiple"/"radio"/"checkbox" ou similaire selon
    la convention du module menu). kiosk dérive `Required` depuis `MinOptions > 0` mais ne propage
    pas le type brut, ce qui peut suffire si `Min`/`Max` couvrent déjà tous les cas d'affichage
    (1 option = radio, plusieurs = checkbox), à confirmer selon le front Kiosk.
16. **[MINEUR]** `Selected`/`Quantity` (état de sélection courant) n'a pas d'équivalent côté
    `KioskModifierOption` — cohérent, car côté menu/catalogue (avant ajout panier) ces champs n'ont
    pas de sens ; ils ne concernent que la relecture d'une commande déjà passée
    (`ProductEntry.Configuration` réutilisée aussi côté commande). Pas un écart réel.

### Ce qui est identique (pour ne pas y toucher)

- `models.ProductEntry`, `models.ProductCategory`, `models.ConfigurableResponse`,
  `models.ConfigurableAttribute`, `models.ConfigurableOption`, `models.AllergenEntry`,
  `models.TagEntry` : structs **partagées** via `internal/models/menu_models.go`, déjà cohérentes
  par construction — kiosk les consomme en entrée (`menuService.GetMenuFromMerchantIdWithMarketing`,
  `GetProductFromMerchantId`) avant de les remapper vers ses propres structs JSON ; aucun écart à
  corriger ici, c'est la source commune des deux canaux.
- Le mécanisme de "flatten" groupe→sous-produits (`scannorder.ComputeGetMenu` /
  `kiosk.flattenKioskProducts`) suit la même logique (groupe indisponible → on remonte les
  sous-produits disponibles), seule la colonne de disponibilité change
  (`is_available_on_sno` vs `is_available_on_kiosk`) — déjà cohérent par conception, pas un écart.
- Le calcul du prix par type de commande (`cleanProductPricesForSNO` vs
  `cleanProductPricesForKiosk`) repose sur les mêmes champs partagés
  (`ProductEntry.Price`/`PriceTakeAway`/`PriceDelivery`) — kiosk omet juste la branche `DELIVERY`,
  ce qui est voulu (voir écart 2, non corrigible).

---

## Flux 2 — Pricing

### Request body — écarts

**scannorder** : `POST /scannorder/{merchant_slug}/pricing`, body = `models.PricingRequest`
(struct **partagée**, voir `internal/models/request_objects.go`) :
```go
type PricingRequest struct {
	MerchantID                  string        `json:"merchant_id"`
	Order                       *OrderRequest `json:"order"`
	DiscountCode                string        `json:"discount_code,omitempty"`
	IsSNO                       bool          `json:"is_sno,omitempty"`
	QRCode           string `json:"qr_code,omitempty"`
	IsInDeliveryZone bool   `json:"is_in_delivery_zone,omitempty"`
	CheckoutSessionType string       `json:"checkout_session_type,omitempty"`
	Merchant            *MerchantRow `json:"merchant,omitempty"`
	// + champs internes json:"-" (DayOfWeek, Time, IsOrderable, ...)
}
```
Le client envoie en réalité un `OrderRequest` complet (avec `Products []OrderProductPayload`,
`Customer`, `OrderType`, etc. — voir Flux 3) ; `merchant_slug` (QR) est injecté côté handler dans
`req.QRCode`.

**kiosk** : `POST /kiosk/pricing`, body = `KioskPricingRequest` (struct **dédiée**) :
```go
type KioskPricingRequest struct {
	FulfillmentType string             `json:"fulfillment_type"`
	Items           []KioskPricingItem `json:"items"`
	DiscountCode    *string            `json:"discount_code,omitempty"`
}
type KioskPricingItem struct {
	ProductID         string   `json:"product_id"`
	Quantity          int      `json:"quantity"`
	SelectedOptionIDs []string `json:"selected_option_ids,omitempty"`
	Notes             string   `json:"notes,omitempty"`
}
```

**Écarts** :
1. **[MAJEUR]** **Forme du payload totalement différente** : scannorder demande au client
   d'envoyer un `OrderRequest` complet et imbriqué (avec tous les champs de commande, même si la
   majorité sont ignorés/recalculés), kiosk a une forme **simplifiée et dédiée** (`items` à plat,
   `fulfillment_type` au lieu de `order.order_type`). Ce n'est **pas un écart à corriger** — c'est un
   choix d'API plus propre côté kiosk, scannorder portant une dette historique (le payload
   `PricingRequest` est réutilisé tel quel pour 3 usages différents : preview, création de
   commande, et structure interne). À ne pas aligner kiosk vers ce legacy.
2. **[MINEUR]** `discount_code` : `string` simple côté scannorder (`PricingRequest.DiscountCode`),
   `*string` (pointeur, nilable) côté kiosk (`KioskPricingRequest.DiscountCode`) — différence de
   nullabilité sans impact observable (les deux distinguent "pas de code" de "code vide" de façon
   équivalente côté Go), mais kiosk gère mieux l'absence explicite (pas de différence à corriger,
   au contraire kiosk est plus rigoureux ici).
3. **[MINEUR]** Pas de `customer` dans `KioskPricingRequest` — cohérent, le Kiosk n'a pas de
   notion de client identifié pour une simple preview de prix (le client n'existe qu'à la création
   de commande, et même là, pas de `customer` explicite — voir Flux 3).

### Response body — écarts

**scannorder** : renvoie directement `*models.PricingResponse` (struct partagée, voir
`internal/models/request_objects.go`) :
```go
type PricingResponse struct {
	Status       string          `json:"status"`
	OrderRequest *PricingRequest `json:"order_request"`
	UnavailableProduct []UnavailableProductInfo `json:"unavailable_products"`
	EstimatedDistributionTime int `json:"estimated_distribution_time"`
	MinimumCartForDeliveryOrder float64 `json:"minimum_cart_for_delivery_order,omitempty"`
	IsOrderable                 bool    `json:"is_orderable"`
	NotOrderableReason          string  `json:"not_orderable_reason,omitempty"`
	AppliedDiscounts []string `json:"applied_discounts,omitempty"`
}
```
Le détail prix par ligne (`TTC`/`TVA`/`HT`, produits avec prix/extra/options recalculés) est donc
imbriqué dans `OrderRequest.Order` (le `OrderRequest` complet, recalculé), pas dans une structure
de synthèse dédiée.

**kiosk** : renvoie `*KioskPricingResponse` (struct dédiée, synthèse uniquement) :
```go
type KioskPricingResponse struct {
	ItemsTotalCents int64 `json:"items_total_cents"`
	DiscountCents   int64 `json:"discount_cents"`
	TaxCents        int64 `json:"tax_cents"`
	TotalCents      int64 `json:"total_cents"`
}
```

**Écarts** :
4. **[MAJEUR]** **Aucun détail par ligne produit côté kiosk** — scannorder renvoie le détail
   produit par produit (prix unitaire recalculé, extras, options sélectionnées avec leur prix
   officiel) dans `order_request.order.products[]` ; kiosk ne renvoie qu'un total agrégé
   (`items_total_cents`/`tax_cents`/`total_cents`). Si l'écran Kiosk doit afficher un récapitulatif
   panier détaillé (prix par article avec options choisies), il devra **soit** reconstituer ce
   détail côté client à partir de ce qu'il a déjà envoyé (risque de désynchronisation avec le prix
   serveur réellement appliqué, notamment sur les promotions), **soit** ce champ doit être ajouté
   côté kiosk. À ne pas dupliquer le `OrderRequest` complet historique de scannorder, mais une
   liste `items: [{product_id, unit_price_cents, options_total_cents, line_total_cents}]` serait
   l'équivalent propre.
5. **[MINEUR]** `unavailable_products`, `is_orderable`, `not_orderable_reason` n'ont pas
   d'équivalent côté `KioskPricingResponse` — `ComputePricing` (kiosk) traite un statut différent
   de `"success"` comme une erreur générique (`models.ErrInvalidInput`), perdant ainsi le détail
   structuré (quel produit est indisponible, pourquoi) que scannorder propage au client. Impact
   UX : le Kiosk ne peut pas afficher "le produit X n'est plus disponible", seulement une erreur
   générique.
6. **[MINEUR]** `applied_discounts` (liste des codes/promos effectivement appliqués) absent côté
   kiosk — `discount_cents` est calculé par soustraction (`itemsTotal - totalCents`, voir code),
   sans distinguer remise automatique vs code promo vs reward. Suffisant pour un total affiché,
   insuffisant pour un détail "Promotion Happy Hour : -2,00€" séparé d'un "Code promo SUMMER10".

### Logique de calcul — écarts

**scannorder** (`GetPricingSNO`) délègue à **`orders.OrdersService.ComputePricing`** après sa
propre validation anti-fraude :
```go
func (s *Service) GetPricingSNO(ctx context.Context, req *models.PricingRequest) (*models.PricingResponse, error) {
	merchant, err := s.repo.GetMerchantByQR(ctx, req.QRCode)
	...
	if err := s.validateAndCleanPricingPayload(ctx, req, merchant); err != nil {
		...
		return &models.PricingResponse{Status: "pricing_validation_failed"}, nil
	}
	...
	pricing, err := s.orderingService.ComputePricing(ctx, req)
	...
	return pricing, err
}
```

**kiosk** (`ComputePricing` → `computeOrderPricing` → `buildOrderProducts`) délègue **également** à
**`orders.OrdersService.ComputePricing`** (le même service, pas une réimplémentation) :
```go
func (s *Service) computeOrderPricing(ctx context.Context, merchantID, fulfillmentType string, items []KioskOrderItem, discountCode *string) (*models.PricingResponse, error) {
	orderType, err := kioskFulfillmentToOrderType(fulfillmentType)
	...
	products, err := s.buildOrderProducts(ctx, merchantID, items)
	...
	pricingReq := &models.PricingRequest{
		MerchantID: merchantID,
		Order: &models.OrderRequest{OrderType: orderType, Products: products},
	}
	...
	return s.ordersService.ComputePricing(ctx, pricingReq)
}
```

**Écarts** :
7. **[MAJEUR]** **Mécanisme anti-fraude différent dans sa forme, équivalent dans son effet, mais
   avec un trou de couverture** : scannorder a une fonction dédiée
   (`validateAndCleanPricingPayload`) qui (a) vérifie l'existence de **chaque `product_id`** ET
   **chaque `option_id`** envoyé, en échouant explicitement si l'un d'eux est inconnu
   (`fmt.Errorf("invalid_product_id: %s", ...)` / `invalid_option_id`), (b) écrase ensuite tous les
   prix avec les valeurs officielles. kiosk fait l'équivalent du (a) pour les **produits**
   (`GetAvailableKioskProductIDs`, échoue avec `models.ErrKioskProductUnavailable` si un produit est
   absent/masqué) et pour les **options** (`GetExistingConfigurationOptionIDs`, échoue avec
   `models.ErrInvalidInput` si une option est inconnue) — donc la couverture est en réalité
   **complète des deux côtés**, mais le (b) n'est **jamais fait explicitement côté kiosk** : kiosk
   ne renseigne aucun prix dans `OrderProductPayload` (`Price` reste à zero-value), comptant
   entièrement sur `orders.OrdersService.buildSelectedProducts`/`applyConfigurationOptionPrices`
   pour les recalculer depuis la base. C'est **fonctionnellement sûr** (le doc du module le
   justifie explicitement : "ComputePricing ignore déjà entièrement les prix envoyés par le
   client"), mais ce n'est **pas le même mécanisme** que scannorder (qui écrase un prix
   potentiellement falsifié plutôt que de ne jamais le lire) — à documenter comme un choix
   d'implémentation différent mais équivalent en sécurité, **pas un écart à corriger**.
8. **[MINEUR]** scannorder vérifie la zone de livraison (`CustomerInDeliveryZone`) dans
   `GetPricingSNO` — sans objet côté kiosk (pas de notion `DELIVERY`), absence cohérente, pas un
   écart.
9. **[MINEUR]** kiosk traduit `fulfillment_type` (`DINE_IN`/`TAKE_AWAY`) en `order_type`
   (`IN`/`TAKE_AWAY`) via `kioskFulfillmentToOrderType` avant d'appeler `ComputePricing` ;
   scannorder reçoit déjà `order_type` dans le vocabulaire interne directement depuis le client
   (`IN`/`DELIVERY`/`TAKE_AWAY`). Différence de contrat d'API uniquement (kiosk expose un
   vocabulaire stable `DINE_IN`/`TAKE_AWAY` côté client, traduit en interne) — choix délibéré et
   documenté, pas un écart à corriger.

### Ce qui est identique

- **Le moteur de calcul est strictement le même** : les deux canaux appellent
  `orders.OrdersService.ComputePricing(ctx, *models.PricingRequest)` — aucune réimplémentation de
  calcul de prix/TVA/remise côté kiosk. C'est le point le plus important à préserver : ne **jamais**
  faire diverger ce point d'entrée.
- `models.OrderProductPayload`, `models.ProductConfiguration`, `models.ConfigurationAttribute`,
  `models.ConfigurationOption`, `models.OrderWithoutPayload` : structs partagées utilisées à
  l'identique par les deux canaux pour construire la requête envoyée à `ComputePricing` — déjà
  cohérent via struct partagée, ne pas dupliquer.
- Le principe "jamais faire confiance au prix envoyé par le client" est respecté des deux côtés
  (juste avec une implémentation différente, voir écart 7), conformément à la règle absolue
  documentée dans `docs/ARCHITECTURE_API.md` §7.5/§11.1.

---

## Flux 3 — Création de commande

### Request body — écarts

**scannorder** : `POST /scannorder/{merchant_slug}/orders`, body = `models.PricingRequest` (même
struct que pour le pricing — voir Flux 2), avec `req.Order.OrderType` parmi `IN`/`DELIVERY`/
`TAKE_AWAY`, `req.Order.Customer` (obligatoire pour DELIVERY/TAKE_AWAY), `req.Order.Products`.

**kiosk** : `POST /kiosk/orders`, body = `CreateKioskOrderRequest` (struct dédiée) :
```go
type CreateKioskOrderRequest struct {
	FulfillmentType string           `json:"fulfillment_type"` // DINE_IN | TAKE_AWAY
	IdempotencyKey  string           `json:"idempotency_key"`
	Items           []KioskOrderItem `json:"items"`
	PaymentMethod   string           `json:"payment_method"` // pay_at_counter uniquement
	DiscountCode    *string          `json:"discount_code,omitempty"`
	OrderNotes      string           `json:"order_notes,omitempty"`
}
type KioskOrderItem struct {
	ProductID           string   `json:"product_id"`
	Quantity            int      `json:"quantity"`
	SelectedOptionIDs   []string `json:"selected_option_ids,omitempty"`
	Notes               string   `json:"notes,omitempty"`
	WithoutComponentIDs []string `json:"without_component_ids,omitempty"`
}
```

**Écarts** :
1. **[MINEUR]** Forme dédiée et simplifiée côté kiosk (cohérent avec Flux 2, choix volontaire — pas
   un écart à corriger, voir Flux 2 écart 1).
2. **[MAJEUR]** **Pas de `customer` côté kiosk** — aucun moyen d'associer un client identifié
   (téléphone, nom) à une commande Kiosk. Pour `TAKE_AWAY`, scannorder résout/crée un client par
   téléphone (`GetCustomerByPhone`) ; kiosk n'a aucun champ équivalent dans
   `CreateKioskOrderRequest`. Si le besoin produit est "appeler le client quand sa commande est
   prête" ou "cumuler des points de fidélité", c'est un manque fonctionnel réel, pas juste
   structurel — mais peut être un choix produit volontaire (commande borne = anonyme par défaut) à
   confirmer plutôt qu'un bug.
3. **[MINEUR]** `idempotency_key` n'a pas d'équivalent côté scannorder (`PricingRequest` n'a aucun
   champ d'idempotence) — amélioration kiosk-only, pas un écart à corriger côté kiosk (au contraire,
   scannorder pourrait s'en inspirer, hors scope ici).

### Response body — écarts

**scannorder** : renvoie `models.CreateOrderResult` (struct partagée) :
```go
type CreateOrderResult struct {
	Message         string             `json:"message,omitempty"`
	Status          string             `json:"status"`
	OrderID         string             `json:"order_id,omitempty"`
	OrderNum        *string            `json:"order_num,omitempty"`
	Action          string             `json:"action,omitempty"`
	CheckoutSession *WRCheckoutSession `json:"checkout_session,omitempty"`
}
```

**kiosk** : renvoie `CreateKioskOrderResponse` (struct dédiée) :
```go
type CreateKioskOrderResponse struct {
	OrderID       string `json:"order_id"`
	DisplayNumber string `json:"display_number"`
	Status        string `json:"status"`
	TotalCents    int64  `json:"total_cents"`
}
```

**Écarts** :
4. **[MINEUR]** Pas de `checkout_session` côté kiosk — cohérent, le paiement en ligne Stripe
   Checkout n'est pas (encore) implémenté pour Kiosk (`payment_method: pay_at_counter` uniquement,
   voir `docs/KIOSK_DECISIONS.md` G.3, point ouvert). Absence volontaire, pas un écart.
5. **[MAJEUR]** **`status` renvoyé incohérent avec l'état réel créé en base** — directement lié à
   l'écart bloquant ci-dessous : `CreateKioskOrderResponse.Status` vaut systématiquement
   `"pending_counter_payment"` (en dur, ligne service), alors que la commande vient d'être créée
   avec `MerchantApproval = "ACCEPTED"` (voir écart bloquant). Le client Kiosk reçoit donc un
   statut qui ne correspond pas à ce qui est réellement stocké en base.

### Mapping items — écarts

**scannorder** : pas de mapping à proprement parler — le client envoie déjà des
`models.OrderProductPayload` complets dans `req.Order.Products` (forme native attendue par
`orders.OrdersService`/`order_life_cycle`), `validateAndCleanPricingPayload` se contente d'écraser
les prix.

**kiosk** (`buildOrderProducts`) traduit `KioskOrderItem` → `models.OrderProductPayload` :
```go
payloads = append(payloads, models.OrderProductPayload{
	ProductID: item.ProductID,
	Quantity:  item.Quantity,
	Config:    config, // construit depuis SelectedOptionIDs, ID attribut fixe "kiosk-options"
	Comment:   comment, // depuis item.Notes, UserID = kioskCreatedBy
	Without:   without, // depuis WithoutComponentIDs
})
```

**Écarts** :
6. **[MAJEUR]** **`ConfigurationAttribute.ID` codé en dur à `"kiosk-options"`** — scannorder (via le
   client qui envoie directement le payload `OrderRequest`) transmet le **véritable** `attribute_id`
   de chaque groupe d'options tel que configuré en base
   (`ConfigurableAttribute.ID`/`ConfigAttributeID`). kiosk, lui, regroupe **toutes** les options
   sélectionnées (quel que soit leur groupe d'attribut réel) sous un unique attribut fictif
   `"kiosk-options"`. Tant que `orders.OrdersService.applyConfigurationOptionPrices` ne fait que
   relire le prix par `option.ID` (sans vérifier la cohérence groupe/option), cela reste
   fonctionnellement correct pour le calcul de prix — mais si un futur traitement (audit, ticket
   cuisine détaillé par groupe d'attribut, validation `min/max` côté serveur) se base sur
   `ConfigurationAttribute.ID` pour regrouper l'affichage par modificateur (ex. "Taille : Grande",
   "Suppléments : Bacon, Fromage"), le ticket Kiosk perdra ce regroupement, contrairement à une
   commande scannorder.
7. **[MINEUR]** **Aucune validation `min`/`max` par groupe côté kiosk** — `buildOrderProducts` ne
   vérifie que l'existence de chaque `option_id` (pas le respect de `ConfigurableAttribute.MinOptions`/
   `MaxOptions` du produit, ex. "choisir exactement 1 taille"). scannorder ne le fait pas non plus
   explicitement dans `validateAndCleanPricingPayload` (la validation min/max n'est documentée nulle
   part comme faite côté serveur pour aucun des deux canaux) — donc **pas un écart entre les deux
   canaux**, mais un trou de validation potentiellement partagé, hors scope de cette comparaison
   (aucune correction kiosk-only ne le résoudrait puisque scannorder ne le fait pas non plus).

### Appel `ordersLifeCycleService` — écarts

**scannorder** (`CreateOrderSNO`) :
```go
order.CreatedBy = &ScannorderOwner // "SCANNORDER"
order.IsSNO = true
order.Payments = []models.PaymentPayload{}
order.CashRegisterId = &ScannorderOwner

newOrder, err := s.orderLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{
	MerchantID: orderReq.Merchant.MerchantID,
	Order:      *orderReq.Order,
})
```
`order.MerchantApproval` est déjà fixé **avant** l'appel pricing, dans le switch sur `OrderType`
(`"ACCEPTED"` pour `IN`, `"PENDING_APPROVAL"` pour `DELIVERY`/`TAKE_AWAY`) — voir Flux 1 du document
ARCHITECTURE_API.md §7.2 et le code source : ce champ est porté dans `req.Order` (le `OrderRequest`
construit côté client, repris tel quel par `GetPricingSNO`/`ComputePricing` qui ne le touchent pas)
**avant** l'appel à `CreateOrder`, donc cohérent avec le type de commande choisi par le client.

**kiosk** (`CreateKioskOrder`) :
```go
orderReq := pricing.OrderRequest.Order
orderReq.OrderType = orderType
orderReq.MerchantApproval = "ACCEPTED"   // <-- voir écart bloquant
orderReq.CreatedBy = strPtr(kioskCreatedBy)     // "KIOSK"
orderReq.CashRegisterId = strPtr(kioskCashRegister)
orderReq.OnlinePayment = false
orderReq.Payments = []models.PaymentPayload{}

newOrder, err := s.ordersLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{
	MerchantID: kiosk.MerchantID,
	Order:      *orderReq,
})
```

**Écarts** :
8. **[NON-BLOQUANT — requalifié, voir note en tête de document]** `orderReq.MerchantApproval =
   "ACCEPTED"` semblait à l'époque de l'audit contredire la conception documentée du module
   (commentaire de `CreateKioskOrder` lui-même : "MerchantApproval reste PENDING_APPROVAL jusqu'à
   ce que le staff encaisse au comptoir"). Conséquences en cascade qui avaient motivé la
   qualification initiale (conservées pour mémoire, plus considérées comme un problème réel — les
   commandes Kiosk aboutissent de toute façon à `ACCEPTED`) :
   - `ConfirmCounterPayment` ne fait "pas de transition d'état" (par design, car il s'attend à ce
     que la commande soit déjà `PENDING_APPROVAL` à encaisser) — mais avec `ACCEPTED` déjà posé à la
     création, la commande part directement en cuisine/KDS comme acceptée et payée, sans qu'aucun
     paiement comptoir n'ait eu lieu.
   - `CancelKioskOrder` vérifie explicitement `order.MerchantApproval != "PENDING_APPROVAL"` →
     `models.ErrKioskOrderNotCancellable` : avec `ACCEPTED` posé dès la création, **toute commande
     Kiosk devient immédiatement non annulable**, y compris dans les premières secondes après
     création (alors que `scannorder.CancelOrderSNO` autorise l'annulation dans les 60 premières
     secondes même pour des commandes `IN` déjà `ACCEPTED`).
   - Le statut renvoyé au client (`"pending_counter_payment"`, en dur dans la réponse) ne
     correspond donc plus à l'état réel stocké en base (`ACCEPTED`), ce qui peut tromper l'écran
     Kiosk et le back-office (qui pourrait, par exemple, filtrer ses listes "à encaisser" sur
     `MerchantApproval = "PENDING_APPROVAL"` et ne jamais voir ces commandes apparaître).
9. **[MAJEUR]** `order.IsSNO` (scannorder le force à `true`) — kiosk n'a pas d'équivalent
   `order.IsKiosk` ou similaire sur `models.OrderRequest`/`models.Order` (le champ n'existe
   simplement pas dans la struct partagée pour "Kiosk"). Le marquage du canal Kiosk se fait
   uniquement via `CreatedBy = "KIOSK"` + `CashRegisterId = "KIOSK_CASH_REGISTER"` (constantes non
   lues dans le détail mais référencées) + `orders.kiosk_id` (colonne dédiée, posée après coup via
   `s.repo.SetKioskIDOnOrder`). C'est cohérent avec la décision documentée dans
   `KIOSK_DECISIONS.md` section D ("Option 1 minimale") — **pas un bug**, mais à signaler car ça
   diffère structurellement du pattern booléen dédié `IsSNO` que scannorder utilise.
10. **[MINEUR]** `OnlinePayment` : scannorder le force à `true` pour `DELIVERY`/`TAKE_AWAY` (paiement
    en ligne obligatoire) ; kiosk le force à `false` (paiement comptoir uniquement, cohérent avec le
    mode actuellement supporté `pay_at_counter`). Différence cohérente avec l'absence de paiement
    carte Kiosk (point ouvert G.3), pas un écart à corriger maintenant.

### Ce qui est identique

- **Les deux canaux appellent le même point d'entrée** :
  `s.ordersLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{MerchantID, Order})` — aucune
  réimplémentation de la création de commande. C'est la règle la plus importante du projet
  (`docs/ARCHITECTURE_API.md` §11.2 anti-pattern "ne jamais contourner OrdersLifeCycleService") et
  elle est respectée des deux côtés.
- `models.RequestObject`, `models.OrderRequest`, `models.OrderProductPayload`,
  `models.PaymentPayload` : structs partagées, déjà cohérentes par construction — aucun écart à
  corriger sur ces types eux-mêmes, seulement sur **la façon dont leurs champs sont remplis** (voir
  écarts 6, 8, 9 ci-dessus).
- `CancelKioskOrder` réutilise `OrdersLifeCycleService.DeleteOrder` (pas `SetOrderDeleted`, qui
  exige un utilisateur humain) — choix correct et documenté, cohérent avec le fait qu'un device
  Kiosk n'a pas d'utilisateur authentifié de type `auth.UserLoginRow`.
- Le mécanisme `order_notes`/`without_component_ids` → `models.OrderRequest.Comment` /
  `models.OrderWithoutPayload` réutilise des champs déjà consommés par POS/ScanNOrder — pas une
  nouvelle interface, déjà cohérent via struct partagée.

---

## Plan de correction proposé

> Rappel : **toutes** les corrections ci-dessous s'appliquent exclusivement à
> `internal/modules/kiosk/`. `internal/modules/scannorder/` n'est jamais modifié.

### Ancienne "correction bloquante" — requalifiée non-bloquante, traitée à titre informatif

> Cette section décrit le correctif qui avait été envisagé lors de l'audit initial. Comme indiqué
> en tête de document, la sévérité "bloquant" était une erreur d'appréciation : le comportement de
> `CreateKioskOrder` n'est pas un bug, c'est l'un des rares choix volontairement différents entre
> Kiosk et ScanNOrder. Le correctif décrit ci-dessous a néanmoins été appliqué dans
> `internal/modules/kiosk/service.go` (voir `docs/KIOSK_DECISIONS.md`, Incrément 11, Correction 1) —
> conservé ici pour la traçabilité historique de l'audit, pas comme une action restante.

1. **Fichier** : `internal/modules/kiosk/service.go`, fonction `CreateKioskOrder`.
   **Modification appliquée** : `orderReq.MerchantApproval = "PENDING_APPROVAL"` à la création
   (`DINE_IN` comme `TAKE_AWAY`), avec transition vers `ACCEPTED` faite par
   `ConfirmCounterPayment` (`OrdersLifeCycleService.SetOrderAccepted`) au moment de l'encaissement
   comptoir réel. Cohérent avec :
   - Le commentaire de la fonction elle-même.
   - `ConfirmCounterPayment` (transition explicite désormais en place).
   - `CancelKioskOrder` (qui exige `MerchantApproval == "PENDING_APPROVAL"` pour autoriser
     l'annulation).
   - Le statut renvoyé au client (`Status: "pending_counter_payment"` dans
     `CreateKioskOrderResponse`).

### Corrections majeures — Flux 1 (Menu)

2. **Fichier** : `internal/modules/kiosk/models.go`, struct `KioskProduct`.
   **Modification** : ajouter `IsPopular bool `json:"is_popular,omitempty"``.
   **Fichier** : `internal/modules/kiosk/service.go`, fonction `mapProductEntryToKioskProduct`.
   **Modification** : ajouter `IsPopular: p.IsPopular,` dans le `KioskProduct{...}` retourné.

3. **Fichier** : `internal/modules/kiosk/models.go`, struct `KioskProduct`.
   **Modification** : ajouter `TVARate *float64 `json:"tva_rate,omitempty"``.
   **Fichier** : `internal/modules/kiosk/service.go`, fonction `mapProductEntryToKioskProduct`.
   **Modification** : ajouter `TVARate: p.TVARate,` dans le `KioskProduct{...}` retourné.

4. **Fichier** : `internal/modules/kiosk/models.go`, struct `KioskModifierOption`.
   **Modification** : ajouter `MaxQuantity int `json:"max_quantity,omitempty"``.
   **Fichier** : `internal/modules/kiosk/service.go`, fonction `mapProductEntryToKioskProduct`
   (boucle `for _, opt := range attr.Options`).
   **Modification** : ajouter `MaxQuantity: opt.MaxQuantity,` dans le `KioskModifierOption{...}`.

5. **Fichier** : `internal/modules/kiosk/models.go`, struct `KioskCategory`.
   **Modification** : ajouter `Available bool `json:"available"``.
   **Fichier** : `internal/modules/kiosk/service.go`, fonction `GetMenu` (boucle
   `for _, pt := range rawMenu.ProductsTypes`).
   **Modification** : ajouter `Available: pt.Available,` dans le `KioskCategory{...}` construit ;
   envisager de filtrer (`continue`) les catégories avec `Available == false` plutôt que de
   simplement exposer le champ, selon le comportement souhaité côté affichage borne (à confirmer :
   masquer entièrement, ou afficher grisé).

### Corrections majeures — Flux 2 (Pricing)

6. **Fichier** : `internal/modules/kiosk/models.go`, struct `KioskPricingResponse`.
   **Modification** : ajouter un champ détail par ligne, ex. :
   ```go
   type KioskPricingLineItem struct {
       ProductID      string `json:"product_id"`
       Quantity       int    `json:"quantity"`
       UnitPriceCents int64  `json:"unit_price_cents"`
       LineTotalCents int64  `json:"line_total_cents"`
   }
   ```
   et `Items []KioskPricingLineItem `json:"items"`` dans `KioskPricingResponse`.
   **Fichier** : `internal/modules/kiosk/service.go`, fonction `pricingResponseToKiosk`.
   **Modification** : peupler `Items` depuis `pricing.OrderRequest.Order.Products` (même boucle
   existante qui calcule déjà `itemsTotal`, juste accumuler aussi par ligne au lieu de sommer
   seulement le total).

7. **Fichier** : `internal/modules/kiosk/models.go`, struct `KioskPricingResponse`.
   **Modification** : ajouter `UnavailableProductIDs []string `json:"unavailable_product_ids,omitempty"``.
   **Fichier** : `internal/modules/kiosk/service.go`, fonction `ComputePricing`.
   **Modification** : avant de retourner `models.ErrInvalidInput` quand `pricing.Status != "success"`,
   inspecter `pricing.UnavailableProduct` et le propager dans la réponse d'erreur ou dans un statut
   structuré plutôt qu'une erreur générique opaque.

### Corrections majeures — Flux 3 (Création de commande)

8. **Fichier** : `internal/modules/kiosk/service.go`, fonction `buildOrderProducts`.
   **Modification** : au lieu de regrouper toutes les options sélectionnées sous un unique attribut
   fictif `"kiosk-options"`, résoudre le véritable `ConfigurableAttribute.ID` propriétaire de chaque
   `option_id` (déjà disponible via `s.menuService.GetProductFromMerchantId` →
   `product.Configuration.Attributes[].Options[].ID`, ou ajouter une méthode repository dédiée type
   `GetOptionToAttributeMap(ctx, optionIDs)`), puis construire un `models.ConfigurationAttribute`
   par groupe réel au lieu d'un seul groupe fourre-tout. Impact : alignement avec ce que produit un
   client scannorder pour la même structure, nécessaire si un futur ticket cuisine/back-office
   détaillé par groupe d'attribut doit fonctionner identiquement pour les deux canaux.

9. **Point ouvert produit (pas une simple correction de code)** : si le besoin business de
   recontacter le client (notification "commande prête") ou de cumuler des points de fidélité sur
   une commande Kiosk est confirmé, ajouter un champ optionnel `CustomerPhone *string
   `json:"customer_phone,omitempty"`` à `CreateKioskOrderRequest`, et dans
   `internal/modules/kiosk/service.go` (`CreateKioskOrder`), résoudre/créer le client par téléphone
   de la même façon que `scannorder.Service.CreateOrderSNO` (cas `TAKE_AWAY`,
   `s.repo.GetCustomerByPhone`) — **à valider avec le produit avant implémentation**, ce n'est pas
   un bug mais une absence de fonctionnalité potentiellement volontaire.

### Écarts volontaires à ne PAS corriger (liste de garde-fou)

- Absence de `DELIVERY` dans `order_type`/`fulfillment_type` Kiosk (Flux 1, écart 2) : voulu.
- Forme de payload simplifiée (`items`/`fulfillment_type`) au lieu du `PricingRequest`/`OrderRequest`
  legacy complet (Flux 2 écart 1, Flux 3 écart 1) : amélioration délibérée, ne pas régresser vers
  le format scannorder.
- Validation anti-fraude implémentée différemment (amont vs nettoyage après coup) mais
  fonctionnellement complète des deux côtés (Flux 2, écart 7) : pas de changement nécessaire.
- Absence de `checkout_session` / paiement carte Kiosk (Flux 3, écart 4) : dépend du point ouvert
  G.3 de `KIOSK_DECISIONS.md`, hors scope de cette comparaison de structs.
- `order.IsKiosk` inexistant, marquage via `CreatedBy`/`CashRegisterId`/`kiosk_id` uniquement
  (Flux 3, écart 9) : décision déjà actée (`KIOSK_DECISIONS.md` section D, Option 1), à ne réviser
  que dans le cadre d'un refactor transverse `orders.channel`, pas isolément pour Kiosk.

---

## Structs de référence scannorder

> Copie intégrale des structs scannorder pertinentes pour les trois flux — la référence que kiosk
> doit reproduire (champ par champ) chaque fois qu'un écart "à corriger" ci-dessus le requiert.

### Flux 1 — Menu

```go
// internal/modules/scannorder/models.go
type MenuResponse struct {
	Status string    `json:"status"`
	Menu   *MenuData `json:"menu,omitempty"`
	Error  string    `json:"error,omitempty"`
}

type MenuData struct {
	OrderType       string                   `json:"order_type"`
	ProductTypes    []models.ProductCategory `json:"products_types"`
	LoyaltyPrograms []LoyaltyProgram         `json:"loyalty_programs,omitempty"`
	Discounts       []Discount               `json:"discounts,omitempty"`
}
```

```go
// internal/models/menu_models.go — struct PARTAGÉE, consommée par les deux canaux
type ProductCategory struct {
	Category     string         `json:"category"`
	CategoryName string         `json:"category_name"`
	CategoryID   *string        `json:"category_id"`
	Order        int            `json:"order"`
	BgColor      *string        `json:"bg_color,omitempty"`
	Available    bool           `json:"available"`
	Products     []ProductEntry `json:"products"`
}

type ProductEntry struct {
	ProductID         string                 `json:"product_id"`
	Name              string                 `json:"name"`
	HasImage          bool                   `json:"has_image,omitempty"`
	ImageURL          *string                `json:"image_url,omitempty"`
	IsPopular         bool                   `json:"is_popular,omitempty"`
	IsAvailableOnSNO  *bool                  `json:"is_available_on_sno,omitempty"`
	Components        []ComponentUsage       `json:"components,omitempty"`
	Description       *string                `json:"description,omitempty"`
	Price             int64                  `json:"price"`
	PriceTakeAway     *int64                 `json:"price_take_away,omitempty"`
	PriceDelivery     *int64                 `json:"price_delivery,omitempty"`
	TVARate           *float64               `json:"tva_rate,omitempty"`
	CategoryID        *string                `json:"category_id,omitempty"`
	CategoryName      *string                `json:"category_name,omitempty"`
	IsProductGroup    *bool                  `json:"is_product_group,omitempty"`
	Status            string                 `json:"status"`
	SubProducts       []ProductEntry         `json:"sub_products,omitempty"`
	Configuration     ConfigurableResponse   `json:"configuration"`
	DisplayOrder      *int                   `json:"display_order"`
	SyncDeliveroo     *bool                  `json:"sync_deliveroo,omitempty"`
	SyncUberEats      *bool                  `json:"sync_ubereats,omitempty"`
	Tags              []TagEntry             `json:"tags"`
	Allergens         []AllergenEntry        `json:"allergens"`
	Integrations      ProductIntegrations    `json:"integrations,omitempty"`
}

type ConfigurableResponse struct {
	Attributes []ConfigurableAttribute `json:"attributes"`
}

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

```go
// internal/modules/scannorder/service.go — nettoyage appliqué avant envoi au client
func (s *Service) cleanProductForSNO(product *models.ProductEntry, deliveryType string) {
	s.cleanProductPricesForSNO(product, deliveryType)

	product.BgColor = nil
	product.Category = nil
	product.TVAIn = nil
	product.TVADelivery = nil
	product.TVATakeAway = nil
	product.IsAvailableOnSNO = nil
	product.IsProductGroup = nil
	product.SubProducts = nil
	product.SyncUberEats = nil
	product.SyncDeliveroo = nil
	product.MerchantID = nil
	product.Available = nil
	product.AvailableIn = nil
	product.AvailableDelivery = nil
	product.AvailableTakeAway = nil
	product.MarginPercent = nil
	product.FoodCostPercent = nil
	product.IsDistributed = nil
	product.ProductionColor = nil
}
```

### Flux 2 — Pricing

```go
// internal/models/request_objects.go — struct PARTAGÉE
type PricingRequest struct {
	MerchantID                  string        `json:"merchant_id"`
	Order                       *OrderRequest `json:"order"`
	DiscountCode                string        `json:"discount_code,omitempty"`
	IsSNO                       bool          `json:"is_sno,omitempty"`
	QRCode           string `json:"qr_code,omitempty"`
	IsInDeliveryZone bool   `json:"is_in_delivery_zone,omitempty"`
	CheckoutSessionType string       `json:"checkout_session_type,omitempty"`
	Merchant            *MerchantRow `json:"merchant,omitempty"`
}

type PricingResponse struct {
	Status       string          `json:"status"`
	OrderRequest *PricingRequest `json:"order_request"`
	UnavailableProduct []UnavailableProductInfo `json:"unavailable_products"`
	EstimatedDistributionTime int `json:"estimated_distribution_time"`
	MinimumCartForDeliveryOrder float64 `json:"minimum_cart_for_delivery_order,omitempty"`
	IsOrderable                 bool    `json:"is_orderable"`
	NotOrderableReason          string  `json:"not_orderable_reason,omitempty"`
	AppliedDiscounts []string `json:"applied_discounts,omitempty"`
}
```

```go
// internal/modules/scannorder/service.go — validation anti-fraude
func (s *Service) validateAndCleanPricingPayload(ctx context.Context, req *models.PricingRequest, merchant *models.MerchantRow) error {
	// 1. Collecte des product_id / option_id du payload client.
	// 2. Récupération des prix officiels (GetProductPricesForSNO, GetConfigurationOptionPricesForSNO).
	// 3. Si un ID envoyé n'existe pas en base -> erreur explicite (tentative de fraude potentielle).
	// 4. Écrasement des prix du payload avec les valeurs officielles avant tout calcul.
	...
}
```

### Flux 3 — Création de commande

```go
// internal/models/create_order_models.go / request_objects.go — structs PARTAGÉES
type RequestObject struct {
	MerchantID         string       `json:"merchant_id"`
	DeviceID           *string      `json:"device_id"`
	UpsellSuggestionID *string      `json:"upsell_suggestion_id,omitempty"`
	Order              OrderRequest `json:"order"`
}

type OrderRequest struct {
	OrderType        string                `json:"order_type"`
	Products         []OrderProductPayload `json:"products"`
	Customer         *CustomerRequest      `json:"customer"`
	CreatedBy        *string               `json:"created_by"`
	Comment          *string               `json:"comment"`
	Payments         []PaymentPayload      `json:"payments"`
	MerchantApproval string                `json:"merchant_approval"`
	BrandStatus      string                `json:"brand_status"`
	OnlinePayment    bool                  `json:"online_payment"`
	IsSNO            bool                  `json:"is_sno"`
	CashRegisterId   *string               `json:"cash_register_id,omitempty"`
	TTC              int                   `json:"TTC"`
	TVA              int                   `json:"TVA"`
	HT               int                   `json:"HT"`
}

type OrderProductPayload struct {
	ProductID   string                   `json:"product_id"`
	Quantity    int                      `json:"quantity"`
	Price       int                      `json:"price"`
	ProductName string                   `json:"product_name"`
	TvaRate     float64                  `json:"tva_rate"`
	Extra       []*OrderExtraPayload     `json:"extra"`
	Without     []*OrderWithoutPayload   `json:"without"`
	Config      *ProductConfiguration    `json:"configuration"`
	Comment     *OrderItemCommentPayload `json:"comment"`
}

type CreateOrderResult struct {
	Message         string             `json:"message,omitempty"`
	Status          string             `json:"status"`
	OrderID         string             `json:"order_id,omitempty"`
	OrderNum        *string            `json:"order_num,omitempty"`
	Action          string             `json:"action,omitempty"`
	CheckoutSession *WRCheckoutSession `json:"checkout_session,omitempty"`
}
```

```go
// internal/modules/scannorder/service.go — extrait CreateOrderSNO, fixation MerchantApproval
// AVANT l'appel pricing/CreateOrder, selon le type de commande :
switch orderType {
case "IN":
	order.MerchantApproval = "ACCEPTED"
	order.BrandStatus = "PENDING"
case "DELIVERY":
	order.OnlinePayment = true
	order.MerchantApproval = "PENDING_APPROVAL"
	fallthrough
case "TAKE_AWAY":
	order.OnlinePayment = true
	order.MerchantApproval = "PENDING_APPROVAL"
}

// ... après pricing recalculé ...
var ScannorderOwner = "SCANNORDER"
order.CreatedBy = &ScannorderOwner
order.IsSNO = true
order.Payments = []models.PaymentPayload{}
order.CashRegisterId = &ScannorderOwner

newOrder, err := s.orderLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{
	MerchantID: orderReq.Merchant.MerchantID,
	Order:      *orderReq.Order,
})
```
