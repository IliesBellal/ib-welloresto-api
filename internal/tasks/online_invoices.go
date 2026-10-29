package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/fiscal"

	"go.uber.org/zap"
)

const (
	onlineInvoicesRunBudget = 4 * time.Minute
	// onlineInvoicesTaskLock : une seule instance envoie les factures à la
	// fois, pour ne jamais envoyer deux fois la même.
	onlineInvoicesTaskLock = "orders:online-invoices-task"
)

// RunOnlineOrderInvoices envoie la facture PDF des commandes en ligne closes,
// payées par Stripe, d'au moins 25 € TTC (docs/emails-styles.md, « Facture
// automatique »).
func (tm *TasksManager) RunOnlineOrderInvoices() {
	if tm.OrderService == nil {
		tm.logError("[CRON] RunOnlineOrderInvoices: service des commandes indisponible")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), onlineInvoicesRunBudget)
	defer cancel()

	release, ok, err := fiscal.TryLock(ctx, tm.DB, onlineInvoicesTaskLock)
	if err != nil {
		tm.logError("[CRON] RunOnlineOrderInvoices: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunOnlineOrderInvoices: une autre instance envoie les factures")
		return
	}
	defer release()

	res, err := tm.OrderService.SendOnlineOrderInvoices(ctx, time.Now())
	if err != nil {
		tm.logError("[CRON] RunOnlineOrderInvoices: lecture des commandes échouée", zap.Error(err))
		return
	}
	if res.Sent+res.Failed > 0 {
		tm.logInfo("[CRON] RunOnlineOrderInvoices: terminé", zap.Int("envoyees", res.Sent), zap.Int("echecs", res.Failed))
	}
}
