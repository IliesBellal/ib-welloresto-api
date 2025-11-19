package models

import "time"

//
// Models mirroring current JSON (legacy)
// Keep field names exactly as used by the mobile apps.
//

type OrderProductExtra struct {
	ID          int64   `json:"id"`
	OrderItemID int64   `json:"order_item_id"`
	OrderID     int64   `json:"order_id"`
	ProductID   int64   `json:"product_id"`
	Name        string  `json:"name"`
	ComponentID int64   `json:"component_id"`
	Price       float64 `json:"price"`
}

type OrderProductWithout struct {
	ID          int64  `json:"id"`
	OrderItemID int64  `json:"order_item_id"`
	OrderID     int64  `json:"order_id"`
	ProductID   int64  `json:"product_id"`
	Name        string `json:"name"`
	ComponentID int64  `json:"component_id"`
	Price       string `json:"price"`
}

type ProductComponent struct {
	ComponentID   int64   `json:"component_id"`
	Name          string  `json:"name"`
	ProductID     int64   `json:"product_id"`
	Price         float64 `json:"price"`
	Quantity      float64 `json:"quantity"`
	UnitOfMeasure string  `json:"unit_of_measure"`
	Status        int     `json:"status"`
}

type ProductConfigurationOption struct {
	ID                int64   `json:"id"`
	ConfigAttributeID int64   `json:"configurable_attribute_id"`
	OrderItemID       int64   `json:"order_item_id"`
	Title             string  `json:"title"`
	ExtraPrice        float64 `json:"extra_price"`
	Quantity          int     `json:"quantity"`
	MaxQuantity       int     `json:"max_quantity"`
	Selected          int     `json:"selected"`
}

type ProductConfigurationAttribute struct {
	ID            int64                        `json:"id"`
	OrderItemID   int64                        `json:"order_item_id"`
	AttributeType string                       `json:"attribute_type"`
	Title         string                       `json:"title"`
	MaxOptions    int                          `json:"max_options"`
	Options       []ProductConfigurationOption `json:"options"`
}

type OrderProduct struct {
	OrderID                      int64                 `json:"order_id"`
	OrderItemID                  int64                 `json:"order_item_id"`
	OrderedOn                    *time.Time            `json:"ordered_on"`
	ProductID                    int64                 `json:"product_id"`
	ProductionStatus             string                `json:"production_status"`
	ProductionStatusDoneQuantity int                   `json:"production_status_done_quantity"`
	Name                         string                `json:"name"`
	ImageURL                     *string               `json:"image_url"`
	Category                     *string               `json:"category"`
	Description                  *string               `json:"description"`
	Quantity                     int                   `json:"quantity"`
	PaidQuantity                 int                   `json:"paid_quantity"`
	DistributedQuantity          int                   `json:"distributed_quantity"`
	ReadyForDistributionQuantity int                   `json:"ready_for_distribution_quantity"`
	IsPaid                       int                   `json:"isPaid"`
	IsDistributed                int                   `json:"isDistributed"`
	Price                        float64               `json:"price"`
	PriceTakeAway                float64               `json:"price_take_away"`
	PriceDelivery                float64               `json:"price_delivery"`
	DiscountID                   *int64                `json:"discount_id"`
	DiscountName                 *string               `json:"discount_name"`
	DiscountedPrice              *float64              `json:"discounted_price"`
	TVARateIn                    float64               `json:"tva_rate_in"`
	TVARateDelivery              float64               `json:"tva_rate_delivery"`
	TVARateTakeAway              float64               `json:"tva_rate_take_away"`
	AvailableIn                  int                   `json:"available_in"`
	AvailableTakeAway            int                   `json:"available_take_away"`
	AvailableDelivery            int                   `json:"available_delivery"`
	ProductionColor              *string               `json:"production_color"`
	Extra                        []OrderProductExtra   `json:"extra"`
	Without                      []OrderProductWithout `json:"without"`
	Components                   []ProductComponent    `json:"components"`
	Customers                    []interface{}         `json:"customers"` // keep generic as original
	Comment                      interface{}           `json:"comment"`
	Configuration                struct {
		Attributes []ProductConfigurationAttribute `json:"attributes"`
	} `json:"configuration"`
}

type Payment struct {
	OrderID     int64      `json:"order_id"`
	PaymentID   int64      `json:"payment_id"`
	MOP         string     `json:"mop"`
	Amount      float64    `json:"amount"`
	PaymentDate *time.Time `json:"payment_date"`
	Enabled     int        `json:"enabled"`
}

type OrderComment struct {
	OrderID      int64      `json:"order_id"`
	UserName     *string    `json:"user_name"`
	Content      string     `json:"content"`
	CreationDate *time.Time `json:"creation_date"`
}

type Location struct {
	OrderID      int64   `json:"order_id"`
	LocationID   int64   `json:"location_id"`
	LocationName string  `json:"location_name"`
	LocationDesc *string `json:"location_desc"`
}

type Responsible struct {
	ID   *int64   `json:"id"`
	Lat  *float64 `json:"lat"`
	Lng  *float64 `json:"lng"`
	Tel  *string  `json:"tel"`
	Name *string  `json:"name"`
}

type Customer struct {
	CustomerID                 *int64   `json:"customer_id"`
	CustomerName               *string  `json:"customer_name"`
	CustomerTel                *string  `json:"customer_tel"`
	CustomerTemporaryPhone     *string  `json:"customer_temporary_phone"`
	CustomerTemporaryPhoneCode *string  `json:"customer_temporary_phone_code"`
	CustomerNbOrders           *int     `json:"customer_nb_orders"`
	CustomerAdditionalInfo     *string  `json:"customer_additional_info"`
	CustomerZoneCode           *string  `json:"customer_zone_code"`
	CustomerAddress            *string  `json:"customer_address"`
	CustomerLat                *float64 `json:"customer_lat"`
	CustomerLng                *float64 `json:"customer_lng"`
	CustomerFloorNumber        *string  `json:"customer_floor_number"`
	CustomerDoorNumber         *string  `json:"customer_door_number"`
	CustomerAdditionalAddress  *string  `json:"customer_additional_address"`
}

type Order struct {
	OrderID          int64          `json:"order_id"`
	OrderNum         *string        `json:"order_num"`
	Brand            *string        `json:"brand"`
	BrandOrderID     *string        `json:"brand_order_id"`
	BrandOrderNum    *string        `json:"brand_order_num"`
	BrandStatus      *string        `json:"brand_status"`
	OrderType        *string        `json:"order_type"`
	CutleryNotes     *string        `json:"cutlery_notes"`
	State            *string        `json:"state"`
	Scheduled        *string        `json:"scheduled"`
	TTC              float64        `json:"TTC"`
	TVA              *float64       `json:"TVA"`
	HT               *float64       `json:"HT"`
	PlacesSettings   *string        `json:"places_settings"`
	PagerNumber      *string        `json:"pager_number"`
	IsPaid           int            `json:"isPaid"`
	IsDistributed    int            `json:"isDistributed"`
	IsSNO            bool           `json:"isSNO"`
	CallHour         *string        `json:"callHour"`
	EstimatedReady   *string        `json:"estimated_ready"`
	IsDelivery       int            `json:"isDelivery"`
	MerchantApproval int            `json:"merchant_approval"`
	DeliveryFees     *float64       `json:"delivery_fees"`
	Customer         *Customer      `json:"customer"`
	Comments         []OrderComment `json:"comments"`
	Payments         []Payment      `json:"payments"`
	Responsible      *Responsible   `json:"responsible"`
	Location         []Location     `json:"location"`
	Products         []OrderProduct `json:"products"`
	Priority         *int64         `json:"priority"`
	CreationDate     *time.Time     `json:"creation_date"`
	FulfillmentType  *string        `json:"fulfillment_type"`
	LastUpdate       *time.Time     `json:"last_update"`
}

type DeliveryManInfo struct {
	UserID         int64    `json:"user_id"`
	FirstName      string   `json:"first_name"`
	LastName       string   `json:"last_name"`
	ProfilePicture *string  `json:"profile_picture"`
	Lat            *float64 `json:"lat"`
	Lng            *float64 `json:"lng"`
	PlanningColor  *string  `json:"planning_color"`
}

type DeliverySession struct {
	DeliverySessionID int64           `json:"delivery_session_id"`
	Status            string          `json:"status"`
	Orders            []Order         `json:"orders"`
	DeliveryMan       DeliveryManInfo `json:"delivery_man"`
}

type PendingOrdersResponse struct {
	Orders           []Order           `json:"orders"`
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
	Timings          interface{}       `json:"timings,omitempty"`
}

type DeliverySessionsResponse struct {
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
}
