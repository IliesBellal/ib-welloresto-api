package stripe

import (
	"encoding/json"
	"time"
)

// Structure globale de l'événement Webhook
type StripeEvent struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Account string `json:"account"` // IMPORTANT : L'ID du compte connecté (acct_...)
	Data    struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// Structure spécifique pour un Payout (Virement)
type Payout struct {
	ID                  string `json:"id"`
	Amount              int64  `json:"amount"`       // En centimes
	ArrivalDate         int64  `json:"arrival_date"` // Timestamp
	Status              string `json:"status"`
	Destination         string `json:"destination"`
	StatementDescriptor string `json:"statement_descriptor"`
}

// Model léger pour le Merchant dans le contexte du Payout
type PayoutMerchant struct {
	Email        string
	BusinessName string
}

type Payment struct {
	MerchantID  string
	OrderID     string
	Amount      int64 // En centimes
	PaymentDate time.Time
}

type StripePayment struct {
	OrderID           string
	PaymentID         int64
	PaymentIntentID   string
	CheckoutSessionID string
	CustomerEmail     string
	WelloRestoFees    int64
	StripeFees        int64
}

type Order struct {
	OrderID      string
	Price        float64
	CreationDate time.Time
	CustomerID   *int64 // Nullable
}

type Merchant struct {
	ID                 string
	BusinessName       string
	Timezone           string
	Currency           string
	Code               string
	LogoURL            string
	AutoAcceptDelivery bool
	AutoAcceptTakeaway bool
}

type Customer struct {
	ID      int64
	Name    string
	Email   string
	Address string
}
