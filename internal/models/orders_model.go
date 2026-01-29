package models

import (
	"time"
)

type Payment struct {
	OrderID     string  `json:"order_id"`
	PaymentID   int64   `json:"payment_id"`
	MOP         string  `json:"mop"`
	Amount      float64 `json:"amount"`
	PaymentDate int64   `json:"payment_date"`
	Enabled     int     `json:"enabled"`
}

type OrderComment struct {
	OrderID      string     `json:"order_id"`
	UserName     *string    `json:"user_name"`
	Content      string     `json:"content"`
	CreationDate *time.Time `json:"creation_date"`
}

type Location struct {
	LocationID   string  `json:"location_id"`
	OrderID      *string `json:"order_id"`
	BookingID    *string `json:"booking_id"`
	LocationName string  `json:"location_name"`
	LocationDesc *string `json:"location_desc"`
	Seats        int     `json:"seats"`
	Order        int     `json:"order"`
	FloorID      string  `json:"floor_id,omitempty"`
	Shape        string  `json:"shape,omitempty"`
	X            string  `json:"x,omitempty"`
	Y            string  `json:"y,omitempty"`
	W            string  `json:"w,omitempty"`
	H            string  `json:"h,omitempty"`
	Angle        string  `json:"angle,omitempty"`
	OpenOrderID  string  `json:"open_order_id,omitempty"`
	Available    string  `json:"available,omitempty"`
}

type SeatingPlan struct {
	Locations []Location `json:"locations"`
	Floors    []Floor    `json:"floors"`
	Areas     []Area     `json:"areas"`
	Bookings  []Booking  `json:"bookings"`
}

type Area struct {
}

type Floor struct {
}

type PaymentItem struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int    `json:"quantity"`
}

type Customer struct {
	CustomerID                         *string  `json:"customer_id"`
	CustomerCode                       *string  `json:"customer_code"`
	CustomerName                       *string  `json:"customer_name"`
	CustomerTel                        *string  `json:"customer_tel"`
	CustomerEmail                      *string  `json:"customer_email"`
	CustomerTemporaryPhone             *string  `json:"customer_temporary_phone"`
	CustomerTemporaryPhoneCode         *string  `json:"customer_temporary_phone_code"`
	CustomerNbOrders                   *int     `json:"customer_nb_orders"`
	CustomerNbBookings                 *int     `json:"customer_nb_bookings"`
	CustomerTotalSpent                 *int     `json:"customer_total_spent"`
	MatchScore                         *int     `json:"match_score"`
	CustomerAdditionalInfo             *string  `json:"customer_additional_info"`
	CustomerZoneCode                   *string  `json:"customer_zone_code"`
	CustomerAddress                    *string  `json:"customer_address"`
	CustomerLat                        *float64 `json:"customer_lat"`
	CustomerLng                        *float64 `json:"customer_lng"`
	CustomerFloorNumber                *string  `json:"customer_floor_number"`
	CustomerDoorNumber                 *string  `json:"customer_door_number"`
	CustomerAdditionalAddress          *string  `json:"customer_additional_address"`
	MerchantID                         string   `json:"merchant_id"`
	CustomerBusinessName               *string  `json:"customer_business_name"`
	CustomerBirthdate                  *string  `json:"customer_birthdate"`
	CustomerTemporaryAddress           *string  `json:"customer_temporary_address"`
	CustomerTemporaryLat               *string  `json:"customer_temporary_lat"`
	CustomerTemporaryLng               *string  `json:"customer_temporary_lng"`
	CustomerTemporaryDoorNumber        *string  `json:"customer_temporary_door_number"`
	CustomerTemporaryFloorNumber       *string  `json:"customer_temporary_floor_number"`
	CustomerTemporaryAdditionalAddress *string  `json:"customer_temporary_additional_address"`
	CreationDate                       *string  `json:"creation_date"`
}

type Order struct {
	OrderID           string         `json:"order_id"`
	OrderNum          *string        `json:"order_num"`
	DeliverySessionID *string        `json:"delivery_session_id"`
	DeliveryPriority  *int           `json:"delivery_priority"`
	Brand             *string        `json:"brand"`
	BrandOrderID      *string        `json:"brand_order_id"`
	BrandOrderNum     *string        `json:"brand_order_num"`
	BrandStatus       *string        `json:"brand_status"`
	OrderType         *string        `json:"order_type"`
	CutleryNotes      *string        `json:"cutlery_notes"`
	State             *string        `json:"state"`
	Scheduled         bool           `json:"scheduled"`
	TTC               int64          `json:"TTC"`
	TVA               *int64         `json:"TVA"`
	HT                *int64         `json:"HT"`
	PlacesSettings    *int64         `json:"places_settings"`
	PagerNumber       *string        `json:"pager_number"`
	IsPaid            bool           `json:"isPaid"`
	IsDistributed     bool           `json:"isDistributed"`
	IsSNO             bool           `json:"isSNO"`
	CallHour          *string        `json:"callHour"`
	EstimatedReady    *int           `json:"estimated_ready"`
	IsDelivery        int            `json:"isDelivery"`
	MerchantApproval  string         `json:"merchant_approval"`
	DeliveryFees      *int64         `json:"delivery_fees"`
	Customer          *Customer      `json:"customer"`
	Comments          []OrderComment `json:"comments"`
	Payments          []Payment      `json:"payments"`
	Responsible       *OrderUser     `json:"responsible"`
	Location          []Location     `json:"location"`
	Products          []ProductEntry `json:"products"`
	Priority          *int           `json:"priority"`
	CreationDate      int64          `json:"creation_date"`
	FulfillmentType   *string        `json:"fulfillment_type"`
	LastUpdate        int64          `json:"last_update"`
}

// OrderUser Can be used as Responsible, OrderedBy, DeliveryMan, etc...
type OrderUser struct {
	UserID         string   `json:"user_id"`
	FirstName      *string  `json:"first_name"`
	LastName       *string  `json:"last_name"`
	ProfilePicture *string  `json:"profile_picture,omitempty"`
	Lat            *float64 `json:"lat,omitempty"`
	Lng            *float64 `json:"lng,omitempty"`
	PlanningColor  *string  `json:"planning_color,omitempty"`
	Status         *string  `json:"status,omitempty"`
}

type PendingOrdersResponse struct {
	Orders           []Order           `json:"orders"`
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
	Timings          interface{}       `json:"timings,omitempty"`
}

type DeliverySessionsResponse struct {
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
}

type DistributedProduct struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int    `json:"quantity"`
}

type SetDistributedProductsRequest struct {
	OrderID  string               `json:"order_id"`
	Products []DistributedProduct `json:"products"`
}
