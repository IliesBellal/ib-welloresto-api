// Package billing owns the "WelloResto bills the merchant" relationship
// (LOT B B2a) — platform_billing_customers (which Stripe Customer a
// merchant is billed through) and sepa_mandates (the accepted SEPA
// mandate). Deliberately separate from welloresto_stripe_customers /
// stripe_accounts, which are about the merchant's OWN Stripe Connect
// account taking payments from its end customers — a different Stripe
// relationship entirely (see docs/decisions.md, LOT B B2a investigation).
package billing

import "time"

// BillingCustomer is one row of platform_billing_customers.
type BillingCustomer struct {
	ID                   string    `json:"id"`
	MerchantID           string    `json:"merchant_id"`
	StripeCustomerID     string    `json:"stripe_customer_id"`
	IsPrimaryForMerchant bool      `json:"is_primary_for_merchant"`
	CreatedAt            time.Time `json:"created_at"`
}

// MandateStatusActive is the only status this chantier ever writes —
// sepa_mandates.status has no closed-set validation (not specified as one
// by the brief, unlike subscription_overrides.reason), so no ValidX map
// exists here; kept as a named const purely so callers don't repeat the
// literal string.
const MandateStatusActive = "active"

// SepaMandate is one row of sepa_mandates.
type SepaMandate struct {
	ID                    string     `json:"id"`
	MerchantID            string     `json:"merchant_id"`
	StripePaymentMethodID string     `json:"stripe_payment_method_id"`
	Status                string     `json:"status"`
	Last4IBANMasked       *string    `json:"last4_iban_masked,omitempty"`
	AcceptedAt            *time.Time `json:"accepted_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}
