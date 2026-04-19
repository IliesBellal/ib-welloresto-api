package discounts

import (
	"time"
)

// DiscountUnit represents the unit type for discount calculation
type DiscountUnit string

const (
	DiscountUnitPercentage DiscountUnit = "PERCENTAGE"
	DiscountUnitCurrency   DiscountUnit = "CURRENCY"
	DiscountUnitNewPrice   DiscountUnit = "NEWPRICE"
)

// OrderType represents the order type for which the discount applies
type OrderType string

const (
	OrderTypeInDining OrderType = "IN"
	OrderTypeDelivery OrderType = "DELIVERY"
)

// MinOrderUnit represents the unit for minimum order requirements
type MinOrderUnit string

const (
	MinOrderUnitCurrency MinOrderUnit = "CURRENCY"
	MinOrderUnitQuantity MinOrderUnit = "QUANTITY"
)

// MaxDiscountUnit represents the unit for maximum discount cap
type MaxDiscountUnit string

const (
	MaxDiscountUnitCurrency MaxDiscountUnit = "CURRENCY"
	MaxDiscountUnitQuantity MaxDiscountUnit = "QUANTITY"
)

// DiscountProduct represents a product associated with a discount and its custom price
type DiscountProduct struct {
	ID         int64  `json:"id"`
	DiscountID int64  `json:"discount_id"`
	ProductID  string `json:"product_id"` // UUID
	NewPrice   *int64 `json:"new_price,omitempty"`
	Enabled    bool   `json:"enabled"`
}

// DiscountSchedule represents time slot availability for a discount
type DiscountSchedule struct {
	ScheduleID    int64     `json:"schedule_id"`
	DiscountID    int64     `json:"discount_id"`
	DayOfWeek     int       `json:"day_of_week"`    // 1 = Sunday, 2 = Monday, etc.
	AvailableFrom time.Time `json:"available_from"` // UTC time (only time part)
	AvailableTo   time.Time `json:"available_to"`   // UTC time (only time part)
	Enabled       bool      `json:"enabled"`
}

// Discount represents a complete discount/promotion
type Discount struct {
	DiscountID         string             `json:"discount_id"`
	MerchantID         string             `json:"merchant_id"`
	DiscountName       string             `json:"discount_name"`
	DiscountDesc       string             `json:"discount_desc"`
	PreferredOrder     int                `json:"preferred_order"`
	DiscountCode       *string            `json:"discount_code,omitempty"`
	OrderType          *OrderType         `json:"order_type,omitempty"` // nil = all types
	DiscountValue      float64            `json:"discount_value"`
	DiscountUnit       DiscountUnit       `json:"discount_unit"`
	ValidFrom          time.Time          `json:"valid_from"`         // UTC
	ValidTo            *time.Time         `json:"valid_to,omitempty"` // UTC
	MinOrderValue      *float64           `json:"min_order_value,omitempty"`
	MinOrderUnit       *MinOrderUnit      `json:"min_order_unit,omitempty"`
	MaxDiscountValue   *float64           `json:"max_discount_value,omitempty"`
	MaxDiscountUnit    *MaxDiscountUnit   `json:"max_discount_unit,omitempty"`
	DiscountedQuantity int                `json:"discounted_quantity"`
	IsCumulative       bool               `json:"is_cumulative"`
	IsTimeLimited      bool               `json:"is_time_limited"`
	Available          bool               `json:"available"`
	Enabled            bool               `json:"enabled"`
	CreationDate       time.Time          `json:"creation_date"` // UTC
	Products           []DiscountProduct  `json:"products,omitempty"`
	Schedules          []DiscountSchedule `json:"schedules,omitempty"`
}

// CreateDiscountRequest is the payload for creating a discount
type CreateDiscountRequest struct {
	DiscountName       string                  `json:"discount_name"`
	DiscountDesc       string                  `json:"discount_desc"`
	PreferredOrder     int                     `json:"preferred_order"`
	DiscountCode       *string                 `json:"discount_code,omitempty"`
	OrderType          *OrderType              `json:"order_type,omitempty"`
	DiscountValue      float64                 `json:"discount_value"`
	DiscountUnit       DiscountUnit            `json:"discount_unit"`
	ValidFrom          time.Time               `json:"valid_from"`
	ValidTo            *time.Time              `json:"valid_to,omitempty"`
	MinOrderValue      *float64                `json:"min_order_value,omitempty"`
	MinOrderUnit       *MinOrderUnit           `json:"min_order_unit,omitempty"`
	MaxDiscountValue   *float64                `json:"max_discount_value,omitempty"`
	MaxDiscountUnit    *MaxDiscountUnit        `json:"max_discount_unit,omitempty"`
	DiscountedQuantity int                     `json:"discounted_quantity"`
	IsCumulative       bool                    `json:"is_cumulative"`
	IsTimeLimited      bool                    `json:"is_time_limited"`
	Available          bool                    `json:"available"`
	Products           []CreateProductRequest  `json:"products,omitempty"`
	Schedules          []CreateScheduleRequest `json:"schedules,omitempty"`
}

// CreateProductRequest represents product data for discount creation
type CreateProductRequest struct {
	ProductID string `json:"product_id"` // UUID
	NewPrice  *int64 `json:"new_price,omitempty"`
}

// CreateScheduleRequest represents schedule data for discount creation
type CreateScheduleRequest struct {
	DayOfWeek     int       `json:"day_of_week"`
	AvailableFrom time.Time `json:"available_from"`
	AvailableTo   time.Time `json:"available_to"`
}

// UpdateDiscountRequest is the payload for updating a discount
type UpdateDiscountRequest struct {
	DiscountName       *string                 `json:"discount_name,omitempty"`
	DiscountDesc       *string                 `json:"discount_desc,omitempty"`
	PreferredOrder     *int                    `json:"preferred_order,omitempty"`
	DiscountCode       *string                 `json:"discount_code,omitempty"`
	OrderType          *OrderType              `json:"order_type,omitempty"`
	DiscountValue      *float64                `json:"discount_value,omitempty"`
	DiscountUnit       *DiscountUnit           `json:"discount_unit,omitempty"`
	ValidFrom          *time.Time              `json:"valid_from,omitempty"`
	ValidTo            *time.Time              `json:"valid_to,omitempty"`
	MinOrderValue      *float64                `json:"min_order_value,omitempty"`
	MinOrderUnit       *MinOrderUnit           `json:"min_order_unit,omitempty"`
	MaxDiscountValue   *float64                `json:"max_discount_value,omitempty"`
	MaxDiscountUnit    *MaxDiscountUnit        `json:"max_discount_unit,omitempty"`
	DiscountedQuantity *int                    `json:"discounted_quantity,omitempty"`
	IsCumulative       *bool                   `json:"is_cumulative,omitempty"`
	IsTimeLimited      *bool                   `json:"is_time_limited,omitempty"`
	Available          *bool                   `json:"available,omitempty"`
	Products           []CreateProductRequest  `json:"products,omitempty"`  // If provided, replaces all
	Schedules          []CreateScheduleRequest `json:"schedules,omitempty"` // If provided, replaces all
}

// Validate checks the CreateDiscountRequest validity
func (r *CreateDiscountRequest) Validate() error {
	if r.DiscountName == "" || len(r.DiscountName) > 50 {
		return ErrInvalidDiscountName
	}
	if r.DiscountDesc == "" || len(r.DiscountDesc) > 100 {
		return ErrInvalidDiscountDesc
	}
	if r.DiscountValue < 0 {
		return ErrInvalidDiscountValue
	}
	if r.DiscountUnit != DiscountUnitPercentage && r.DiscountUnit != DiscountUnitCurrency && r.DiscountUnit != DiscountUnitNewPrice {
		return ErrInvalidDiscountUnit
	}
	if r.IsTimeLimited && len(r.Schedules) == 0 {
		return ErrInvalidSchedules
	}
	return nil
}
