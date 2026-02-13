package tasks

import (
	"log"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
)

const autoCaptureDelay = 720 // 12 heures en minutes

// CapturePayments : Valide les paiements Stripe différés
func (tm *TasksManager) CapturePayments() {
	//log.Println("[CRON] Démarrage: CapturePayments")

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

	//log.Println("[CRON] Terminé: CapturePayments")
}

// CancelPayments : Annule les empreintes bancaires si commande annulée
func (tm *TasksManager) CancelPayments() {
	//log.Println("[CRON] Démarrage: CancelPayments")

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

	//log.Println("[CRON] Terminé: CancelPayments")
}

// Helper pour éviter la duplication entre Capture et Cancel
func (tm *TasksManager) processStripePayments(query string, action string) {
	rows, err := tm.DB.Query(query, autoCaptureDelay)
	if err != nil {
		log.Printf("[CRON] Erreur Stripe Query: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var intentID, accountID string
		if err := rows.Scan(&intentID, &accountID); err != nil {
			continue
		}

		if action == "CAPTURE" {
			req := stripeclient.PaymentRequest{
				IntentID:  intentID,
				AccountID: accountID,
			}
			log.Printf("[CRON] Capturing " + intentID)
			// Adapter selon la signature réelle de ton StripeService
			tm.StripeService.ProcessPaymentAsync(req)
		} else {
			req := stripeclient.RefundRequest{
				IntentID:  intentID,
				AccountID: accountID,
			}
			log.Printf("[CRON] Refunding " + intentID)
			tm.StripeService.RefundAsync(req)
		}
	}
}
