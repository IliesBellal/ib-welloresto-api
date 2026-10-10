package stripeclient

import (
	"encoding/json"
	"testing"

	"github.com/stripe/stripe-go/v84"
)

// Ligne de balance réelle d'un compte connecté (paiement borne, mode test) :
// la commission de la plateforme et les frais Stripe sont deux entrées de
// fee_details, la source est dépliée (expand data.source).
const chargeBalanceTransaction = `{
  "id": "txn_3U6XjGISGuDm6FEV1FsIcZO6", "object": "balance_transaction",
  "type": "charge", "reporting_category": "charge", "status": "available",
  "amount": 720, "fee": 51, "net": 669, "created": 1790000000, "currency": "eur",
  "fee_details": [
    {"amount": 20, "currency": "eur", "description": "WELLO RESTO SAS application fee", "type": "application_fee"},
    {"amount": 31, "currency": "eur", "description": "Stripe processing fees", "type": "stripe_fee"}
  ],
  "source": {"id": "ch_1", "object": "charge", "payment_intent": "pi_3U6XjGISGuDm6FEV1VlkFXPF", "metadata": {"channel": "kiosk"}}
}`

func TestPayoutTransactionFrom_SplitsCommissionFromStripeFees(t *testing.T) {
	var bt stripe.BalanceTransaction
	if err := json.Unmarshal([]byte(chargeBalanceTransaction), &bt); err != nil {
		t.Fatal(err)
	}

	tx := payoutTransactionFrom(&bt)

	if tx.CommissionFee != 20 || tx.StripeFee != 31 {
		t.Errorf("commission %d / frais Stripe %d, want 20 / 31", tx.CommissionFee, tx.StripeFee)
	}
	if tx.Amount-tx.CommissionFee-tx.StripeFee != tx.Net {
		t.Errorf("montant %d - commission %d - frais %d != net %d", tx.Amount, tx.CommissionFee, tx.StripeFee, tx.Net)
	}
	if tx.PaymentIntentID != "pi_3U6XjGISGuDm6FEV1VlkFXPF" {
		t.Errorf("PaymentIntent = %q", tx.PaymentIntentID)
	}
	if tx.Type != "charge" || tx.ReportingCategory != "charge" {
		t.Errorf("type = %q / %q", tx.Type, tx.ReportingCategory)
	}
}

func TestPayoutTransactionFrom_RefundSourceAndNoFeeDetails(t *testing.T) {
	var bt stripe.BalanceTransaction
	raw := `{"id": "txn_r", "object": "balance_transaction", "type": "refund", "reporting_category": "refund",
	  "amount": -300, "fee": -6, "net": -294, "created": 1790000000, "fee_details": [],
	  "source": {"id": "re_1", "object": "refund", "payment_intent": "pi_refunded"}}`
	if err := json.Unmarshal([]byte(raw), &bt); err != nil {
		t.Fatal(err)
	}

	tx := payoutTransactionFrom(&bt)

	// sans fee_details, tout le fee est rangé en frais Stripe : le net reste juste
	if tx.CommissionFee != 0 || tx.StripeFee != -6 || tx.Net != -294 {
		t.Errorf("tx = %+v", tx)
	}
	if tx.PaymentIntentID != "pi_refunded" {
		t.Errorf("PaymentIntent = %q", tx.PaymentIntentID)
	}
}

func TestPayoutTransactionFrom_UnexpandedSource(t *testing.T) {
	var bt stripe.BalanceTransaction
	if err := json.Unmarshal([]byte(`{"id": "txn_x", "type": "adjustment", "amount": -100, "fee": 0, "net": -100, "source": "du_123"}`), &bt); err != nil {
		t.Fatal(err)
	}
	if tx := payoutTransactionFrom(&bt); tx.PaymentIntentID != "" {
		t.Errorf("PaymentIntent = %q, want vide", tx.PaymentIntentID)
	}
}
