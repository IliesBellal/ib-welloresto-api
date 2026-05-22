package models

import (
	"time"
)

type PaymentRequest struct {
	DeviceID       string        `json:"device_id"`
	OrderID        string        `json:"order_id"`
	MOP            string        `json:"mop"`
	Amount         int           `json:"amount"`
	Items          []PaymentItem `json:"items"`
	Comment        string        `json:"discount_comment"`
	StatusCheck    string        `json:"status_check"`
	Code           string        `json:"tr_code"`
	CashRegisterID *string       `json:"cash_register_id"`
}

// La requête envoyée par ton Front-end / TPE
type RefundRequest struct {
	DeviceID string `json:"device_id"`
	OrderID  string `json:"order_id"` // La commande d'origine
	MOP      string `json:"mop"`      // Moyen de paiement (ex: CB, CASH)
	Amount   int    `json:"amount"`   // Le montant à rembourser (peut être positif en JSON, on l'inversera)
	Comment  string `json:"comment"`  // Raison du remboursement
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
	CashFund      int    `json:"cash_fund"`
	FinalCashFund int    `json:"final_cash_fund"`
	Comment       string `json:"comment"`
	UserID        string `json:"user_id"`
	DeviceID      string `json:"device_id"`
}

type CashRegisterSummaryResponse struct {
	Status       string               `json:"status"`
	CashRegister *CashRegisterSummary `json:"cash_register"`
}

type CashRegisterSummary struct {
	CashRegisterID string         `json:"cash_register_id"`
	StartDate      int64          `json:"start_date"`
	EndDate        *int           `json:"end_date"`
	CashDesk       CashDeskInfo   `json:"cash_desk"`
	CashFund       int            `json:"cash_fund"`
	FinalCashFund  int            `json:"final_cash_fund"`
	Closed         bool           `json:"closed"`   // correspond à la colonne closed
	Enclosed       bool           `json:"enclosed"` // correspond à la colonne enclosed
	Currency       string         `json:"currency"`
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
	MOP      string  `json:"mop"`
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
	IsDelivery   *int         `json:"isDelivery"`
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

type CashRegisterOpenResponse struct {
	Status       string            `json:"status"`
	CashRegister *CashRegisterOpen `json:"cash_register"`
}

type CashRegisterOpen struct {
	CashRegisterId string `json:"cash_register_id"`
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
	MOP    string `json:"mop"`
	Amount int    `json:"amount"`
	Label  string `json:"label,omitempty"`
}

type AddCustomItemRequest struct {
	Label string `json:"label"`
	Value int    `json:"value"`
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
	DateFrom   *string  `json:"date_from"`
	DateTo     *string  `json:"date_to"`
	Search     *string  `json:"search"`
	CustomerID *string  `json:"customer_id"`
	Channel    []string `json:"channel"`
	OrderType  []string `json:"order_type"`
	Status     []string `json:"status"`
	Page       *int     `json:"page"`
	Limit      *int     `json:"limit"`
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
	DayOfWeek                   int           `json:"-"`
	Time                        string        `json:"-"`
	DiscountCode                string        `json:"discount_code,omitempty"`
	MinimumCartForDeliveryOrder int           `json:"-"`
	IsOrderable                 bool          `json:"-"`
	IsSNO                       bool          `json:"is_sno,omitempty"`
	NotOrderableReason          string        `json:"not_orderable_reason,omitempty"`

	QRCode           string `json:"qr_code,omitempty"`
	IsInDeliveryZone bool   `json:"is_in_delivery_zone,omitempty"`

	CheckoutSessionType string       `json:"checkout_session_type,omitempty"`
	Merchant            *MerchantRow `json:"merchant,omitempty"`
}

type MerchantRow struct {
	MerchantID                  string
	FullName                    string
	Address                     string
	Lat                         float64
	Lng                         float64
	DeliveryDistanceLimit       float64 // EN MÈTRES (comme en DB)
	Timezone                    string
	Phone                       *string
	Currency                    string
	PrimaryColor                string
	TextColor                   string
	LogoURL                     *string
	BannerURL                   *string
	DeliveryFees                float64
	DeliveryFeesLimit           float64
	MinimumCartForDeliveryOrder float64
	MenuOnly                    bool
	UserID                      *string
	LastWaiterCall              *int
	OrderID                     *string
	LocationID                  *string
	LocationName                *string
	CreationDate                *int64
	VariableFees                *float64
	FixedFees                   *int
	AccountID                   *string

	TakeawayEnabled   bool `json:"takeaway_enabled"`
	TakeawayAvailable bool `json:"takeaway_available"`
	DeliveryEnabled   bool `json:"delivery_enabled"`
	DeliveryAvailable bool `json:"delivery_available"`
	InEnabled         bool `json:"in_enabled"`
	InAvailable       bool `json:"in_available"`

	PrepTimeMode        string `json:"prep_time_mode"` // AUTO | MANUAL
	PrepTime            int    `json:"prep_time"`      // en minutes
	EnableAdvanceOrders bool   `json:"enable_advance_orders"`
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
	DiscountName       *string  `json:"discount_name,omitempty"`
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
	RewardID           string   `json:"reward_id"`
	LoyaltyProgramID   string   `json:"loyalty_program_id"`
	RewardType         string   `json:"reward_type,omitempty"`       // e.g. "free_product", "fixed_discount"
	RewardOrderType    string   `json:"reward_order_type,omitempty"` // order type constraint
	RewardValue        *int     `json:"reward_value,omitempty"`
	MinOrderValue      int      `json:"min_order_value,omitempty"`
	MaxDiscountValue   *int     `json:"max_discount_value,omitempty"`
	MaxRewardsPerOrder int      `json:"max_rewards_per_order,omitempty"`
	CreationDate       *string  `json:"creation_date,omitempty"`
	IsUsed             bool     `json:"is_used"`
	ProductIDs         []string `json:"products,omitempty"`
	ProgramName        *string  `json:"program_name,omitempty"` // optional human label
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
	Status       string          `json:"status"`
	OrderRequest *PricingRequest `json:"order_request"`

	UnavailableProduct []UnavailableProductInfo `json:"unavailable_products"`

	EstimatedDistributionTime int `json:"estimated_distribution_time"`

	MinimumCartForDeliveryOrder float64 `json:"minimum_cart_for_delivery_order,omitempty"`
	IsOrderable                 bool    `json:"is_orderable"`
	NotOrderableReason          string  `json:"not_orderable_reason,omitempty"`

	AppliedDiscounts []string `json:"applied_discounts,omitempty"`
}

type UnavailableProductInfo struct {
	ProductID int64  `json:"product_id"`
	Name      string `json:"name"`
	Status    int    `json:"status"`
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

type UserSettingsRequest struct {
	FirstName            *string `json:"first_name,omitempty"`
	LastName             *string `json:"last_name,omitempty"`
	Email                *string `json:"email,omitempty"`
	Tel                  *string `json:"tel,omitempty"`
	Address              *string `json:"address,omitempty"`
	StreetNumber         *string `json:"street_number,omitempty"`
	Street               *string `json:"street,omitempty"`
	City                 *string `json:"city,omitempty"`
	Country              *string `json:"country,omitempty"`
	ZipCode              *string `json:"zip_code,omitempty"`
	PlanningColor        *string `json:"planning_color,omitempty"`
	ProfilePicture       *string `json:"profile_picture,omitempty"`
	TermsOfUseAccepted   *bool   `json:"terms_of_use_accepted,omitempty"`
	WaiterDeviceToken    *string `json:"waiter_device_token,omitempty"`
	ReceptionDeviceToken *string `json:"reception_device_token,omitempty"`
	DeliveryDeviceToken  *string `json:"delivery_device_token,omitempty"`
}

type ScannorderSettings struct {
	MerchantID             *string `json:"merchant_id,omitempty"`
	Activated              *bool   `json:"activated,omitempty"`
	ShowAddress            *bool   `json:"show_address,omitempty"`
	HeaderBackground       *string `json:"header_background,omitempty"`
	HeaderBackgroundURL    *string `json:"header_background_url,omitempty"`
	HomePage               *bool   `json:"home_page,omitempty"`
	HomePageTitle          *string `json:"home_page_title,omitempty"`
	HomePageDesc           *string `json:"home_page_desc,omitempty"`
	InfoPopupEnabled       *bool   `json:"info_popup_enabled,omitempty"`
	ProductBgColor         *string `json:"product_bg_color,omitempty"`
	BtnColor               *string `json:"btn_color,omitempty"`
	BtnTextColor           *string `json:"btn_text_color,omitempty"`
	DeliveryType           *int    `json:"delivery_type,omitempty"`
	EnablePayments         *bool   `json:"enable_payments,omitempty"`
	InfoPopupTitle         *string `json:"info_popup_title,omitempty"`
	InfoPopupContent       *string `json:"info_popup_content,omitempty"`
	InfoPopupButtonContent *string `json:"info_popup_button_content,omitempty"`
	NavBGColor             *string `json:"nav_bg_color,omitempty"`
	BGColor                *string `json:"bg_color,omitempty"`
	ProductCategBGColor    *string `json:"product_categ_bg_color,omitempty"`
	ProductCategTextColor  *string `json:"product_categ_text_color,omitempty"`
	PopupBGColor           *string `json:"popup_bg_color,omitempty"`
	PopupTextColor         *string `json:"popup_text_color,omitempty"`
	ADTextColor            *string `json:"ad_text_color,omitempty"`
	HomeTextColor          *string `json:"home_text_color,omitempty"`
	ProductTextColor       *string `json:"product_text_color,omitempty"`
	DiscountColor          *string `json:"discount_color,omitempty"`
	DiscountTextColor      *string `json:"discount_text_color,omitempty"`
	BorderRadius           *string `json:"border_radius,omitempty"`
	VariableFees           *string `json:"variable_fees,omitempty"`
	FixedFees              *string `json:"fixed_fees,omitempty"`
	UsersDefaultName       *string `json:"users_default_name,omitempty"`
	SEOTitle               *string `json:"seo_title,omitempty"`
	SEODescription         *string `json:"seo_description,omitempty"`
	SEOKeywords            *string `json:"seo_keywords,omitempty"`
	SEOCuisineType         *string `json:"seo_cuisine_type,omitempty"`
	ShadowStyle            *string `json:"shadow_style,omitempty"`
}
type MerchantMarketingSettings struct {
	ID               string  `json:"id"`
	MerchantID       string  `json:"merchant_id"`
	SMSUnitPrice     string  `json:"sms_unit_price"`
	SMSEnabled       *bool   `json:"sms_enabled,omitempty"`
	EmailEnabled     *bool   `json:"email_enabled,omitempty"`
	SMSSenderName    *string `json:"sms_sender_name,omitempty"`
	EmailSenderName  *string `json:"email_sender_name,omitempty"`
	SMSTemplate      *string `json:"sms_template,omitempty"`
	EmailTemplate    *string `json:"email_template,omitempty"`
	TrackingTemplate *string `json:"tracking_template,omitempty"`
	MessaggioLogin   *string `json:"messaggio_login,omitempty"`
	MessaggioFrom    *string `json:"messaggio_from,omitempty"`
	CreatedAt        *string `json:"created_at,omitempty"`
	UpdatedAt        *string `json:"updated_at,omitempty"`
}
type MerchantParametersSettings struct {
	MerchantID                        string  `json:"merchant_id"`
	LastMenuUpdate                    *string `json:"last_menu_update,omitempty"`
	ManageOnSite                      *bool   `json:"manage_on_site,omitempty"`
	EnabledRating                     *bool   `json:"enabled_rating,omitempty"`
	ManageTakeAway                    *bool   `json:"manage_take_away,omitempty"`
	ManageDelivery                    *bool   `json:"manage_delivery,omitempty"`
	ConcurrentPreparationCapacity     *int    `json:"concurrent_preparation_capacity,omitempty"`
	DeliveryFees                      *int    `json:"delivery_fees,omitempty"`
	DeliveryFeesLimit                 *int    `json:"delivery_fees_limit,omitempty"`
	DeliveryDistanceLimit             *int    `json:"delivery_distance_limit,omitempty"`
	MinimumCartForDeliveryOrder       *int    `json:"minimum_cart_for_delivery_order,omitempty"`
	KitchenShowOnlyPaid               *bool   `json:"kitchen_show_only_paid,omitempty"`
	KitchenShowPendingApproval        *bool   `json:"kitchen_show_pending_approval,omitempty"`
	KitchenDistributionMode           *string `json:"kitchen_distribution_mode,omitempty"`
	ProductionDisplayMode             *string `json:"production_display_mode,omitempty"`
	MinimumPreparationTime            *int    `json:"minimum_preparation_time,omitempty"`
	MaximumPreparationTime            *int    `json:"maximum_preparation_time,omitempty"`
	DisableComponentsUnderSafetyStock *bool   `json:"disable_components_under_safety_stock,omitempty"`
	ServiceRequiredForOrdering        *bool   `json:"service_required_for_ordering,omitempty"`
	CashRegisterRequiredForOrdering   *bool   `json:"cash_register_required_for_ordering,omitempty"`
	WaiterAppCanCashIn                *bool   `json:"waiter_app_can_cash_in,omitempty"`
	WaiterAppCanClockIn               *bool   `json:"waiter_app_can_clock_in,omitempty"`
	AutoCompleteOrders                *bool   `json:"auto_complete_orders,omitempty"`
	AutoCompleteOrdersDelay           *int    `json:"auto_complete_orders_delay,omitempty"`
	AutoAcceptSnoDeliveryOrders       *bool   `json:"auto_accept_sno_delivery_orders,omitempty"`
	AutoAcceptSnoTakeAwayOrders       *bool   `json:"auto_accept_sno_take_away_orders,omitempty"`
	AutomaticallyAddCustomerRewards   *bool   `json:"automatically_add_customer_rewards,omitempty"`
	WarningNewOrderNotPaid            *bool   `json:"warning_new_order_not_paid,omitempty"`
	EnableAdvanceOrders               *bool   `json:"enable_advance_orders,omitempty"`
	AdvanceOrderDays                  *int    `json:"advance_order_days,omitempty"`
	PagerNumberRequired               *bool   `json:"pager_number_required,omitempty"`
	Currency                          *string `json:"currency,omitempty"`
	IsOpen                            *bool   `json:"is_open,omitempty"`
	PrimaryColor                      *string `json:"primary_color,omitempty"`
	TextColorOnPrimaryColor           *string `json:"text_color_on_primary_color,omitempty"`
	ZoningType                        *string `json:"zoning_type,omitempty"`
	RadialConeCount                   *string `json:"radial_cone_count,omitempty"`
	GridCellSizeKm                    *string `json:"grid_cell_size_km,omitempty"`
	RadialZoneRanges                  *string `json:"radial_zone_ranges,omitempty"`
	GridOriginLat                     *string `json:"grid_origin_lat,omitempty"`
	GridOriginLng                     *string `json:"grid_origin_lng,omitempty"`
	CardinalConeCount                 *string `json:"cardinal_cone_count,omitempty"`
	CardinalZoneRanges                *string `json:"cardinal_zone_ranges,omitempty"`
}
type MerchantSettings struct {
	MerchantID     *string  `json:"merchant_id,omitempty"`
	BusinessName   *string  `json:"business_name,omitempty"`
	Address        *string  `json:"address,omitempty"`
	StreetNumber   *string  `json:"street_number,omitempty"`
	Street         *string  `json:"street,omitempty"`
	ZipCode        *string  `json:"zip_code,omitempty"`
	City           *string  `json:"city,omitempty"`
	Country        *string  `json:"country,omitempty"`
	Lat            *float64 `json:"lat,omitempty"`
	Lng            *float64 `json:"lng,omitempty"`
	Timezone       *string  `json:"timezone,omitempty"`
	Logo           *string  `json:"logo,omitempty"`
	LogoURL        *string  `json:"logo_url,omitempty"`
	HandicapAccess *bool    `json:"handicap_access,omitempty"`
	SIRET          *string  `json:"SIRET,omitempty"`
	Email          *string  `json:"email,omitempty"`
	Phone          *string  `json:"phone,omitempty"`
	MerchantTel    *string  `json:"merchant_tel,omitempty"`
	IsActive       *bool    `json:"is_active,omitempty"`
	CreationDate   *string  `json:"creation_date,omitempty"`
	WebSite        *string  `json:"web_site,omitempty"`
}
type UpdateMerchantSettingsRequest struct {
	Merchant          *MerchantSettings           `json:"merchant,omitempty"`
	Parameters        *MerchantParametersSettings `json:"parameters,omitempty"`
	Marketing         *MerchantMarketingSettings  `json:"marketing,omitempty"`
	Scannorder        *ScannorderSettings         `json:"scannorder,omitempty"`
	Info              *POSSettingsInfoPatch       `json:"info,omitempty"`
	Timings           *POSSettingsTimingsPatch    `json:"timings,omitempty"`
	Ordering          *POSSettingsOrderingPatch   `json:"ordering,omitempty"`
	ScanOrder         *POSSettingsScanOrderPatch  `json:"scan_order,omitempty"`
	HoursOfOperations *[]POSHoursOfOperationPatch `json:"hours_of_operations,omitempty"`
}

type POSHoursOfOperation struct {
	ID               string  `json:"id"`
	DayOfWeekFrom    int     `json:"day_of_week_from"`
	DayOfWeekTo      int     `json:"day_of_week_to"`
	HourFrom         string  `json:"hour_from"`
	HourTo           string  `json:"hour_to"`
	BookingCapacity  *int    `json:"booking_capacity,omitempty"`
	FirstBookingTime *string `json:"first_booking_time,omitempty"`
	LastBookingTime  *string `json:"last_booking_time,omitempty"`
	ValidFrom        *string `json:"valid_from,omitempty"`
	ValidTo          *string `json:"valid_to,omitempty"`
	Enabled          bool    `json:"enabled"`
}

type POSHoursOfOperationPatch struct {
	ID               *string `json:"id,omitempty"`
	DayOfWeekFrom    int     `json:"day_of_week_from"`
	DayOfWeekTo      int     `json:"day_of_week_to"`
	HourFrom         string  `json:"hour_from"`
	HourTo           string  `json:"hour_to"`
	BookingCapacity  *int    `json:"booking_capacity,omitempty"`
	FirstBookingTime *string `json:"first_booking_time,omitempty"`
	LastBookingTime  *string `json:"last_booking_time,omitempty"`
	ValidFrom        *string `json:"valid_from,omitempty"`
	ValidTo          *string `json:"valid_to,omitempty"`
	Enabled          *bool   `json:"enabled,omitempty"`
}

type POSSettingsInfo struct {
	Name         string  `json:"name"`
	Phone        string  `json:"phone"`
	SIRET        string  `json:"siret"`
	Address      string  `json:"address"`
	Street       string  `json:"street"`
	City         string  `json:"city"`
	PostalCode   string  `json:"postal_code"`
	Country      string  `json:"country"`
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	Currency     string  `json:"currency"`
	PrimaryColor string  `json:"primary_color"`
	TextColor    string  `json:"text_color"`
	IsOpen       bool    `json:"is_open"`
}

type POSSettingsInfoPatch struct {
	Name         *string  `json:"name,omitempty"`
	Phone        *string  `json:"phone,omitempty"`
	SIRET        *string  `json:"siret,omitempty"`
	Address      *string  `json:"address,omitempty"`
	Street       *string  `json:"street,omitempty"`
	City         *string  `json:"city,omitempty"`
	PostalCode   *string  `json:"postal_code,omitempty"`
	Country      *string  `json:"country,omitempty"`
	Lat          *float64 `json:"lat,omitempty"`
	Lng          *float64 `json:"lng,omitempty"`
	Currency     *string  `json:"currency,omitempty"`
	PrimaryColor *string  `json:"primary_color,omitempty"`
	TextColor    *string  `json:"text_color,omitempty"`
	IsOpen       *bool    `json:"is_open,omitempty"`
}

type POSSettingsTimings struct {
	WaitTimeMin      int  `json:"wait_time_min"`
	WaitTimeMax      int  `json:"wait_time_max"`
	AutoCloseEnabled bool `json:"auto_close_enabled"`
	AutoCloseDelay   int  `json:"auto_close_delay"`
}

type POSSettingsTimingsPatch struct {
	WaitTimeMin      *int  `json:"wait_time_min,omitempty"`
	WaitTimeMax      *int  `json:"wait_time_max,omitempty"`
	AutoCloseEnabled *bool `json:"auto_close_enabled,omitempty"`
	AutoCloseDelay   *int  `json:"auto_close_delay,omitempty"`
}

type POSSettingsOrdering struct {
	PaidOrdersOnly     bool   `json:"paid_orders_only"`
	ConcurrentCapacity int    `json:"concurrent_capacity"`
	ServiceRequired    string `json:"service_required"`
	DisableLowStock    bool   `json:"disable_low_stock"`
	RegisterRequired   bool   `json:"register_required"`
	ActiveOnSite       bool   `json:"active_on_site"`
	ActiveTakeaway     bool   `json:"active_takeaway"`
	ActiveDelivery     bool   `json:"active_delivery"`
}

type POSSettingsOrderingPatch struct {
	PaidOrdersOnly     *bool   `json:"paid_orders_only,omitempty"`
	ConcurrentCapacity *int    `json:"concurrent_capacity,omitempty"`
	ServiceRequired    *string `json:"service_required,omitempty"`
	DisableLowStock    *bool   `json:"disable_low_stock,omitempty"`
	RegisterRequired   *bool   `json:"register_required,omitempty"`
	ActiveOnSite       *bool   `json:"active_on_site,omitempty"`
	ActiveTakeaway     *bool   `json:"active_takeaway,omitempty"`
	ActiveDelivery     *bool   `json:"active_delivery,omitempty"`
}

type POSSettingsScanOrder struct {
	ActiveDelivery     bool `json:"active_delivery"`
	ActiveTakeaway     bool `json:"active_takeaway"`
	ActiveOnSite       bool `json:"active_on_site"`
	AutoAcceptDelivery bool `json:"auto_accept_delivery"`
	AutoAcceptTakeaway bool `json:"auto_accept_takeaway"`
	AllowScheduled     bool `json:"allow_scheduled"`
	MaxScheduleDays    int  `json:"max_schedule_days"`
	EnableRating       bool `json:"enable_rating"`
}

type POSSettingsScanOrderPatch struct {
	ActiveDelivery     *bool `json:"active_delivery,omitempty"`
	ActiveTakeaway     *bool `json:"active_takeaway,omitempty"`
	ActiveOnSite       *bool `json:"active_on_site,omitempty"`
	AutoAcceptDelivery *bool `json:"auto_accept_delivery,omitempty"`
	AutoAcceptTakeaway *bool `json:"auto_accept_takeaway,omitempty"`
	AllowScheduled     *bool `json:"allow_scheduled,omitempty"`
	MaxScheduleDays    *int  `json:"max_schedule_days,omitempty"`
	EnableRating       *bool `json:"enable_rating,omitempty"`
}

type POSSettingsResponse struct {
	Info              POSSettingsInfo       `json:"info"`
	Timings           POSSettingsTimings    `json:"timings"`
	Ordering          POSSettingsOrdering   `json:"ordering"`
	ScanOrder         POSSettingsScanOrder  `json:"scan_order"`
	HoursOfOperations []POSHoursOfOperation `json:"hours_of_operations"`
}

type UserProfileResponse struct {
	FirstName     string  `json:"firstname"`
	LastName      string  `json:"lastname"`
	Email         string  `json:"email"`
	Phone         string  `json:"phone"`
	Address       string  `json:"address"`
	Street        string  `json:"street"`
	City          string  `json:"city"`
	PostalCode    string  `json:"postal_code"`
	Country       string  `json:"country"`
	Lat           float64 `json:"lat"`
	Lng           float64 `json:"lng"`
	Avatar        string  `json:"avatar"`
	MFAType       string  `json:"mfa_type"`
	EmailVerified bool    `json:"email_verified"`
	PhoneVerified bool    `json:"phone_verified"`
}

type UpdateUserProfileRequest struct {
	FirstName  *string  `json:"firstname,omitempty"`
	LastName   *string  `json:"lastname,omitempty"`
	Email      *string  `json:"email,omitempty"`
	Phone      *string  `json:"phone,omitempty"`
	Address    *string  `json:"address,omitempty"`
	Street     *string  `json:"street,omitempty"`
	City       *string  `json:"city,omitempty"`
	PostalCode *string  `json:"postal_code,omitempty"`
	Country    *string  `json:"country,omitempty"`
	Lat        *float64 `json:"lat,omitempty"`
	Lng        *float64 `json:"lng,omitempty"`
	MFAType    *string  `json:"mfa_type,omitempty"`
}

type UpdatePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type UpdateLocationRequest struct {
	UserID string  `json:"user_id"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
}

const (
	BrandUberEats   = "UBER_EATS"
	BrandDeliveroo  = "DELIVEROO"
	BrandWelloResto = "WELLO_RESTO"

	PaymentUberEats  = "UBER_EATS"
	PaymentDeliveroo = "DELIVEROO"
	PaymentStripe    = "STRIPE"
)

type OrderMeta struct {
	Brand        string
	MerchantID   string // kept as string to match your desired future ids
	BrandOrderID string
	CreationDate time.Time
}

type DenyOrderRequest struct {
	DeletionReasonID   string `json:"deletion_reason_id" binding:"required"`
	DeletionReasonType string `json:"deletion_reason_type" binding:"required"`
	DeletionComment    string `json:"deletion_comment"     binding:"required"`
	UserID             string `json:"user_id"              binding:"required"`
	MerchantID         string `json:"merchant_id"          binding:"required"`
}

type DenyOrderInput struct {
	OrderID            string
	DeletionReasonID   string
	DeletionReasonType string
	DeletionComment    string
	UserID             string
	MerchantID         string
}

type ReadyForDistributionInput struct {
	OrderID    string
	MerchantID string
	UserID     string
}

type ReadyForDistributionRequest struct {
	MerchantID string `json:"merchant_id" binding:"required"`
	UserID     string `json:"user_id"     binding:"required"`
}

type DeleteOrderRequest struct {
	MerchantID       int    `json:"merchant_id" binding:"required"`
	UserID           int    `json:"user_id" binding:"required"`
	DeletionReasonID int    `json:"deletion_reason_id" binding:"required"`
	DeletionComment  string `json:"deletion_comment"`
}
