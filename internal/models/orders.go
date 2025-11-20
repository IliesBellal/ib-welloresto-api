package models

import "time"

type Payment struct {
	OrderID     string     `json:"order_id"`
	PaymentID   int64      `json:"payment_id"`
	MOP         string     `json:"mop"`
	Amount      float64    `json:"amount"`
	PaymentDate *time.Time `json:"payment_date"`
	Enabled     int        `json:"enabled"`
}

type OrderComment struct {
	OrderID      string     `json:"order_id"`
	UserName     *string    `json:"user_name"`
	Content      string     `json:"content"`
	CreationDate *time.Time `json:"creation_date"`
}

type Location struct {
	OrderID      string  `json:"order_id"`
	LocationID   int64   `json:"location_id"`
	LocationName string  `json:"location_name"`
	LocationDesc *string `json:"location_desc"`
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
	OrderID          string         `json:"order_id"`
	OrderNum         *string        `json:"order_num"`
	Brand            *string        `json:"brand"`
	BrandOrderID     *string        `json:"brand_order_id"`
	BrandOrderNum    *string        `json:"brand_order_num"`
	BrandStatus      *string        `json:"brand_status"`
	OrderType        *string        `json:"order_type"`
	CutleryNotes     *string        `json:"cutlery_notes"`
	State            *string        `json:"state"`
	Scheduled        bool           `json:"scheduled"`
	TTC              int64          `json:"TTC"`
	TVA              *int64         `json:"TVA"`
	HT               *int64         `json:"HT"`
	PlacesSettings   *int64         `json:"places_settings"`
	PagerNumber      *string        `json:"pager_number"`
	IsPaid           bool           `json:"isPaid"`
	IsDistributed    bool           `json:"isDistributed"`
	IsSNO            bool           `json:"isSNO"`
	CallHour         *string        `json:"callHour"`
	EstimatedReady   *string        `json:"estimated_ready"`
	IsDelivery       int            `json:"isDelivery"`
	MerchantApproval string         `json:"merchant_approval"`
	DeliveryFees     *int64         `json:"delivery_fees"`
	Customer         *Customer      `json:"customer"`
	Comments         []OrderComment `json:"comments"`
	Payments         []Payment      `json:"payments"`
	Responsible      *OrderUser     `json:"responsible"`
	Location         []Location     `json:"location"`
	Products         []ProductEntry `json:"products"`
	Priority         *int64         `json:"priority"`
	CreationDate     *time.Time     `json:"creation_date"`
	FulfillmentType  *string        `json:"fulfillment_type"`
	LastUpdate       *time.Time     `json:"last_update"`
}

// Can be use as Responsible, OrderedBy, DeliveryMan, etc...
type OrderUser struct {
	UserID         string   `json:"user_id"`
	FirstName      *string  `json:"first_name"`
	LastName       *string  `json:"last_name"`
	ProfilePicture *string  `json:"profile_picture"`
	Lat            *float64 `json:"lat"`
	Lng            *float64 `json:"lng"`
	PlanningColor  *string  `json:"planning_color"`
}

type DeliverySession struct {
	DeliverySessionID string    `json:"delivery_session_id"`
	Status            string    `json:"status"`
	Orders            []Order   `json:"orders"`
	DeliveryMan       OrderUser `json:"delivery_man"`
}

type PendingOrdersResponse struct {
	Orders           []Order           `json:"orders"`
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
	Timings          interface{}       `json:"timings,omitempty"`
}

type DeliverySessionsResponse struct {
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
}
