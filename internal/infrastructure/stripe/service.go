package stripeclient

import (
	"context"
	"fmt"
	"runtime/debug"
	"welloresto-api/internal/logger"

	"github.com/stripe/stripe-go/v84"
)

// CaptureExistingPaymentAsync : Reproduit la fonction PHP capturePayment
// Elle capture une autorisation existante sur un compte connecté.
func (s *StripeManager) CaptureExistingPaymentAsync(req PaymentRequest) {
	go func() {
		log := logger.FromContext(context.Background())
		defer func() {
			if r := recover(); r != nil {
				log.Error("CRITICAL: Panic recovered in Stripe Capture " + string(debug.Stack()))
			}
		}()

		if req.IntentID == "" || req.AccountID == "" {
			log.Error("Missing IntentID or AccountID for capture")
			return
		}

		log.Info("Starting async Stripe capture for intent " + req.IntentID)

		// Configuration des paramètres pour la capture
		params := &stripe.PaymentIntentCaptureParams{}
		// Équivalent PHP: ['stripe_account' => $payment->account_id]
		params.SetStripeAccount(req.AccountID)

		// Appel API Stripe : Capture
		pi, err := s.client.PaymentIntents.Capture(req.IntentID, params)
		if err != nil {
			log.Error("Stripe Capture Failed : intent_id - " + req.IntentID + " error " + err.Error())
			return
		}

		// Vérification du statut comme en PHP ($captured_payment_intent->status == 'succeeded')
		if pi.Status == stripe.PaymentIntentStatusSucceeded {
			log.Info("Stripe Payment captured successfully : intent_id " + pi.ID)
		} else {
			log.Warn("Stripe Payment captured but status is " + string(pi.Status) + " : intent_id " + pi.ID)
		}
	}()
}

// RefundOrCancelAsync : Reproduit la logique intelligente de ton PHP refundPayment et cancelPayment.
// Elle vérifie le statut : si non capturé -> Cancel, si capturé -> Refund.
func (s *StripeManager) RefundOrCancelAsync(req RefundRequest) {
	go func() {
		log := logger.FromContext(context.Background())
		defer func() {
			if r := recover(); r != nil {
				log.Error("CRITICAL: Panic recovered in Stripe RefundOrCancel")
			}
		}()

		if req.IntentID == "" || req.AccountID == "" {
			log.Error("Missing IntentID or AccountID for refund/cancel")
			return
		}

		// 1. Récupérer le PaymentIntent pour vérifier son statut
		// Équivalent PHP: $stripe->paymentIntents->retrieve(...)
		getParams := &stripe.PaymentIntentParams{}
		getParams.SetStripeAccount(req.AccountID)

		pi, err := s.client.PaymentIntents.Get(req.IntentID, getParams)
		if err != nil {
			log.Error("Stripe Retrieve Failed : intent_id " + req.IntentID + " error " + err.Error())
			return
		}

		// 2. Logique de décision (Comme en PHP)
		// Si le paiement nécessite une capture ou confirmation, on l'annule simplement (Cancel)
		if pi.Status == stripe.PaymentIntentStatusRequiresCapture || pi.Status == stripe.PaymentIntentStatusRequiresConfirmation {
			log.Info("Payment not captured yet (" + string(pi.Status) + "), cancelling intent: " + req.IntentID)

			cancelParams := &stripe.PaymentIntentCancelParams{}
			cancelParams.SetStripeAccount(req.AccountID)

			_, err := s.client.PaymentIntents.Cancel(req.IntentID, cancelParams)
			if err != nil {
				log.Error("Stripe Cancel Failed : intent_id " + req.IntentID + " error " + err.Error())
				return
			}
			log.Info("PaymentIntent cancelled successfully: " + req.IntentID)
			return
		}

		// 3. Sinon, si c'est déjà capturé (Succeeded), on rembourse la Charge associée (Refund)
		if pi.LatestCharge != nil {
			log.Info("Payment captured, refunding charge: " + pi.LatestCharge.ID)

			refundParams := &stripe.RefundParams{
				Charge:               stripe.String(pi.LatestCharge.ID),
				RefundApplicationFee: stripe.Bool(true), // Équivalent PHP: 'refund_application_fee' => true
			}
			refundParams.SetStripeAccount(req.AccountID)

			refund, err := s.client.Refunds.New(refundParams)
			if err != nil {
				log.Error("Stripe Refund Failed : charge_id " + pi.LatestCharge.ID + " error " + err.Error())
				return
			}
			log.Info("Refund processed successfully : refund_id " + refund.ID)
		} else {
			log.Error("Cannot refund: No charge found on PaymentIntent " + req.IntentID)
		}
	}()
}

// ProcessPaymentAsync gère un paiement sans bloquer le thread principal.
func (s *StripeManager) ProcessPaymentAsync(req PaymentRequest) {
	// 1. Lancement de la Goroutine (Asynchrone)
	go func() {
		log := logger.FromContext(context.Background())
		// 2. Protection contre les Panics (Crashs)
		defer func() {
			if r := recover(); r != nil {
				log.Error("CRITICAL: Panic recovered in Stripe ProcessPayment " + string(debug.Stack()))
			}
		}()

		// 3. Validation des champs obligatoires
		if err := s.validatePaymentRequest(req); err != nil {
			log.Error("Validation failed for payment : order_id - " + req.OrderID + " error " + err.Error())
			return
		}

		log.Info("Starting async Stripe payment order_id " + req.OrderID)

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
			log.Error("Stripe API error : order_id - " + req.OrderID + " error " + err.Error())
			// TODO: appeler un service interne pour marquer la commande en "Erreur Paiement"
			return
		}

		log.Info("Stripe PaymentIntent created successfully : order_id" + req.OrderID + " . payment_intent_id " + pi.ID)

	}() // Fin de la goroutine
}

// RefundAsync gère le remboursement de manière asynchrone.
func (s *StripeManager) RefundAsync(req RefundRequest) {
	go func() {
		log := logger.FromContext(context.Background())

		defer func() {
			if r := recover(); r != nil {
				log.Error("CRITICAL: Panic recovered in Stripe Refund error")
			}
		}()

		if req.ChargeID == "" || req.Amount <= 0 {
			log.Error("Invalid refund request parameters charge_id: " + req.ChargeID)
			return
		}

		params := &stripe.RefundParams{
			Charge: stripe.String(req.ChargeID),
			Amount: stripe.Int64(req.Amount),
		}

		refund, err := s.client.Refunds.New(params)
		if err != nil {
			log.Error("Stripe Refund Failed charge_id: " + req.ChargeID + " error " + err.Error())
			return
		}

		log.Info("Refund processed : refund_id " + refund.ID)
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
