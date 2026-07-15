package tasks

import (
	"context"
	stripeclient "welloresto-api/internal/infrastructure/stripe"

	"go.uber.org/zap"
)

const autoCaptureDelay = 720 // 12 heures en minutes

// CapturePayments : Valide les paiements Stripe différés
func (tm *TasksManager) CapturePayments() {
	query := `
		SELECT sp.payment_intent_id, sa.account_id
		FROM payments p
		INNER JOIN orders o on o.order_id = p.order_id
		INNER JOIN stripe_payments sp on sp.payment_id = p.payment_id
		INNER JOIN stripe_accounts sa on sa.merchant_id = p.merchant_id
		WHERE sp.payment_intent_status = 'REQUIRES_CONFIRMATION'
		AND o.state = 'CLOSED'
		AND o.brand_status NOT IN ('DENIED', 'CANCELED')
		AND sp.payment_intent_id IS NOT NULL
		AND TIMESTAMPDIFF(MINUTE, p.payment_date, UTC_TIMESTAMP) >= ?;`

	tm.processStripePayments(query, "CAPTURE")
}

// CancelPayments : Annule les empreintes bancaires si commande annulée
func (tm *TasksManager) CancelPayments() {
	query := `
		SELECT sp.payment_intent_id, sa.account_id
		FROM payments p
		INNER JOIN orders o on o.order_id = p.order_id
		INNER JOIN stripe_payments sp on sp.payment_id = p.payment_id
		INNER JOIN stripe_accounts sa on sa.merchant_id = p.merchant_id
		WHERE sp.payment_intent_status = 'REQUIRES_CONFIRMATION'
		AND o.state = 'CLOSED'
		AND o.brand_status IN ('DENIED', 'CANCELED')
		AND sp.payment_intent_id IS NOT NULL
		AND TIMESTAMPDIFF(MINUTE, p.payment_date, UTC_TIMESTAMP) >= ?;`

	tm.processStripePayments(query, "CANCEL")
}

// Helper pour éviter la duplication entre Capture et Cancel.
// Les paires (intent, account) sont collectées puis le rows fermé avant les
// appels Stripe : les Async ne touchent pas la DB mais on libère l'unique
// connexion du pool au plus tôt.
func (tm *TasksManager) processStripePayments(query string, action string) {
	ctx := context.Background()

	rows, err := tm.DB.QueryContext(ctx, query, autoCaptureDelay)
	if err != nil {
		tm.logError("[CRON] Stripe "+action+": requête échouée", zap.Error(err))
		return
	}

	type paymentRef struct{ intentID, accountID string }
	var payments []paymentRef
	for rows.Next() {
		var ref paymentRef
		if err := rows.Scan(&ref.intentID, &ref.accountID); err != nil {
			tm.logError("[CRON] Stripe "+action+": scan échoué", zap.Error(err))
			continue
		}
		payments = append(payments, ref)
	}
	if err := rows.Err(); err != nil {
		tm.logError("[CRON] Stripe "+action+": itération interrompue", zap.Error(err))
	}
	rows.Close()

	for _, ref := range payments {
		if action == "CAPTURE" {
			tm.logInfo("[CRON] Stripe: tentative de capture", zap.String("intent_id", ref.intentID))
			tm.StripeService.CaptureExistingPaymentAsync(stripeclient.PaymentRequest{
				IntentID:  ref.intentID,
				AccountID: ref.accountID,
			})
		} else {
			tm.logInfo("[CRON] Stripe: tentative de refund/cancel", zap.String("intent_id", ref.intentID))
			tm.StripeService.RefundOrCancelAsync(stripeclient.RefundRequest{
				IntentID:  ref.intentID,
				AccountID: ref.accountID,
			})
		}
	}
}
