package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/stripe/stripe-go/v78"
)

// Paiement par QR code depuis la caisse : le webhook est la seule source de
// vérité. checkout.session.completed (channel=pos_qr) enregistre le paiement
// STRIPE sur la commande et prévient la caisse (notification de commande, qui
// ferme son QR code). Il n'applique rien du flux ScanNOrder : date de création,
// statut, lignes payées, acceptation automatique, e-mails et SMS.

// posQRLifecycle : ce que le webhook demande au cycle de vie des commandes
// (order_life_cycle.OrdersLifeCycleService).
type posQRLifecycle interface {
	OrderStillOpen(ctx context.Context, orderID string) (bool, error)
	CreatePayment(ctx context.Context, payment models.Payment) error
}

// posQRStripe : appels Stripe du webhook de la caisse (stripeclient.StripeManager).
type posQRStripe interface {
	RefundOrCancelAsync(req stripeclient.RefundRequest)
	PaymentFees(accountID, paymentIntentID string) (wrFees, stripeFees, total int64, err error)
}

// SetPOSQRStripe branche le client Stripe : frais du paiement et remboursement
// d'un paiement qu'on ne peut pas enregistrer.
func (s *StripeWebhookService) SetPOSQRStripe(m posQRStripe) {
	s.posQRStripe = m
}

// posQREvent : l'objet de l'événement porte channel=pos_qr.
func posQREvent(data json.RawMessage) bool {
	var obj struct {
		Metadata map[string]string `json:"metadata"`
	}
	return json.Unmarshal(data, &obj) == nil && obj.Metadata["channel"] == stripeclient.POSQRPaymentChannel
}

// HandlePOSQRCheckoutCompleted enregistre le paiement d'une session de la
// caisse. accountID est le compte Connect de l'événement.
func (s *StripeWebhookService) HandlePOSQRCheckoutCompleted(ctx context.Context, data json.RawMessage, accountID string) error {
	log := logger.FromContext(ctx)

	var session stripe.CheckoutSession
	if err := json.Unmarshal(data, &session); err != nil {
		return fmt.Errorf("unmarshal session: %w", err)
	}
	if session.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		log.Info("[stripe webhook] pos_qr session " + session.ID + " not paid, ignored")
		return nil
	}

	merchantID := session.Metadata["merchant_id"]
	orderID := session.Metadata["order_id"]
	if merchantID == "" || orderID == "" {
		return errors.New("missing metadata in stripe session")
	}
	userID := session.Metadata["user_id"]
	if userID == "" {
		userID = models.StripeWebhookUserID
	}
	piID := ""
	if session.PaymentIntent != nil {
		piID = session.PaymentIntent.ID
	}

	refund := func(reason string) {
		log.Warn("[stripe webhook] pos_qr payment " + piID + " not recorded on order " + orderID + " (" + reason + "), refunding")
		if s.posQRStripe != nil && piID != "" && accountID != "" {
			s.posQRStripe.RefundOrCancelAsync(stripeclient.RefundRequest{IntentID: piID, AccountID: accountID})
		}
	}

	// Commande close ou annulée depuis l'affichage du QR code : même règle que
	// la saisie d'un paiement (AddPayment), rien n'y est ajouté.
	open, err := s.posQRLifecycle.OrderStillOpen(ctx, orderID)
	if err != nil {
		return err
	}
	if !open {
		refund("order closed")
		return nil
	}

	var email *string
	if session.CustomerDetails != nil && session.CustomerDetails.Email != "" {
		email = &session.CustomerDetails.Email
	}
	err = s.posQRLifecycle.CreatePayment(ctx, models.Payment{
		CashRegisterID:    session.Metadata["cash_register_id"],
		MOP:               models.StripeMOP,
		Amount:            int(session.AmountTotal),
		OrderID:           orderID,
		MerchantID:        merchantID,
		UserID:            userID,
		OperationType:     models.OperationTypeSale,
		PaymentIntentID:   &piID,
		CheckoutSessionID: &session.ID,
		CustomerEmail:     email,
	})
	var overpaid *models.OrderNotFullyPaidError
	if errors.As(err, &overpaid) {
		// Une autre part a soldé la commande entre-temps : le client est remboursé.
		refund("order already paid")
		return nil
	}
	if err != nil {
		return err
	}

	// Frais réels du paiement (fee / net_amount), au mieux : le paiement est
	// déjà enregistré, un échec ici ne doit pas faire rejouer l'événement.
	if s.posQRStripe != nil && piID != "" && accountID != "" {
		wr, stripeFees, total, feeErr := s.posQRStripe.PaymentFees(accountID, piID)
		if feeErr == nil {
			feeErr = s.repo.UpdateFees(ctx, piID, wr, stripeFees, total)
		}
		if feeErr != nil {
			log.Warn("[stripe webhook] pos_qr fees not recorded for " + piID + ": " + feeErr.Error())
		}
	}
	return nil
}
