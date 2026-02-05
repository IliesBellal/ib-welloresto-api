package stripeclient

import "github.com/stripe/stripe-go/v84"

// StripeProvider définit les méthodes disponibles pour l'extérieur.
type StripeProvider interface {
	// ProcessPaymentAsync lance le paiement en arrière-plan.
	ProcessPaymentAsync(req PaymentRequest)
	// RefundAsync lance un remboursement en arrière-plan.
	RefundAsync(req RefundRequest)
	// Tu pourras ajouter: CreateCustomerAsync, etc.
	CreateCheckoutSession(req map[string]interface{}) (*stripe.CheckoutSession, error)
}
