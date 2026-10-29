package stripeclient

import (
	"fmt"
	"math"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/stripe/stripe-go/v84"
)

type CreateCheckoutParams struct {
	OrderRequest map[string]interface{}
}

func (c *StripeManager) CreateCheckoutSession(req CheckoutSessionRequestObject) (*stripe.CheckoutSession, error) {

	order := req.Order
	merchant := req.Merchant

	variableFees := *merchant.VariableFees
	fixedFees := *merchant.FixedFees

	ttc := order.TTC

	fees := int64(math.Floor(
		float64(ttc)*variableFees + float64(fixedFees) + 0.5,
	))

	lineItems := []*stripe.CheckoutSessionLineItemParams{}
	//orderItems := []CheckoutOrderItem{}

	products := order.Products

	for _, p := range products {
		product := p

		options := selectedOptions(product.Config)

		configurationPrice := 0
		for _, option := range options {
			if option.ExtraPrice > 0 {
				configurationPrice += option.ExtraPrice
			}
		}

		var unitAmount int
		if product.DiscountedPrice != nil {
			unitAmount = *product.DiscountedPrice
		} else {
			unitAmount = product.Price
		}

		qty := product.Quantity

		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			Quantity: helpers.IntToInt64Ptr(qty),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(string(stripe.CurrencyEUR)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name:        &product.ProductName,
					Description: optionsDescription(options),
				},
				UnitAmount: stripe.Int64(int64((unitAmount + configurationPrice))),
			},
		})
		/*
			orderItems = append(orderItems, CheckoutOrderItem{
				OrderItemID: toInt64Ptr(product["order_item_id"]),
				Quantity:    &qty,
			})

		*/
	}

	// Delivery fees
	if order.DeliveryFees > 0 {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(string(stripe.CurrencyEUR)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String("Frais de livraison"),
				},
				UnitAmount: stripe.Int64(int64(order.DeliveryFees)),
			},
		})
	}

	//orderItemsJSON, _ := json.Marshal(orderItems)

	qrCode := req.QRCode
	orderID := *order.OrderID
	sessionType := req.CheckoutSessionType

	// Retour client vers la page de suivi : id public uniquement, l'id interne
	// ne reste que dans les metadata lues par le webhook (docs/
	// SCANNORDER_PUBLIC_ORDER_ID.md, D7).
	trackingURL := helpers.ScanNOrderOrderURL(req.BaseURL, qrCode, helpers.SafeString(order.PublicID))
	successURL := trackingURL
	cancelURL := trackingURL
	// ?payment= distingue le retour payé du retour sans paiement : la page de
	// suivi n'affiche le bouton « Payer » que dans le second cas (le webhook
	// peut arriver après le retour du client).
	if trackingURL != "" {
		successURL = trackingURL + "?payment=success"
		cancelURL = trackingURL + "?payment=canceled"
	}
	captureMethod := stripe.PaymentIntentCaptureMethodManual

	if sessionType == "partial_order" {
		successURL = helpers.ScanNOrderBaseURL(req.BaseURL) + "/restaurant/" + qrCode
		cancelURL = successURL
		captureMethod = stripe.PaymentIntentCaptureMethodAutomatic
	}

	if successURL == "" {
		return nil, fmt.Errorf("checkout session: missing public id or QR code for order %s", orderID)
	}

	params := &stripe.CheckoutSessionParams{
		LineItems:  lineItems,
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		ExpiresAt:  stripe.Int64(time.Now().Add(30 * time.Minute).Unix()),
		Metadata: map[string]string{
			"order_id":              orderID,
			"merchant_id":           fmt.Sprintf("%v", merchant.MerchantID),
			"checkout_session_type": sessionType,
		},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			ApplicationFeeAmount: stripe.Int64(fees),
			CaptureMethod:        stripe.String(string(captureMethod)),
			// Métadonnées du PaymentIntent (donc du paiement, visibles dans le
			// dashboard Stripe et dans les lignes d'un payout) ; celles de la
			// session ci-dessus ne se propagent pas jusqu'à lui. Même forme
			// que le PaymentIntent Terminal de la borne (channel=kiosk).
			Metadata: map[string]string{
				"order_id":    orderID,
				"merchant_id": fmt.Sprintf("%v", merchant.MerchantID),
				"channel":     "scannorder",
			},
		},
	}

	params.SetStripeAccount(*merchant.AccountID)

	return c.client.CheckoutSessions.New(params)
}

// selectedOptions renvoie les options cochées de la configuration d'un produit.
func selectedOptions(config *models.ProductConfiguration) []models.ConfigurationOption {
	if config == nil {
		return nil
	}

	var selected []models.ConfigurationOption
	for _, attr := range config.Attributes {
		for _, option := range attr.Options {
			if option.Selected {
				selected = append(selected, option)
			}
		}
	}
	return selected
}

// optionsDescription liste les options choisies avec leur supplément éventuel,
// ex. "Frites, Coca (+1,50 €)". Renvoie nil sans option à afficher : Stripe
// refuse une description vide.
func optionsDescription(options []models.ConfigurationOption) *string {
	parts := make([]string, 0, len(options))
	for _, option := range options {
		if option.Label == nil || strings.TrimSpace(*option.Label) == "" {
			continue
		}

		part := strings.TrimSpace(*option.Label)
		if option.ExtraPrice > 0 {
			part += " (+" + formatEuros(option.ExtraPrice) + ")"
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return nil
	}
	return stripe.String(strings.Join(parts, ", "))
}

// formatEuros formate un montant en centimes à la française : 150 -> "1,50 €".
func formatEuros(cents int) string {
	return strings.Replace(fmt.Sprintf("%.2f €", float64(cents)/100), ".", ",", 1)
}

func toInt64Ptr(v interface{}) *int64 {
	if v == nil {
		return nil
	}
	val := int64(v.(float64))
	return &val
}
