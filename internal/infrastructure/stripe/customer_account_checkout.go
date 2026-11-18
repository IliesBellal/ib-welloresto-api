package stripeclient

import (
	"fmt"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v84"
)

// CustomerAccountCheckout : paiement en ligne de l'ardoise d'un client
// (docs/customer-accounts.md), sur le compte Connect de l'établissement.
type CustomerAccountCheckout struct {
	StripeAccountID string
	Amount          int64
	ApplicationFee  int64
	Description     string
	SuccessURL      string
	CancelURL       string
	Metadata        map[string]string
}

// CreateCustomerAccountCheckout crée la session Stripe Checkout d'un
// règlement de compte client et renvoie son adresse. La session vit une
// heure : le lien envoyé au client en recrée une à chaque clic.
func (c *StripeManager) CreateCustomerAccountCheckout(p CustomerAccountCheckout) (string, error) {
	if strings.TrimSpace(p.StripeAccountID) == "" {
		return "", fmt.Errorf("customer account checkout: no stripe connected account")
	}
	if p.Amount <= 0 {
		return "", fmt.Errorf("customer account checkout: invalid amount %d", p.Amount)
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
		ExpiresAt:  stripe.Int64(time.Now().Add(time.Hour).Unix()),
		Metadata:   p.Metadata,
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			CaptureMethod: stripe.String(string(stripe.PaymentIntentCaptureMethodAutomatic)),
			// Recopiées sur le PaymentIntent et sa charge : le webhook ignore
			// leurs événements (channel=customer_account).
			Metadata: p.Metadata,
		},
	}
	if p.ApplicationFee > 0 {
		params.PaymentIntentData.ApplicationFeeAmount = stripe.Int64(p.ApplicationFee)
	}
	params.SetStripeAccount(p.StripeAccountID)
	session, err := c.client.CheckoutSessions.New(params)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}
