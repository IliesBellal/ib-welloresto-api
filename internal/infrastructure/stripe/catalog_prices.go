package stripeclient

import (
	"github.com/stripe/stripe-go/v84"
)

// CreateRecurringPrice creates a Stripe Product + Price in one call (a new
// monthly recurring EUR price) — LOT B B2c-0's "les créer une fois en mode
// test et documenter leurs identifiants": called only by
// cmd/ensure_stripe_prices, never by request-serving code, and only when
// pricing_catalog has no stripe_price_id yet for that row.
func (s *StripeManager) CreateRecurringPrice(productName string, unitAmountCents int, code string) (*stripe.Price, error) {
	params := &stripe.PriceParams{
		Currency:    stripe.String("eur"),
		UnitAmount:  stripe.Int64(int64(unitAmountCents)),
		Recurring:   &stripe.PriceRecurringParams{Interval: stripe.String("month")},
		ProductData: &stripe.PriceProductDataParams{Name: stripe.String(productName)},
		LookupKey:   stripe.String("welloresto_" + code),
	}
	params.Metadata = map[string]string{"code": code}
	return s.client.Prices.New(params)
}
