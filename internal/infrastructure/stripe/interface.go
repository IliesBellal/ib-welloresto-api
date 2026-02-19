package stripeclient

import (
	"welloresto-api/internal/models"

	"github.com/stripe/stripe-go/v84"
)

type CheckoutSessionRequestObject struct {
	QRCode              string               `json:"qr_code"`
	Order               *models.OrderRequest `json:"order"`
	Merchant            *models.MerchantRow  `json:"merchant,omitempty"`
	BaseURL             string               `json:"base_url"`
	CheckoutSessionType string               `json:"checkout_session_type"`
}

// StripeProvider définit les méthodes disponibles pour l'extérieur.
type StripeProvider interface {
	// ProcessPaymentAsync lance le paiement en arrière-plan.
	ProcessPaymentAsync(req PaymentRequest)
	// RefundAsync lance un remboursement en arrière-plan.
	RefundAsync(req RefundRequest)
	// Tu pourras ajouter: CreateCustomerAsync, etc.
	CreateCheckoutSession(req map[string]interface{}) (*stripe.CheckoutSession, error)
}
