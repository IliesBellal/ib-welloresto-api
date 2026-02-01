package deliveroo

// DeliverooPayload représente le corps du webhook
type DeliverooPayload struct {
	EventName string         `json:"event_name"`
	Order     DeliverooOrder `json:"order"`
}

type DeliverooOrder struct {
	ID              string             `json:"id"`
	DisplayID       string             `json:"display_id"`
	Status          string             `json:"status"`
	LocationID      string             `json:"location_id"`
	FulfillmentType string             `json:"fulfillment_type"`
	ASAP            bool               `json:"asap"` // Nouveau champ requis pour l'update
	PrepareFor      string             `json:"prepare_for"`
	OrderNotes      string             `json:"order_notes"`
	CutleryNotes    string             `json:"cutlery_notes"`
	TotalPrice      DeliverooMoney     `json:"total_price"`
	CashDue         DeliverooMoney     `json:"cash_due"`
	Items           []DeliverooItem    `json:"items"`
	Customer        *DeliverooCustomer `json:"customer"` // Pour le Delivery by Deliveroo
	Delivery        *DeliverooDelivery `json:"delivery"` // Pour le Delivery by Restaurant
	RemakeDetails   *RemakeDetails     `json:"remake_details"`
}

type RemakeDetails struct {
	ParentOrderID string `json:"parent_order_id"`
}

type DeliverooCustomer struct {
	FirstName         string `json:"first_name"`
	ContactNumber     string `json:"contact_number"`
	ContactAccessCode string `json:"contact_access_code"`
}

type DeliverooDelivery struct {
	CustomerName      string            `json:"customer_name"`
	ContactNumber     string            `json:"contact_number"`
	ContactAccessCode string            `json:"contact_access_code"`
	Line1             string            `json:"line1"`
	City              string            `json:"city"`
	PostalCode        string            `json:"postal_code"`
	Location          DeliverooLocation `json:"location"`
}

type DeliverooLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type DeliverooMoney struct {
	Fractional int `json:"fractional"`
}

type DeliverooItem struct {
	PosItemID       string              `json:"pos_item_id"`
	Name            string              `json:"name"`
	OperationalName string              `json:"operational_name"`
	Quantity        int                 `json:"quantity"`
	UnitPrice       DeliverooMoney      `json:"unit_price"`
	TotalPrice      DeliverooMoney      `json:"total_price"`
	DiscountAmount  DeliverooMoney      `json:"discount_amount"`
	Modifiers       []DeliverooModifier `json:"modifiers"`
}

type DeliverooModifier struct {
	PosItemID string         `json:"pos_item_id"`
	Name      string         `json:"name"`
	Quantity  int            `json:"quantity"`
	UnitPrice DeliverooMoney `json:"unit_price"`
}

// Struct pour l'API Sync Status de Deliveroo
type SyncStatusRequest struct {
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	OccurredAt string `json:"occurred_at"`
}
