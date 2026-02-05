package stripe

import "time"

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
