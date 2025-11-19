package models

import "time"

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
	CategoryID int64          `json:"category_id"`
	Order      int            `json:"order"`
	BgColor    *string        `json:"bg_color,omitempty"`
	Products   []ProductEntry `json:"products"`
}

// product
type ProductEntry struct {
	ProductID         int64                `json:"product_id"`
	ByProductOf       *int64               `json:"by_product_of,omitempty"`
	HasImage          bool                 `json:"has_image"`
	ImageURL          *string              `json:"image_url,omitempty"`
	IsPopular         bool                 `json:"is_popular"`
	IsAvailableOnSNO  bool                 `json:"is_available_on_sno"`
	Name              string               `json:"name"`
	Components        []ComponentUsage     `json:"components"`
	Description       *string              `json:"description,omitempty"`
	Price             int                  `json:"price"`
	PriceTakeAway     int                  `json:"price_take_away"`
	PriceDelivery     int                  `json:"price_delivery"`
	TVAIn             *float64             `json:"tva_rate_in,omitempty"`
	TVADelivery       *float64             `json:"tva_rate_delivery,omitempty"`
	TVATakeAway       *float64             `json:"tva_rate_take_away,omitempty"`
	AvailableIn       *string              `json:"available_in,omitempty"`
	AvailableTakeAway *string              `json:"available_take_away,omitempty"`
	AvailableDelivery *string              `json:"available_delivery,omitempty"`
	Category          int64                `json:"category"`
	IsProductGroup    bool                 `json:"is_product_group"`
	BgColor           *string              `json:"bg_color,omitempty"`
	Status            int                  `json:"status"`
	SubProducts       []ProductEntry       `json:"sub_products"`
	Configuration     ConfigurableResponse `json:"configuration"`
}

// components required
type ComponentUsage struct {
	ComponentID   int64   `json:"component_id"`
	Name          string  `json:"name"`
	Price         int     `json:"price"`
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
	ComponentID int64  `json:"component_id"`
	Name        string `json:"name"`
	Category    int64  `json:"category"`
	Price       int    `json:"price"`
	Status      int    `json:"status"`
}

// configurable attributes
type ConfigurableResponse struct {
	Attributes []ConfigurableAttribute `json:"attributes"`
}

type ConfigurableAttribute struct {
	ID            int64                `json:"id"`
	ProductID     int64                `json:"product_id"`
	Title         string               `json:"title"`
	MaxOptions    int                  `json:"max_options"`
	MinOptions    int                  `json:"min_options"`
	AttributeType string               `json:"attribute_type"`
	Options       []ConfigurableOption `json:"options"`
}

type ConfigurableOption struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ExtraPrice  int    `json:"extra_price"`
	MaxQuantity int    `json:"max_quantity"`
}

// delays
type DelayEntry struct {
	DelayID          int64  `json:"delay_id"`
	ShortDescription string `json:"short_description"`
	Duration         int    `json:"duration"`
}
