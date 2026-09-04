package analytics

// Canonical payment-method keys. The real payments.mop column carries 14
// distinct raw values on PROD (DROITS.md/AUDIT.md P14) — 7 genuine payment
// methods plus commercial-gesture markers stored as if they were payment
// methods (PERCENTAGE, DISCOUNT), a webhook artifact (STRIPE_WEB_HOOK), and
// a stray '1'. None of the 14 is "mobile" — the back-office maquette's
// "paiement mobile" filter does not correspond to anything in this schema;
// do not invent a mapping to it here.
const (
	PaymentMethodCB        = "CB"
	PaymentMethodES        = "ES"
	PaymentMethodStripe    = "STRIPE"
	PaymentMethodTR        = "TR"
	PaymentMethodCurrency  = "CURRENCY"
	PaymentMethodUberEats  = "UBER_EATS"
	PaymentMethodDeliveroo = "DELIVEROO"
	// PaymentMethodOther covers every raw mop value outside the 7 canonical
	// ones — never dropped silently, same pattern as ChannelUnknown.
	PaymentMethodOther = "other"
)

// PaymentMethods lists every canonical key in a fixed display order.
var PaymentMethods = []string{
	PaymentMethodCB,
	PaymentMethodES,
	PaymentMethodStripe,
	PaymentMethodTR,
	PaymentMethodCurrency,
	PaymentMethodUberEats,
	PaymentMethodDeliveroo,
	PaymentMethodOther,
}

// paymentMethodCaseExpr is the single SQL derivation of a canonical payment
// method from payments.mop, aliased `p`. Matched as-is (no upper()): the 7
// canonical raw values are already uppercase on PROD; if a lowercase variant
// ever shows up it falls into "other" rather than silently miscounting under
// a canonical bucket — verify against staging (docs/analytics/ Phase 2)
// before assuming case never varies here the way brand_status does.
const paymentMethodCaseExpr = `
	CASE
		WHEN p.mop IN ('CB', 'ES', 'STRIPE', 'TR', 'CURRENCY', 'UBER_EATS', 'DELIVEROO') THEN p.mop
		ELSE 'other'
	END
`
