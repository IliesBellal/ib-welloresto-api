package deliveroo

// =======================
// ROOT PAYLOAD
// =======================

type DeliverooWebhookPayload struct {
	Event string               `json:"event"`
	Body  DeliverooWebhookBody `json:"body"`
}

type DeliverooWebhookBody struct {
	Order DeliverooOrder `json:"order"`
}

// =======================
// ORDER
// =======================

type DeliverooOrder struct {
	ID              string               `json:"id"`
	OrderNumber     string               `json:"order_number"`
	LocationID      string               `json:"location_id"`
	BrandID         string               `json:"brand_id"`
	DisplayID       string               `json:"display_id"`
	Status          string               `json:"status"`
	StatusLog       []DeliverooStatusLog `json:"status_log"`
	FulfillmentType string               `json:"fulfillment_type"`
	OrderNotes      string               `json:"order_notes"`
	CutleryNotes    string               `json:"cutlery_notes"`
	ASAP            bool                 `json:"asap"`
	PrepareFor      string               `json:"prepare_for"`
	TableNumber     string               `json:"table_number"`

	Subtotal             DeliverooMoney `json:"subtotal"`
	TotalPrice           DeliverooMoney `json:"total_price"`
	PartnerOrderSubtotal DeliverooMoney `json:"partner_order_subtotal"`
	PartnerOrderTotal    DeliverooMoney `json:"partner_order_total"`
	OfferDiscount        DeliverooMoney `json:"offer_discount"`
	CashDue              DeliverooMoney `json:"cash_due"`
	BagFee               DeliverooMoney `json:"bag_fee"`
	Surcharge            DeliverooMoney `json:"surcharge"`

	Delivery      *DeliverooDelivery      `json:"delivery"`
	FeeBreakdown  []DeliverooFeeBreakdown `json:"fee_breakdown"`
	Items         []DeliverooItem         `json:"items"`
	Promotions    []DeliverooPromotion    `json:"promotions"`
	RemakeDetails *DeliverooRemakeDetails `json:"remake_details"`

	IsTabletless bool                `json:"is_tabletless"`
	MealCards    []DeliverooMealCard `json:"meal_cards"`
	Customer     *DeliverooCustomer  `json:"customer"`

	StartPreparingAt string `json:"start_preparing_at"`
	ConfirmAt        string `json:"confirm_at"`
}

// =======================
// STATUS LOG
// =======================

type DeliverooStatusLog struct {
	At     string `json:"at"`
	Status string `json:"status"`
}

// =======================
// DELIVERY
// =======================

type DeliverooDelivery struct {
	DeliveryFee       DeliverooMoney    `json:"delivery_fee"`
	DeliveryNotes     string            `json:"delivery_notes"`
	Line1             string            `json:"line1"`
	Line2             string            `json:"line2"`
	City              string            `json:"city"`
	Postcode          string            `json:"postcode"`
	ContactNumber     string            `json:"contact_number"`
	ContactAccessCode string            `json:"contact_access_code"`
	DeliverBy         string            `json:"deliver_by"`
	CustomerName      string            `json:"customer_name"`
	Location          DeliverooLocation `json:"location"`
}

type DeliverooLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// =======================
// ITEMS
// =======================

type DeliverooItem struct {
	PosItemID       string              `json:"pos_item_id"`
	Name            string              `json:"name"`
	OperationalName string              `json:"operational_name"`
	UnitPrice       DeliverooMoney      `json:"unit_price"`
	MenuUnitPrice   DeliverooMoney      `json:"menu_unit_price"`
	TotalPrice      DeliverooMoney      `json:"total_price"`
	Quantity        int                 `json:"quantity"`
	ItemFees        []DeliverooItemFee  `json:"item_fees"`
	Modifiers       []DeliverooModifier `json:"modifiers"`
	DiscountAmount  DeliverooMoney      `json:"discount_amount"`
}

type DeliverooModifier struct {
	PosItemID       string             `json:"pos_item_id"`
	Name            string             `json:"name"`
	OperationalName string             `json:"operational_name"`
	Quantity        int                `json:"quantity"`
	UnitPrice       DeliverooMoney     `json:"unit_price"`
	MenuUnitPrice   DeliverooMoney     `json:"menu_unit_price"`
	TotalPrice      DeliverooMoney     `json:"total_price"`
	DiscountAmount  DeliverooMoney     `json:"discount_amount"`
	ItemFees        []DeliverooItemFee `json:"item_fees"`
}

type DeliverooItemFee struct {
	Type        string         `json:"type"`
	CostPerUnit DeliverooMoney `json:"cost_per_unit"`
}

// =======================
// FEES
// =======================

type DeliverooFeeBreakdown struct {
	Type         string         `json:"type"`
	Amount       DeliverooMoney `json:"amount"`
	BundleAmount DeliverooMoney `json:"bundle_amount"`
	Quantity     int            `json:"quantity"`
}

// =======================
// PROMOTIONS
// =======================

type DeliverooPromotion struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Value      int                      `json:"value"`
	Amount     DeliverooMoney           `json:"amount"`
	PosItemIDs []DeliverooPromotionItem `json:"pos_item_ids"`
}

type DeliverooPromotionItem struct {
	ID string `json:"id"`
}

// =======================
// CUSTOMER
// =======================

type DeliverooCustomer struct {
	FirstName            string            `json:"first_name"`
	ContactNumber        string            `json:"contact_number"`
	ContactAccessCode    string            `json:"contact_access_code"`
	OrderFrequencyAtSite string            `json:"order_frequency_at_site"`
	Loyalty              *DeliverooLoyalty `json:"loyalty"`
}

type DeliverooLoyalty struct {
	LoyaltyID string `json:"loyalty_id"`
}

// =======================
// REMAKE
// =======================

type DeliverooRemakeDetails struct {
	ParentOrderID string `json:"parent_order_id"`
	Fault         string `json:"fault"`
	OrderCost     int    `json:"order_cost"`
}

// =======================
// MEAL CARDS
// =======================

type DeliverooMealCard struct {
	Provider string `json:"provider"`
	Amount   int    `json:"amount"`
}

// =======================
// MONEY
// =======================

type DeliverooMoney struct {
	Fractional   int    `json:"fractional"`
	CurrencyCode string `json:"currency_code"`
}

// Struct pour l'API Sync Status de Deliveroo
type SyncStatusRequest struct {
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	OccurredAt string `json:"occurred_at"`
}
