package stripeclient

import (
	"fmt"
	"runtime/debug"

	"github.com/stripe/stripe-go/v84"
)

// ProcessPaymentAsync gère un paiement sans bloquer le thread principal.
func (s *StripeManager) ProcessPaymentAsync(req PaymentRequest) {
	// 1. Lancement de la Goroutine (Asynchrone)
	go func() {
		// 2. Protection contre les Panics (Crashs)
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("CRITICAL: Panic recovered in Stripe ProcessPayment",
					"error", r,
					"stack", string(debug.Stack()),
				)
			}
		}()

		// 3. Validation des champs obligatoires
		if err := s.validatePaymentRequest(req); err != nil {
			s.logger.Error("Validation failed for payment", "order_id", req.OrderID, "error", err)
			return
		}

		s.logger.Info("Starting async Stripe payment", "order_id", req.OrderID)

		// 4. Appel à l'API Stripe
		params := &stripe.PaymentIntentParams{
			Amount:   stripe.Int64(req.Amount),
			Currency: stripe.String(req.Currency),
			Customer: stripe.String(req.CustomerID),
			Metadata: map[string]string{
				"order_id": req.OrderID, // Lien vers ton système interne
			},
			// AutomaticPaymentMethods permet à Stripe de gérer les méthodes configurées
			AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
				Enabled: stripe.Bool(true),
			},
		}

		pi, err := s.client.PaymentIntents.New(params)
		if err != nil {
			// Log de l'erreur sans crasher
			s.logger.Error("Stripe API Error during payment",
				"order_id", req.OrderID,
				"stripe_error", err,
			)
			// TODO: Ici, tu pourrais appeler un service interne pour marquer la commande en "Erreur Paiement"
			return
		}

		s.logger.Info("Stripe PaymentIntent created successfully",
			"order_id", req.OrderID,
			"payment_intent_id", pi.ID,
		)

	}() // Fin de la goroutine
}

// RefundAsync gère le remboursement de manière asynchrone.
func (s *StripeManager) RefundAsync(req RefundRequest) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("CRITICAL: Panic recovered in Stripe Refund", "error", r)
			}
		}()

		if req.ChargeID == "" || req.Amount <= 0 {
			s.logger.Error("Invalid refund request parameters", "charge_id", req.ChargeID)
			return
		}

		params := &stripe.RefundParams{
			Charge: stripe.String(req.ChargeID),
			Amount: stripe.Int64(req.Amount),
		}

		refund, err := s.client.Refunds.New(params)
		if err != nil {
			s.logger.Error("Stripe Refund Failed", "charge_id", req.ChargeID, "error", err)
			return
		}

		s.logger.Info("Refund processed", "refund_id", refund.ID)
	}()
}

// validatePaymentRequest est une méthode privée pour valider les données
func (s *StripeManager) validatePaymentRequest(req PaymentRequest) error {
	if req.Amount <= 0 {
		return fmt.Errorf("amount must be positive")
	}
	if req.Currency == "" {
		return fmt.Errorf("currency is required")
	}
	if req.CustomerID == "" {
		return fmt.Errorf("customer ID is required")
	}
	return nil
}
