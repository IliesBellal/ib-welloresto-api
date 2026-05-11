package models

// Top-level response
type MenuResponse struct {
	Status          string              `json:"status"`
	LastMenuUpdate  *int                `json:"last_menu_update"`
	ProductsTypes   []ProductCategory   `json:"products_types,omitempty"`
	ComponentsTypes []ComponentCategory `json:"components_types,omitempty"`
	Delays          []DelayEntry        `json:"delays,omitempty"`
}

// product category (type)
type ProductCategory struct {
	Category     string         `json:"category"`
	CategoryName string         `json:"category_name"`
	CategoryID   *string        `json:"category_id"`
	Order        int            `json:"order"`
	BgColor      *string        `json:"bg_color,omitempty"`
	Available    bool           `json:"available"`
	Products     []ProductEntry `json:"products"`
}

// product
type ProductEntry struct {
	OrderID                      string                 `json:"order_id,omitempty"`
	OrderItemID                  string                 `json:"order_item_id,omitempty"`
	ProductID                    string                 `json:"product_id"`
	MerchantID                   *string                `json:"merchant_id,omitempty"`
	OrderedOn                    int64                  `json:"ordered_on,omitempty"`
	ProductionStatus             string                 `json:"production_status,omitempty"`
	ProductionStatusDoneQuantity int                    `json:"production_status_done_quantity,omitempty"`
	Name                         string                 `json:"name"`
	HasImage                     bool                   `json:"has_image,omitempty"`
	ByProductOf                  *string                `json:"by_product_of,omitempty"`
	ImageURL                     *string                `json:"image_url,omitempty"`
	IsPopular                    bool                   `json:"is_popular,omitempty"`
	IsAvailableOnSNO             *bool                  `json:"is_available_on_sno,omitempty"`
	Available                    *bool                  `json:"available,omitempty"`
	Components                   []ComponentUsage       `json:"components,omitempty"`
	Description                  *string                `json:"description,omitempty"`
	Price                        int64                  `json:"price"`
	PriceTakeAway                *int64                 `json:"price_take_away,omitempty"`
	PriceDelivery                *int64                 `json:"price_delivery,omitempty"`
	PriceUberEats                *int64                 `json:"price_uber_eats,omitempty"`
	PriceDeliveroo               *int64                 `json:"price_deliveroo,omitempty"`
	TVARate                      *float64               `json:"tva_rate,omitempty"`
	TVAIn                        *float64               `json:"tva_rate_in,omitempty"`
	TVADelivery                  *float64               `json:"tva_rate_delivery,omitempty"`
	TVATakeAway                  *float64               `json:"tva_rate_take_away,omitempty"`
	CostPrice                    *float64               `json:"cost_price,omitempty"`
	FoodCostPercent              *float64               `json:"foodcost_percent,omitempty"`
	MarginPercent                *float64               `json:"margin_percent,omitempty"`
	AvailableIn                  *bool                  `json:"available_in,omitempty"`
	AvailableTakeAway            *bool                  `json:"available_take_away,omitempty"`
	AvailableDelivery            *bool                  `json:"available_delivery,omitempty"`
	Category                     *string                `json:"category,omitempty"` // To Be Deleted
	CategoryID                   *string                `json:"category_id,omitempty"`
	CategoryName                 *string                `json:"category_name,omitempty"`
	IsProductGroup               *bool                  `json:"is_product_group,omitempty"`
	BgColor                      *string                `json:"bg_color,omitempty"`
	Status                       string                 `json:"status"`
	SubProducts                  []ProductEntry         `json:"sub_products,omitempty"`
	Configuration                ConfigurableResponse   `json:"configuration"`
	Quantity                     *int                   `json:"quantity,omitempty"`
	PaidQuantity                 *int                   `json:"paid_quantity,omitempty"`
	DistributedQuantity          *int                   `json:"distributed_quantity,omitempty"`
	ReadyForDistributionQuantity *int                   `json:"ready_for_distribution_quantity,omitempty"`
	IsPaid                       *bool                  `json:"isPaid,omitempty"`
	IsDistributed                *bool                  `json:"isDistributed,omitempty"`
	DiscountID                   *int64                 `json:"discount_id,omitempty"`
	DiscountName                 *string                `json:"discount_name,omitempty"`
	DiscountedPrice              *int64                 `json:"discounted_price,omitempty"`
	ProductionColor              *string                `json:"production_color,omitempty"`
	Extra                        *[]OrderProductExtra   `json:"extra,omitempty"`
	Without                      *[]OrderProductWithout `json:"without,omitempty"`
	Comment                      *OrderComment          `json:"comment,omitempty"`
	DisplayOrder                 *int                   `json:"display_order"`
	SyncDeliveroo                *bool                  `json:"sync_deliveroo,omitempty"`
	SyncUberEats                 *bool                  `json:"sync_ubereats,omitempty"`
	Tags                         []TagEntry             `json:"tags"`
	Allergens                    []AllergenEntry        `json:"allergens"`
	Integrations                 ProductIntegrations    `json:"integrations,omitempty"`
}

type ProductIntegrations struct {
	UberEats  ProductIntegrationItem `json:"uber_eats"`
	Deliveroo ProductIntegrationItem `json:"deliveroo"`
}

type ProductIntegrationItem struct {
	Enabled       bool `json:"enabled"`
	PriceOverride *int `json:"price_override,omitempty"` // Permet de spécifier un prix différent pour l'intégration
}

// AllergenEntry represents one of the 14 EU regulated allergens.
type AllergenEntry struct {
	ID    string `json:"allergen_id"`
	Name  string `json:"name"`
	Code  string `json:"code"`
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
}

// TagEntry represents a merchant-specific label attached to a product.
type TagEntry struct {
	ID           string `json:"id"`
	MerchantID   string `json:"merchant_id,omitempty"`
	Name         string `json:"name"`
	Color        string `json:"color,omitempty"`
	DisplayOrder int    `json:"display_order"`
	ProductCount int    `json:"product_count"`
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
	ComponentID     string   `json:"component_id"`
	ProductID       string   `json:"product_id,omitempty"`
	Name            string   `json:"name"`
	Price           int64    `json:"price,omitempty"`
	Status          int      `json:"status,omitempty"`
	Quantity        *float64 `json:"quantity,omitempty"`
	UnitOfMeasure   string   `json:"unit_of_measure"`
	UnitOfMeasureID string   `json:"unit_of_measure_id,omitempty"`
	Cost            float64  `json:"cost,omitempty"`
}

// component category
type ComponentCategory struct {
	CategoryID   string           `json:"category_id"`
	CategoryName string           `json:"category_name"`
	Category     string           `json:"category,omitempty"`
	Order        int              `json:"order"`
	Components   []ComponentBasic `json:"components"`
}

type ComponentBasic struct {
	ComponentID             string   `json:"component_id"`
	Name                    string   `json:"name"`
	Category                *string  `json:"category"`
	Price                   int      `json:"price"`
	Status                  string   `json:"status"`
	Cost                    int64    `json:"cost,omitempty"`
	UnitOfMeasure           string   `json:"unit_of_measure,omitempty"`
	UnitOfMeasureID         string   `json:"unit_of_measure_id,omitempty"`
	UnitOfMeasureShortName  string   `json:"unit_of_measure_short_name,omitempty"`
	PurchasePrice           *int     `json:"purchase_price,omitempty"`
	PurchasePriceQty        *float64 `json:"purchase_price_qty,omitempty"`
	PurchasePricePerUnit    *float64 `json:"purchase_price_per_unit,omitempty"`
	PurchaseUnitOfMeasureID string   `json:"purchase_unit_of_measure_id,omitempty"`
	PurchaseUnitOfMeasure   string   `json:"purchase_unit_of_measure,omitempty"`
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
	Selected          bool   `json:"selected"`
}

// delays
type DelayEntry struct {
	DelayID          int64  `json:"delay_id"`
	ShortDescription string `json:"short_description"`
	Duration         int    `json:"duration"`
}

type StatusRequest struct {
	Status string `json:"status"` // "available" or "unavailable"
}

type AvailabilityResponse struct {
	Status  string `json:"status"`
	Updated int64  `json:"updated"`
}
