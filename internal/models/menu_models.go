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
	Products     []ProductEntry `json:"products"`
}

// product
type ProductEntry struct {
	OrderID                      string                `json:"order_id,omitempty"`
	OrderItemID                  string                `json:"order_item_id"`
	ProductID                    string                `json:"product_id"`
	OrderedOn                    int64                 `json:"ordered_on,omitempty"`
	ProductionStatus             string                `json:"production_status,omitempty"`
	ProductionStatusDoneQuantity int                   `json:"production_status_done_quantity,omitempty"`
	Name                         string                `json:"name"`
	HasImage                     bool                  `json:"has_image,omitempty"`
	ByProductOf                  *string               `json:"by_product_of,omitempty"`
	ImageURL                     *string               `json:"image_url,omitempty"`
	IsPopular                    bool                  `json:"is_popular,omitempty"`
	IsAvailableOnSNO             bool                  `json:"is_available_on_sno,omitempty"`
	Components                   []ComponentUsage      `json:"components,omitempty"`
	Description                  *string               `json:"description,omitempty"`
	Price                        int64                 `json:"price"`
	PriceTakeAway                *int64                `json:"price_take_away"`
	PriceDelivery                *int64                `json:"price_delivery"`
	TVAIn                        *float64              `json:"tva_rate_in,omitempty"`
	TVADelivery                  *float64              `json:"tva_rate_delivery,omitempty"`
	TVATakeAway                  *float64              `json:"tva_rate_take_away,omitempty"`
	AvailableIn                  bool                  `json:"available_in"`
	AvailableTakeAway            bool                  `json:"available_take_away"`
	AvailableDelivery            bool                  `json:"available_delivery"`
	Category                     *string               `json:"category"` // To Be Deleted
	CategoryID                   *string               `json:"category_id"`
	IsProductGroup               bool                  `json:"is_product_group"`
	BgColor                      *string               `json:"bg_color,omitempty"`
	Status                       string                `json:"status"`
	SubProducts                  []ProductEntry        `json:"sub_products"`
	Configuration                ConfigurableResponse  `json:"configuration"`
	Quantity                     int                   `json:"quantity"`
	PaidQuantity                 int                   `json:"paid_quantity"`
	DistributedQuantity          int                   `json:"distributed_quantity"`
	ReadyForDistributionQuantity int                   `json:"ready_for_distribution_quantity"`
	IsPaid                       int                   `json:"isPaid"`
	IsDistributed                int                   `json:"isDistributed"`
	DiscountID                   *int64                `json:"discount_id"`
	DiscountName                 *string               `json:"discount_name"`
	DiscountedPrice              *int64                `json:"discounted_price"`
	ProductionColor              *string               `json:"production_color"`
	Extra                        []OrderProductExtra   `json:"extra"`
	Without                      []OrderProductWithout `json:"without"`
	Customers                    []interface{}         `json:"customers"` // keep generic as original
	Comment                      OrderComment          `json:"comment,omitempty"`
	DisplayOrder                 *int                  `json:"display_order"`
	SyncDeliveroo                bool                  `json:"sync_deliveroo,omitempty"`
	SyncUberEats                 bool                  `json:"sync_ubereats,omitempty"`
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
	Status        int     `json:"status"`
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
	ComponentID int64   `json:"component_id"`
	Name        string  `json:"name"`
	Category    *string `json:"category"`
	Price       int     `json:"price"`
	Status      string  `json:"status"`
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
	Status string `json:"status"` // "1" or "0"
}

type AvailabilityResponse struct {
	Status  string `json:"status"`
	Updated int64  `json:"updated"`
}
