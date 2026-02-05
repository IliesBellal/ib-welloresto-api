package stripeclient

type CheckoutOrderItem struct {
	OrderItemID *int64 `json:"order_item_id,omitempty"`
	Quantity    *int64 `json:"quantity,omitempty"`
}

// PaymentRequest représente les données nécessaires pour initier un paiement.
// C'est ici que tes modules passeront les infos (ex: depuis internal/modules/orders).
type PaymentRequest struct {
	OrderID       string
	CustomerID    string
	Amount        int64  // En centimes (ex: 1000 = 10.00€)
	Currency      string // ex: "eur"
	PaymentMethod string
}

// RefundRequest représente une demande de remboursement.
type RefundRequest struct {
	ChargeID string
	Amount   int64
	Reason   string
}
