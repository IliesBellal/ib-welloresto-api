package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/fiscal"

	"go.uber.org/zap"
)

const (
	// subscriptionInvoiceRunBudget borne un passage horaire.
	subscriptionInvoiceRunBudget = 10 * time.Minute
	// subscriptionInvoiceTaskLock : une seule instance envoie à la fois (les
	// autres passent leur tour), pour ne pas envoyer deux fois la même facture.
	subscriptionInvoiceTaskLock = "subscription-invoices:documents-task"
)

// RunSubscriptionInvoiceDocuments envoie au restaurateur la facture Stripe de
// chaque paiement d'abonnement (docs/payouts-justificatifs.md, « Factures
// d'abonnement »). Les factures arrivent par le webhook invoice.paid ; une
// facture dont l'envoi échoue est retentée au passage suivant. Rattrapage des
// factures passées : cmd/backfill_subscription_invoices.
func (tm *TasksManager) RunSubscriptionInvoiceDocuments() {
	if tm.SubscriptionInvoiceService == nil {
		tm.logError("[CRON] RunSubscriptionInvoiceDocuments: service des factures indisponible")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), subscriptionInvoiceRunBudget)
	defer cancel()

	release, ok, err := fiscal.TryLock(ctx, tm.DB, subscriptionInvoiceTaskLock)
	if err != nil {
		tm.logError("[CRON] RunSubscriptionInvoiceDocuments: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunSubscriptionInvoiceDocuments: une autre instance envoie les factures")
		return
	}
	defer release()

	res, err := tm.SubscriptionInvoiceService.ProcessPending(ctx)
	if err != nil {
		tm.logError("[CRON] RunSubscriptionInvoiceDocuments: lecture des factures échouée", zap.Error(err))
		return
	}
	if res.Done+res.Retry+res.Failed > 0 {
		tm.logInfo("[CRON] RunSubscriptionInvoiceDocuments: terminé",
			zap.Int("envoyees", res.Done), zap.Int("a_retenter", res.Retry), zap.Int("abandonnees", res.Failed))
	}
}
