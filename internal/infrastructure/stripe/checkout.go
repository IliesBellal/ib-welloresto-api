package stripeclient

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
	"welloresto-api/internal/helpers"

	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/checkout/session"
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

		description := product.ProductName
		var configurationPrice int = 0

		if product.Config != nil {
			if product.Config.Attributes != nil {
				for _, a := range product.Config.Attributes {
					attr := a
					for _, o := range attr.Options {
						option := o

						if !option.Selected {
							continue
						}

						if option.Label != nil {
							description += *option.Label
						}

						if option.ExtraPrice > 0 {
							description += fmt.Sprintf("(+%.2f EUR)", float64(option.ExtraPrice)/100)
							configurationPrice += option.ExtraPrice
						}
						description += ", "
					}
				}
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
					Description: &description,
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
	baseURL := req.BaseURL
	sessionType := req.CheckoutSessionType

	successURL := baseURL + "/restaurant/" + qrCode + "/" + orderID
	cancelURL := successURL
	captureMethod := stripe.PaymentIntentCaptureMethodManual

	if sessionType == "partial_order" {
		successURL = baseURL + "restaurant/" + qrCode
		cancelURL = successURL
		captureMethod = stripe.PaymentIntentCaptureMethodAutomatic
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
		},
	}

	params.SetStripeAccount(*merchant.AccountID)

	return c.client.CheckoutSessions.New(params)
}

func (c *StripeManager) CreateCheckoutSessionOld(req map[string]interface{}) (*stripe.CheckoutSession, error) {

	order := req["order"].(map[string]interface{})
	merchant := req["merchant"].(map[string]interface{})

	variableFees := merchant["variable_fees"].(float64)
	fixedFees := merchant["fixed_fees"].(float64)

	ttc := order["TTC"].(float64)

	fees := int64((ttc * variableFees) + fixedFees + 0.5) // round half up

	lineItems := []*stripe.CheckoutSessionLineItemParams{}
	orderItems := []CheckoutOrderItem{}

	products := order["products"].([]interface{})

	for _, p := range products {
		product := p.(map[string]interface{})

		description := ""
		var configurationPrice float64 = 0

		if cfg, ok := product["configuration"].(map[string]interface{}); ok {
			if attrs, ok := cfg["attributes"].([]interface{}); ok {
				for _, a := range attrs {
					attr := a.(map[string]interface{})
					for _, o := range attr["options"].([]interface{}) {
						option := o.(map[string]interface{})

						selected := true
						if s, ok := option["selected"].(bool); ok {
							selected = s
						}
						if !selected {
							continue
						}

						description += option["title"].(string)

						if extra, ok := option["extra_price"].(float64); ok && extra > 0 {
							description += fmt.Sprintf("(+%.2f EUR)", extra/100)
							configurationPrice += extra
						}
						description += ", "
					}
				}
			}
		}

		var unitAmount float64
		if dp, ok := product["discounted_price"].(float64); ok && product["price"] != nil {
			unitAmount = dp
		} else {
			unitAmount = product["price"].(float64)
		}

		qty := int64(product["quantity"].(float64))

		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			Quantity: stripe.Int64(qty),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(string(stripe.CurrencyEUR)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name:        stripe.String(product["product_name"].(string)),
					Description: stripe.String(description),
				},
				UnitAmount: stripe.Int64(int64(unitAmount + (configurationPrice * float64(qty)))),
			},
		})

		orderItems = append(orderItems, CheckoutOrderItem{
			OrderItemID: toInt64Ptr(product["order_item_id"]),
			Quantity:    &qty,
		})
	}

	// Delivery fees
	if df, ok := order["delivery_fees"].(float64); ok && df > 0 {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(string(stripe.CurrencyEUR)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String("Frais de livraison"),
				},
				UnitAmount: stripe.Int64(int64(df)),
			},
		})
	}

	orderItemsJSON, _ := json.Marshal(orderItems)

	qrCode := req["qr_code"].(string)
	orderID := fmt.Sprintf("%v", order["order_id"])
	baseURL := req["redirect_base_url"].(string)
	sessionType := req["checkout_session_type"].(string)

	successURL := baseURL + "restaurant/" + qrCode + "/" + orderID
	cancelURL := successURL
	captureMethod := stripe.PaymentIntentCaptureMethodManual

	if sessionType == "partial_order" {
		successURL = baseURL + "restaurant/" + qrCode
		cancelURL = successURL
		captureMethod = stripe.PaymentIntentCaptureMethodAutomatic
	}

	params := &stripe.CheckoutSessionParams{
		LineItems:  lineItems,
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		ExpiresAt:  stripe.Int64(time.Now().Add(30 * time.Minute).Unix()),
		Metadata: map[string]string{
			"order_id":              orderID,
			"merchant_id":           fmt.Sprintf("%v", merchant["id"]),
			"checkout_session_type": sessionType,
			"order_items":           string(orderItemsJSON),
		},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			ApplicationFeeAmount: stripe.Int64(fees),
			CaptureMethod:        stripe.String(string(captureMethod)),
		},
	}

	params.SetStripeAccount(merchant["account_id"].(string))

	return session.New(params)
}

func toInt64Ptr(v interface{}) *int64 {
	if v == nil {
		return nil
	}
	val := int64(v.(float64))
	return &val
}
