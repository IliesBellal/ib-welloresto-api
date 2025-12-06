package models

type PaymentRequest struct {
	DeviceID        string        `json:"device_id"`
	OrderID         string        `json:"order_id"`
	MOP             string        `json:"mop"`
	Amount          int           `json:"amount"`
	Items           []PaymentItem `json:"items"`
	DiscountComment string        `json:"discount_comment"`
	StatusCheck     string        `json:"status_check"`
	Code            string        `json:"tr_code"`
}

type OpenCashRegisterRequest struct {
	CashRegister struct {
		CashRegisterID *string `json:"cash_register_id"` // peut être null
		CashDeskID     string  `json:"cash_desk_id"`
		CashFund       float64 `json:"cash_fund"`
		UserID         string  `json:"user_id"`
	} `json:"cash_register"`
	DeviceID string `json:"device_id"`
}

type CloseCashRegisterRequest struct {
	CashFund float64 `json:"cash_fund"`
	UserID   string  `json:"user_id"`
	DeviceID string  `json:"device_id"`
}

type CashRegisterSummaryResponse struct {
	Status       string               `json:"status"`
	CashRegister *CashRegisterSummary `json:"cash_register"`
}

type CashRegisterSummary struct {
	CashRegisterID string         `json:"cash_register_id"`
	StartDate      string         `json:"start_date"`
	EndDate        *string        `json:"end_date"`
	CashDesk       CashDeskInfo   `json:"cash_desk"`
	CashFund       float64        `json:"cash_fund"`
	Currency       string         `json:"currency"`
	Closed         int            `json:"closed"`
	ClosureComment *string        `json:"closure_comment"`
	OpenedBy       UserBaseInfo   `json:"opened_by"`
	ClosedBy       UserBaseInfo   `json:"closed_by"`
	Orders         []CROrder      `json:"orders"`
	Payments       []CRPayment    `json:"payments"`
	Items          []CRItem       `json:"items"`
	CustomItems    []CRCustomItem `json:"custom_items"`
}

type CashDeskInfo struct {
	CashDeskID     string       `json:"cash_desk_id"`
	CashDeskName   string       `json:"cash_desk_name"`
	Active         int          `json:"active"`
	DeviceID       *string      `json:"device_id"`
	CashRegisterID *string      `json:"cash_register_id"`
	OpenedBy       OpenedByInfo `json:"opened_by"`
}

type UserBaseInfo struct {
	UserID    *string `json:"user_id"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
}

type CRItem struct {
	ItemID   string  `json:"item_id"`
	MOP      string  `json:"mop"`
	Label    *string `json:"label"`
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

type CRCustomItem struct {
	ItemID   string  `json:"item_id"`
	Label    string  `json:"label"`
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

type CROrder struct {
	OrderID      string       `json:"order_id"`
	OrderNum     string       `json:"order_num"`
	OrderedBy    UserBaseInfo `json:"ordered_by"`
	CreationDate string       `json:"creation_date"`
	Currency     string       `json:"currency"`
	IsDelivery   int          `json:"isDelivery"`
	Status       string       `json:"status"`
	Price        float64      `json:"price"`
}

type CRPayment struct {
	OrderID     string       `json:"order_id"`
	OrderNum    string       `json:"order_num"`
	PaymentID   string       `json:"payment_id"`
	CollectedBy UserBaseInfo `json:"collected_by"`
	PaymentDate string       `json:"payment_date"`
	Amount      float64      `json:"amount"`
	Enabled     int          `json:"enabled"`
	Currency    string       `json:"currency"`
	MOP         string       `json:"mop"`
}

type CashRegisterReport struct {
	Status         int                       `json:"status"`
	CashReportID   string                    `json:"cash_report_id"`
	PeriodFrom     string                    `json:"period_from"`
	PeriodTo       string                    `json:"period_to"`
	CashFund       float64                   `json:"cash_fund"`
	HT             int                       `json:"HT"`
	TTC            int                       `json:"TTC"`
	TVA            int                       `json:"TVA"`
	CashReport     []CashReportDeliveryGroup `json:"cash_report"`
	MOP            []MOPLine                 `json:"mop"`
	CashReportType string                    `json:"cash_report_type"`
}

type CashReportDeliveryGroup struct {
	DeliveryTypeID    string            `json:"delivery_type_id"`
	DeliveryTypeLabel string            `json:"delivery_type_label"`
	TVACategories     []TVACategoryLine `json:"tva_categories"`
}

type TVACategoryLine struct {
	TVATitle string `json:"tva_title"`
	HT       int    `json:"HT"`
	TTC      int    `json:"TTC"`
	TVA      int    `json:"TVA"`
}

type MOPLine struct {
	MOP    string  `json:"mop"`
	Amount float64 `json:"amount"`
	Label  string  `json:"label,omitempty"`
}

type AddCustomItemRequest struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type EncloseCashRegisterRequest struct {
	UserID  string `json:"user_id"`
	Comment string `json:"comment"`
}

type BookingObjectRequest struct {
	MerchantID      string   `json:"merchant_id"`
	BookingID       *string  `json:"booking_id"`
	BookingNumber   *string  `json:"booking_number"`
	BookingDateFrom *string  `json:"booking_date_from"`
	BookingDateTo   *string  `json:"booking_date_to"`
	CreatedBy       string   `json:"created_by"`
	Customer        Customer `json:"customer"`
	Booking         Booking  `json:"booking"`
}

type OrderHistoryRequest struct {
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}

type UpdateLocationCoordinatesRequest struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type TRCheckResponse struct {
	Status  string `json:"status"` // valid, used, expired, invalid_format, no_value
	Message string `json:"message"`

	ID      string  `json:"id"`
	Value   float64 `json:"value"`
	Vintage int     `json:"vintage"`
	Code    string  `json:"code"`
}

// models/pricing.go

type PricingRequest struct {
	MerchantID                  string        `json:"merchant_id"`
	Order                       *OrderRequest `json:"order"`
	DayOfWeek                   int           `json:"day_of_week"`
	Time                        string        `json:"time"`
	DiscountCode                string        `json:"discount_code,omitempty"`
	MinimumCartForDeliveryOrder int
	IsOrderable                 bool
	IsSNO                       bool
	NotOrderableReason          string
}

type PricingResult struct {
	Status                int           `json:"status"`
	Order                 *OrderRequest `json:"order"`
	EstimatedDistribution int           `json:"estimated_distribution_time"`
	RetrievedDiscounts    interface{}   `json:"retrieved_discounts"`
}

type PricingDBData struct {
	Merchant    *MerchantPricingInfo
	Products    []DBProduct
	Unavailable []int64
	Discounts   []*DBDiscount
	Rewards     []*DBReward
	DistTimeSec int
}

type MerchantPricingInfo struct {
	Timezone                    string
	Currency                    *string
	DeliveryFees                int
	DeliveryFeesLimit           int
	MinimumCartForDeliveryOrder int
}

// DBProduct représente les données côté base nécessaires au pricing.
type DBProduct struct {
	ProductID       string  `json:"product_id"`
	Name            string  `json:"name"`
	Price           int     `json:"price"`              // prix par défaut (in-store)
	PriceTakeAway   int     `json:"price_take_away"`    // prix takeaway
	PriceDelivery   int     `json:"price_delivery"`     // prix delivery
	TVARateIn       float64 `json:"tva_rate_in"`        // TVA rate for "in"
	TVARateDelivery float64 `json:"tva_rate_delivery"`  // TVA rate for delivery
	TVARateTakeAway float64 `json:"tva_rate_take_away"` // TVA rate for takeaway

	// Optional: configuration / extras / without / components may be filled separately
	// (we keep them generic so que la couche service puisse y attacher les payloads reçus)
	Configuration *ProductConfiguration `json:"configuration,omitempty"`
	Extra         []ExtraLine           `json:"extra,omitempty"`
	Without       []WithoutLine         `json:"without,omitempty"`
	// other DB-side fields if needed
}

// ProductConfiguration is a minimal model for configurable attributes/options.
type ProductConfiguration struct {
	Attributes []ConfigurationAttribute `json:"attributes,omitempty"`
}

type ConfigurationAttribute struct {
	ID      string                `json:"id"`
	Name    *string               `json:"name,omitempty"`
	Options []ConfigurationOption `json:"options,omitempty"`
}

type ConfigurationOption struct {
	ID         string  `json:"id"`
	Label      *string `json:"label,omitempty"`
	ExtraPrice int     `json:"extra_price,omitempty"` // may be filled from DB
	Selected   bool    `json:"selected,omitempty"`    // payload flag
	Quantity   int     `json:"quantity,omitempty"`
}

type ExtraLine struct {
	ComponentID int64    `json:"component_id"`
	Price       *float64 `json:"price,omitempty"`
}

type WithoutLine struct {
	ComponentID int64 `json:"component_id"`
}

// DBDiscount représente une promotion (discount) côté DB,
// incluant produits et options reliés (pré-groupés pour faciliter la logique business).
type DBDiscount struct {
	DiscountID         string   `json:"discount_id"`
	DiscountOrderType  string   `json:"discount_order_type,omitempty"` // e.g. "DELIVERY", "TAKE_AWAY", ...
	DiscountCode       *string  `json:"discount_code,omitempty"`
	DiscountDesc       *string  `json:"discount_desc,omitempty"`
	DiscountValue      int      `json:"discount_value"`
	DiscountUnit       string   `json:"discount_unit,omitempty"` // e.g. "PERCENTAGE", "CURRENCY", "NEWPRICE"
	MinOrderValue      int      `json:"min_order_value,omitempty"`
	MinOrderUnit       string   `json:"min_order_unit,omitempty"` // "QTY" or "CURRENCY"
	MaxDiscountValue   *float64 `json:"max_discount_value,omitempty"`
	MaxDiscountUnit    *string  `json:"max_discount_unit,omitempty"`
	DiscountedQuantity int      `json:"discounted_quantity"`
	IsCumulative       bool     `json:"is_cumulative"` // true/false
	Available          bool     `json:"available"`
	PreferredOrder     int      `json:"prefered_order"`
	// RelatedProducts: product -> new_price (if unit NEWPRICE) or presence for discount targeting
	RelatedProductIDs []int64                       `json:"related_products_ids,omitempty"`
	RelatedProducts   map[int64]DiscountProductInfo `json:"related_products,omitempty"` // key = product_id
	// Related options per product for option-based promotions:
	// map[discount_id][product_id] -> []DiscountOptionInfo in PHP; here we attach directly
	RelatedProductOptions map[string][]DiscountOptionInfo `json:"related_products_options,omitempty"` // key=product_id
}

type DiscountProductInfo struct {
	ProductID string `json:"product_id"`
	NewPrice  int    `json:"new_price,omitempty"`
}

type DiscountOptionInfo struct {
	OptionID          string `json:"option_id"`
	IsOptionMandatory bool   `json:"is_option_mandatory"`
	NewPrice          *int   `json:"new_price,omitempty"`
}

// DBReward représente une reward utilisateur (customer reward) et les produits liés.
type DBReward struct {
	RewardID         string   `json:"reward_id"`
	LoyaltyProgramID int64    `json:"loyalty_program_id"`
	RewardType       string   `json:"reward_type,omitempty"`       // e.g. "free_product", "fixed_discount"
	RewardOrderType  string   `json:"reward_order_type,omitempty"` // order type constraint
	RewardValue      *int     `json:"reward_value,omitempty"`
	CreationDate     *string  `json:"creation_date,omitempty"`
	IsUsed           bool     `json:"is_used"`
	ProductIDs       []string `json:"products,omitempty"`
	ProgramName      *string  `json:"program_name,omitempty"` // optional human label
}

type PricingOrder struct {
	OrderType  string `json:"order_type"`
	IsDelivery string `json:"is_delivery"`
	Currency   string `json:"currency"`

	Products []*PricingOrderProduct `json:"products"`

	// Calculated values
	TTC          float64 `json:"ttc"`
	TVA          float64 `json:"tva"`
	HT           float64 `json:"ht"`
	DeliveryFees float64 `json:"delivery_fees"`

	UsedRewards []UsedReward `json:"used_rewards,omitempty"`
}

type PricingOrderProduct struct {
	ProductID   int64   `json:"product_id"`
	Quantity    int     `json:"quantity"`
	Comment     *string `json:"comment,omitempty"`
	Description *string `json:"description,omitempty"`

	// Options & extras
	Extra         []*ProductExtra       `json:"extra,omitempty"`
	Without       []*ProductWithout     `json:"without,omitempty"`
	Configuration *ProductConfiguration `json:"configuration,omitempty"`
}

type PricingResponse struct {
	Status       int             `json:"status"`
	OrderRequest *PricingRequest `json:"order_request"`

	UnavailableProduct []int64 `json:"unavailable_products,omitempty"`

	EstimatedDistributionTime int `json:"estimated_distribution_time"`

	MinimumCartForDeliveryOrder float64 `json:"minimum_cart_for_delivery_order,omitempty"`
	IsOrderable                 bool    `json:"is_orderable"`
	NotOrderableReason          string  `json:"not_orderable_reason,omitempty"`

	AppliedDiscounts []string `json:"applied_discounts,omitempty"`
}

type SelectedProduct struct {
	ProductID       string                   `json:"product_id"`
	ProductName     string                   `json:"product_name"`
	Quantity        int                      `json:"quantity"`
	Comment         *OrderItemCommentPayload `json:"comment"`
	Description     *string                  `json:"description,omitempty"`
	Price           int                      `json:"price"`
	TvaRate         float64                  `json:"tva_rate"`
	Extra           []*OrderExtraPayload     `json:"extra,omitempty"`
	Without         []*OrderWithoutPayload   `json:"without,omitempty"`
	Config          *ProductConfiguration    `json:"configuration,omitempty"`
	DiscountID      *string                  `json:"discount_id,omitempty"`
	DiscountedPrice *int                     `json:"discounted_price,omitempty"`
	OrderedDate     string                   `json:"ordered_date"`
}

type ProductExtra struct {
	ExtraID int64   `json:"extra_id"`
	Price   float64 `json:"price"`
	Name    string  `json:"name"`
}

type ProductWithout struct {
	WithoutID int64  `json:"without_id"`
	Name      string `json:"name"`
}

type ConfigOption struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Selected bool   `json:"selected"`

	// Loaded from DB
	ExtraPrice float64 `json:"extra_price"`
}

type UsedReward struct {
	RewardID string `json:"reward_id"`
}
