package menu

import (
	"time"
	"welloresto-api/internal/models"
)

// Top-level response
type MenuResponse struct {
	Status          string              `json:"status"`
	LastMenuUpdate  *time.Time          `json:"last_menu_update"` // will be marshalled like "2006-01-02 15:04:05"
	ProductsTypes   []ProductCategory   `json:"products_types"`   // same as products_types in old API
	ComponentsTypes []ComponentCategory `json:"components_types"`
	Delays          []DelayEntry        `json:"delays"`
}

// product category (type)
type ProductCategory struct {
	Category   string         `json:"category"`
	CategoryID *string        `json:"category_id"`
	Order      int            `json:"order"`
	BgColor    *string        `json:"bg_color,omitempty"`
	ImageURL   *string        `json:"image_url,omitempty"`
	Products   []ProductEntry `json:"products"`
}

// product
type ProductEntry struct {
	OrderID                      string                     `json:"order_id,omitempty"`
	OrderItemID                  string                     `json:"order_item_id"`
	ProductID                    string                     `json:"product_id"`
	MerchantID                   *string                    `json:"merchantID"`
	OrderedOn                    *time.Time                 `json:"ordered_on,omitempty"`
	ProductionStatus             string                     `json:"production_status,omitempty"`
	ProductionStatusDoneQuantity int                        `json:"production_status_done_quantity,omitempty"`
	Name                         string                     `json:"name"`
	HasImage                     bool                       `json:"has_image,omitempty"`
	ByProductOf                  *string                    `json:"by_product_of,omitempty"`
	ImageURL                     *string                    `json:"image_url,omitempty"`
	IsPopular                    bool                       `json:"is_popular,omitempty"`
	EligibleForUpsales           bool                       `json:"eligible_for_upsales,omitempty"`
	IsAvailableOnSNO             bool                       `json:"is_available_on_sno,omitempty"`
	Components                   []ComponentUsage           `json:"components,omitempty"`
	Allergens                    []models.AllergenEntry     `json:"allergens,omitempty"`
	Tags                         []models.TagEntry          `json:"tags,omitempty"`
	Description                  *string                    `json:"description,omitempty"`
	Price                        int64                      `json:"price"`
	PriceTakeAway                int64                      `json:"price_take_away"`
	PriceDelivery                int64                      `json:"price_delivery"`
	TVAIn                        float64                    `json:"tva_rate_in,omitempty"`
	TVADelivery                  float64                    `json:"tva_rate_delivery,omitempty"`
	TVATakeAway                  float64                    `json:"tva_rate_take_away,omitempty"`
	AvailableIn                  bool                       `json:"available_in,omitempty"`
	AvailableTakeAway            bool                       `json:"available_take_away,omitempty"`
	AvailableDelivery            bool                       `json:"available_delivery,omitempty"`
	Category                     *string                    `json:"category"` // To Be Deleted
	CategoryID                   *string                    `json:"category_id"`
	IsProductGroup               bool                       `json:"is_product_group"`
	BgColor                      *string                    `json:"bg_color,omitempty"`
	Status                       int                        `json:"status"`
	SubProducts                  []ProductEntry             `json:"sub_products"`
	Configuration                ConfigurableResponse       `json:"configuration"`
	Quantity                     int                        `json:"quantity"`
	PaidQuantity                 int                        `json:"paid_quantity"`
	DistributedQuantity          int                        `json:"distributed_quantity"`
	ReadyForDistributionQuantity int                        `json:"ready_for_distribution_quantity"`
	IsPaid                       int                        `json:"isPaid"`
	IsDistributed                int                        `json:"isDistributed"`
	DiscountID                   *int64                     `json:"discount_id"`
	DiscountName                 *string                    `json:"discount_name"`
	DiscountedPrice              *int64                     `json:"discounted_price"`
	ProductionColor              *string                    `json:"production_color"`
	Extra                        []OrderProductExtra        `json:"extra"`
	Without                      []OrderProductWithout      `json:"without"`
	Customers                    []interface{}              `json:"customers"` // keep generic as original
	Comment                      models.OrderComment        `json:"comment"`
	DisplayOrder                 *int                       `json:"display_order"`
	Integrations                 models.ProductIntegrations `json:"integrations,omitempty"`
}

type OrderProductExtra struct {
	ID          string  `json:"id"`
	OrderItemID string  `json:"order_item_id"`
	OrderID     string  `json:"order_id"`
	ProductID   string  `json:"product_id"`
	Name        string  `json:"name"`
	ComponentID string  `json:"component_id"`
	Price       float64 `json:"price"`
}

type OrderProductWithout struct {
	ID          string `json:"id"`
	OrderItemID string `json:"order_item_id"`
	OrderID     string `json:"order_id"`
	ProductID   string `json:"product_id"`
	Name        string `json:"name"`
	ComponentID string `json:"component_id"`
	Price       int64  `json:"price"`
}

// components required
type ComponentUsage struct {
	ComponentID   int64   `json:"component_id"`
	ProductID     string  `json:"product_id,omitempty"`
	Name          string  `json:"name"`
	Price         int64   `json:"price"`
	Status        string  `json:"status"` // textuel, aligné sur models.ComponentUsage
	Quantity      float64 `json:"quantity"`
	UnitOfMeasure string  `json:"unit_of_measure"`
}

// component category
type ComponentCategory struct {
	Category   string           `json:"category"`
	Order      int              `json:"order"`
	Components []ComponentBasic `json:"components"`
}

type ComponentBasic struct {
	ComponentID      int64   `json:"component_id"`
	Name             string  `json:"name"`
	Category         *string `json:"category"`
	Price            int     `json:"price"`
	Status           int     `json:"status"`
	UnitOfMeasureID  string  `json:"unit_of_measure_id,omitempty"`
	UnitOfMeasure    string  `json:"unit_of_measure,omitempty"`
	PurchasePrice    *int    `json:"purchase_price,omitempty"`
	PurchasePriceQty *int    `json:"purchase_price_qty,omitempty"`
}

// configurable attributes
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
	Selected          int    `json:"selected"`
}

// delays
type DelayEntry struct {
	DelayID          int64  `json:"delay_id"`
	ShortDescription string `json:"short_description"`
	Duration         int    `json:"duration"`
}

type AvailabilityRequest struct {
	Status string `json:"status"` // "1" or "0"
}

type AvailabilityResponse struct {
	Status  string `json:"status"`
	Updated int64  `json:"updated"`
}

type Unit struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	ShortName   string           `json:"short_name,omitempty"`
	Conversions []UnitConversion `json:"conversions"`
}

type UnitConversion struct {
	ToUnitID        string  `json:"to_unit_id"`
	ToUnitName      string  `json:"to_unit_name"`
	ToUnitShortName string  `json:"to_unit_short_name,omitempty"`
	Multiplier      float64 `json:"multiplier"`
}

// Temporaire, à remplacer par les vraies struct
type Attribute struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`  // attribute_type
	Name         string            `json:"name"`  // name
	Title        string            `json:"title"` // title
	Min          int               `json:"min"`   // min_options
	Max          int               `json:"max"`   // max_options
	Options      []AttributeOption `json:"options"`
	ProductCount int               `json:"product_count"` // Nombre de produits utilisant cet attribut
}

type AttributeOption struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Price           int      `json:"price"`        // extra_price (idéalement stocké en centimes)
	MaxQuantity     int      `json:"max_quantity"` // max_quantity
	Enabled         bool     `json:"enabled"`      // enabled
	ImageURL        *string  `json:"image_url,omitempty"`
	ComponentID     *string  `json:"component_id,omitempty"`       // ingrédient (components.component_id) lié, nil = aucun
	Quantity        *float64 `json:"quantity,omitempty"`           // quantité consommée par sélection ; nil si ComponentID est nil
	UnitOfMeasureID *string  `json:"unit_of_measure_id,omitempty"` // unit_of_measure.id de la quantité ; nil si ComponentID est nil
}

type CreateProductPayload struct {
	MerchantID          string  `json:"merchant_id"`
	Name                string  `json:"name"`
	ProductDesc         string  `json:"description"`
	Price               float64 `json:"price"`
	PriceTakeAway       float64 `json:"price_take_away"`
	PriceDelivery       float64 `json:"price_delivery"`
	TvaInID             string  `json:"tva_in_id"`
	TvaDeliveryID       string  `json:"tva_delivery_id"`
	TvaTakeAwayID       string  `json:"tva_take_away_id"`
	CategoryID          string  `json:"category_id"`
	IsProductGroup      bool    `json:"is_product_group"`
	MarketingCategoryID *string `json:"marketing_category_id,omitempty"`

	// Champs facultatifs permettant de créer un produit complet en un seul
	// appel (fiche produit du back-office : le sheet de création expose les
	// mêmes onglets que l'édition). Omis = valeur par défaut de la colonne
	// pour les scalaires, association vide pour les listes. L'ensemble est
	// persisté dans la même transaction que l'INSERT products : un échec sur
	// une association ne laisse aucun produit partiel en base.
	BgColor           *string                    `json:"bg_color,omitempty"`
	ProductionColor   *string                    `json:"production_color,omitempty"`
	Status            *string                    `json:"status,omitempty"`
	IsAvailableOnSno  *bool                      `json:"is_available_on_sno,omitempty"`
	AvailableIn       *bool                      `json:"available_in,omitempty"`
	AvailableTakeAway *bool                      `json:"available_take_away,omitempty"`
	AvailableDelivery *bool                      `json:"available_delivery,omitempty"`
	Configuration     []string                 `json:"configuration,omitempty"` // IDs d'attributs configurables
	Components        []ProductComponentUpdate `json:"components,omitempty"`    // Composition (ingrédient + quantité + unité)
	Tags              []string                 `json:"tags,omitempty"`          // IDs de tags
	Allergens         []string                 `json:"allergens,omitempty"`     // IDs d'allergènes

	// Pointeur volontairement : sync_uber_eats et sync_deliveroo valent TRUE par
	// défaut en base. Avec une valeur non-pointeur, « désactivé » et « champ
	// absent » seraient indistinguables et un produit créé avec les plateformes
	// désactivées ressortirait actif. nil = garder les défauts, non-nil = la
	// valeur des deux canaux fait foi.
	Integrations *models.ProductIntegrations `json:"integrations,omitempty"`
}

// ProductComponentUpdate pour mettre à jour les composants d'un produit
type ProductComponentUpdate struct {
	ComponentID string  `json:"component_id"`       // ID du composant
	Quantity    float64 `json:"quantity"`           // Quantité requise
	UnitID      string  `json:"unit_of_measure_id"` // ID de l'unité de mesure
}

// ProductUpdatePayload correspond aux champs de la table 'products' + associations
type ProductUpdatePayload struct {
	Name              *string                    `json:"name"` // Pointeurs pour gérer le NULL/Omission
	Description       *string                    `json:"description"`
	IsAvailableOnSno  *bool                      `json:"is_available_on_sno"`
	CategoryID        *string                    `json:"category_id"`
	Price             *int                       `json:"price"`
	PriceTakeAway     *int                       `json:"price_take_away"`
	PriceDelivery     *int                       `json:"price_delivery"`
	AvailableIn       *bool                      `json:"available_in"`
	AvailableTakeAway *bool                      `json:"available_take_away"`
	AvailableDelivery *bool                      `json:"available_delivery"`
	ByProductOf       *string                    `json:"by_product_of"` // Peut être null
	BgColor           *string                    `json:"bg_color"`
	ProductionColor   *string                    `json:"production_color"`
	Enabled           *bool                      `json:"enabled"`
	Status            *string                    `json:"status"`
	Configuration     []string                   `json:"configuration"` // Liste des IDs d'attributs configurables
	Components        []ProductComponentUpdate   `json:"components"`    // Liste des composants avec quantity et unit_id
	Tags              []string                   `json:"tags"`          // Liste des IDs de tags
	Allergens         []string                   `json:"allergens"`     // Liste des IDs d'allergènes
	Integrations      models.ProductIntegrations `json:"integrations"`  // Liste des intégrations à synchroniser (ex: "uber_eats", "deliveroo")

	// TVA modifiable après création : sans ces champs une erreur de saisie à la
	// création restait définitive, la fiche produit n'ayant aucun autre moyen
	// de corriger le taux. Mêmes IDs que CreateProductPayload (tva_categories).
	TvaInID       *string `json:"tva_in_id"`
	TvaTakeAwayID *string `json:"tva_take_away_id"`
	TvaDeliveryID *string `json:"tva_delivery_id"`
}

// ProductAttributesPayload pour la configuration des attributs
type ProductAttributesPayload struct {
	Configuration []string `json:"configuration"` // Liste des ID d'attributs
}

// UpdateComponentPayload pour la mise à jour de composants
type UpdateComponentPayload struct {
	Name             *string  `json:"name"`              // Nom du composant (optionnel)
	Price            *int     `json:"price"`             // Prix de vente en centimes (optionnel)
	PurchaseCost     *int     `json:"purchase_cost"`     // Coût d'achat en centimes (optionnel)
	PurchaseUnitID   *string  `json:"purchase_unit_id"`  // ID unité de mesure d'achat (optionnel)
	PurchaseCostQty  *float64 `json:"purchase_cost_qty"` // Quantité pour le coût d'achat (optionnel)
	MerchantID       string   `json:"-"`                 // ID du marchand, à récupérer du token
	CategoryID       *string  `json:"category_id"`       // ID de la catégorie
	UnitID           *string  `json:"unit_id"`           // ID de l'unité de mesure
	ConservationDays *int     `json:"conservation_days"` // Durée de conservation après ouverture, en jours (optionnel)
	ConservationType *string  `json:"conservation_type"` // Type de conservation: froid, congele, sec, ambiant (optionnel)
	StorageTempMin   *float64 `json:"storage_temp_min"`  // Température min de stockage en °C (optionnel)
	StorageTempMax   *float64 `json:"storage_temp_max"`  // Température max de stockage en °C (optionnel)
}

// UpsertComponentCategoryPayload pour la création de catégories de composants
type UpsertComponentCategoryPayload struct {
	Name       string `json:"name"` // Nom de la catégorie
	MerchantID string `json:"-"`    // Sera défini par le service
}

// UpdateComponentCategoryPayload pour le renommage d'une catégorie de composants
type UpdateComponentCategoryPayload struct {
	Name *string `json:"name"` // Pointeur : distingue l'omission d'une valeur vide
}

// UpdateComponentCategoriesDisplayOrderPayload pour le réordonnancement
type UpdateComponentCategoriesDisplayOrderPayload struct {
	CategoryIDs []string `json:"category_ids"`
}

// CreateProductCategoryPayload pour la création de catégories de produits
type CreateProductCategoryPayload struct {
	Name       string `json:"name"` // Nom de la catégorie
	MerchantID string `json:"-"`    // Sera défini par le service
}

// DisplayOrderItem represents a category with its ordered products
type DisplayOrderItem struct {
	CategoryID string   `json:"category_id"`
	Products   []string `json:"products"`
}

// DisplayOrderPayload is used to update category and product display orders
type DisplayOrderPayload struct {
	DisplayOrder []DisplayOrderItem `json:"display_order"`
}

// UpdateAttributeOptionPayload represents an option in the update attribute payload
type UpdateAttributeOptionPayload struct {
	ID          *string `json:"id"`           // Optional: if not provided, option will be created
	Title       string  `json:"title"`        // Title of the option
	Price       int     `json:"price"`        // Price in cents
	MaxQuantity *int    `json:"max_quantity"` // Max quantity per option
	Enabled     *bool   `json:"enabled"`      // Whether the option is enabled
	ExtraPrice  *int    `json:"extra_price"`  // Extra price (deprecated, use Price)
	ImageURL    *string `json:"image_url"`    // Image URL (managed primarily via the dedicated upload endpoint)

	// Lien ingrédient. Non-pointeurs volontairement : le back-office envoie
	// toujours la valeur complète actuelle de l'option à chaque sauvegarde
	// (UpdateAttribute désactive toutes les options existantes puis réinsère
	// chaque option du payload — ce n'est pas un patch partiel), comme
	// Title/Price/MaxQuantity sur ce même struct. "" = aucun ingrédient lié.
	ComponentID     string  `json:"component_id"`       // Ingrédient (components.component_id) lié. "" = aucun ingrédient.
	Quantity        float64 `json:"quantity"`           // Quantité consommée. Ignorée (stockée NULL) si ComponentID est "".
	UnitOfMeasureID string  `json:"unit_of_measure_id"` // unit_of_measure.id de Quantity. Ignorée (stockée NULL) si ComponentID est "".
}

// UpdateAttributePayload for updating configurable attributes
type UpdateAttributePayload struct {
	Type    string                         `json:"type"`    // attribute_type (e.g., "CHECK", "RADIO")
	Name    string                         `json:"name"`    // Name of the attribute
	Title   string                         `json:"title"`   // Display title
	Min     int                            `json:"min"`     // Minimum options to select
	Max     int                            `json:"max"`     // Maximum options to select
	Options []UpdateAttributeOptionPayload `json:"options"` // Array of options
}

// BulkAssignProductsPayload is the standard payload for bulk assigning products to a resource
type BulkAssignProductsPayload struct {
	ProductIDs []string `json:"product_ids"` // Product IDs to assign
}

// BulkUpdateProductPrice represents a single product price update
type BulkUpdateProductPrice struct {
	ProductID      string `json:"product_id"`      // Product ID to update
	Price          *int   `json:"price"`           // Base price (optional)
	PriceTakeAway  *int   `json:"price_take_away"` // Take away price (optional)
	PriceDelivery  *int   `json:"price_delivery"`  // Delivery price (optional)
	PriceUberEats  *int   `json:"price_uber_eats"` // Uber Eats price override (optional)
	PriceDeliveroo *int   `json:"price_deliveroo"` // Deliveroo price override (optional)
}

// BulkUpdateProductPricesPayload for bulk updating product prices
type BulkUpdateProductPricesPayload struct {
	Products []BulkUpdateProductPrice `json:"products"` // Array of products with prices to update
}

// BulkProductsPayload est le corps commun des actions de groupe qui ne portent
// que sur une liste de produits (suppression pour l'instant).
type BulkProductsPayload struct {
	ProductIDs []string `json:"product_ids"` // Produits ciblés par l'action
}

// BulkSetProductsStatusPayload — passage en masse d'un statut de vente.
// Status reprend les valeurs textuelles de la colonne products.status
// ('available', 'not_available', 'out_of_stock', 'removed_from_menu').
type BulkSetProductsStatusPayload struct {
	ProductIDs []string `json:"product_ids"`
	Status     string   `json:"status"`
}

// BulkSetProductsAttributesPayload — remplace la configuration (groupes
// d'options/suppléments) de chaque produit ciblé par la liste fournie.
// Une liste vide est valide : elle retire toutes les options.
type BulkSetProductsAttributesPayload struct {
	ProductIDs    []string `json:"product_ids"`
	Configuration []string `json:"configuration"`
}

type MarketingCategoryEntry struct {
	CategoryID   string   `json:"category_id"`
	Name         string   `json:"name"`
	DisplayOrder int      `json:"display_order"`
	ImageURL     *string  `json:"image_url,omitempty"`
	Available    bool     `json:"available"`
	ProductCount int      `json:"product_count"`
	ProductIDs   []string `json:"product_ids"`
}

type CreateMarketingCategoryPayload struct {
	Name string `json:"name"`
}

type UpdateMarketingCategoryPayload struct {
	Name      *string `json:"name"`
	Available *bool   `json:"available"`
}

type UpdateMarketingCategoriesDisplayOrderPayload struct {
	CategoryIDs []string `json:"category_ids"`
}

type AssignProductMarketingCategoryPayload struct {
	CategoryID string `json:"category_id"`
}
