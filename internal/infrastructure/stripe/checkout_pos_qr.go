package stripeclient

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v84"
)

// POSQRPaymentChannel : métadonnée « channel » des sessions et paiements Stripe
// initiés depuis la caisse (bouton « QR code » de la calculatrice). Le webhook
// les reconnaît ainsi et n'applique pas le flux ScanNOrder.
const POSQRPaymentChannel = "pos_qr"

// POSQRMinAmount : montant minimal d'un paiement Stripe en euros (centimes).
const POSQRMinAmount = 50

// posQRSessionTTL : validité d'une session. Stripe impose au moins 30 minutes
// entre la création et l'expiration ; la marge couvre le délai d'appel.
const posQRSessionTTL = 40 * time.Minute

// POSQRCheckout décrit la session de paiement d'une part (ou de la totalité)
// d'une commande de caisse, sur le compte Connect de l'établissement.
type POSQRCheckout struct {
	StripeAccountID string
	Amount          int64
	ApplicationFee  int64
	Description     string
	SuccessURL      string
	CancelURL       string
	Metadata        map[string]string
}

// CreatePOSQRCheckout crée la session Stripe Checkout d'un paiement par QR code
// de la caisse : une seule ligne au montant demandé (la commande entière n'est
// pas facturée, le client peut ne payer qu'une part), capture automatique.
func (c *StripeManager) CreatePOSQRCheckout(p POSQRCheckout) (*stripe.CheckoutSession, error) {
	if strings.TrimSpace(p.StripeAccountID) == "" {
		return nil, fmt.Errorf("pos qr checkout: no stripe connected account")
	}
	if p.Amount < POSQRMinAmount {
		return nil, fmt.Errorf("pos qr checkout: invalid amount %d", p.Amount)
	}
	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(string(stripe.CurrencyEUR)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(p.Description),
				},
				UnitAmount: stripe.Int64(p.Amount),
			},
		}},
		SuccessURL: stripe.String(p.SuccessURL),
		CancelURL:  stripe.String(p.CancelURL),
		ExpiresAt:  stripe.Int64(time.Now().Add(posQRSessionTTL).Unix()),
		Metadata:   p.Metadata,
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			CaptureMethod: stripe.String(string(stripe.PaymentIntentCaptureMethodAutomatic)),
			// Copiées sur le PaymentIntent (dashboard Stripe, lignes de payout).
			Metadata: p.Metadata,
		},
	}
	if p.ApplicationFee > 0 {
		params.PaymentIntentData.ApplicationFeeAmount = stripe.Int64(p.ApplicationFee)
	}
	params.SetStripeAccount(p.StripeAccountID)
	return c.client.CheckoutSessions.New(params)
}

// PaymentFees lit les frais d'un paiement déjà encaissé : part WelloResto
// (application_fee), frais Stripe et total. Une capture automatique n'émet pas
// charge.captured (qui alimente fee/net_amount des paiements ScanNOrder), le
// webhook de la caisse les relit donc ici.
func (c *StripeManager) PaymentFees(accountID, paymentIntentID string) (wrFees, stripeFees, total int64, err error) {
	params := &stripe.PaymentIntentParams{}
	params.AddExpand("latest_charge.balance_transaction")
	params.SetStripeAccount(accountID)
	pi, err := c.client.PaymentIntents.Get(paymentIntentID, params)
	if err != nil {
		return 0, 0, 0, err
	}
	if pi.LatestCharge == nil || pi.LatestCharge.BalanceTransaction == nil {
		return 0, 0, 0, fmt.Errorf("payment intent %s: no balance transaction yet", paymentIntentID)
	}
	bt := pi.LatestCharge.BalanceTransaction
	for _, f := range bt.FeeDetails {
		switch f.Type {
		case "application_fee":
			wrFees += f.Amount
		case "stripe_fee":
			stripeFees += f.Amount
		}
	}
	return wrFees, stripeFees, bt.Fee, nil
}

// ApplicationFee : frais ScanNOrder de l'établissement sur un paiement en ligne
// (variable × montant + fixe, arrondi ; jamais plus que le montant).
func ApplicationFee(amount int64, variable float64, fixed int64) int64 {
	if variable <= 0 && fixed <= 0 {
		return 0
	}
	fee := int64(math.Floor(float64(amount)*variable + float64(fixed) + 0.5))
	if fee < 0 {
		return 0
	}
	if fee >= amount {
		return amount - 1
	}
	return fee
}
