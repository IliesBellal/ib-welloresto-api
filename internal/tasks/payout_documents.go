package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/fiscal"

	"go.uber.org/zap"
)

const (
	// payoutDocumentsRunBudget borne un passage horaire.
	payoutDocumentsRunBudget = 10 * time.Minute
	// payoutDocumentsTaskLock : une seule instance traite les payouts à la
	// fois (les autres passent leur tour), pour ne pas envoyer deux fois le
	// même mail.
	payoutDocumentsTaskLock = "payouts:documents-task"
)

// RunPayoutDocuments produit et envoie le relevé de versement et la facture de
// commission des payouts Stripe payés (docs/payouts-justificatifs.md). Les
// payouts arrivent par le webhook payout.paid ; un payout que Stripe n'a pas
// fini de rapprocher est simplement retenté au passage suivant. Rattrapage des
// payouts passés : cmd/backfill_payout_documents.
func (tm *TasksManager) RunPayoutDocuments() {
	if tm.PayoutService == nil {
		tm.logError("[CRON] RunPayoutDocuments: service des justificatifs indisponible")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), payoutDocumentsRunBudget)
	defer cancel()

	release, ok, err := fiscal.TryLock(ctx, tm.DB, payoutDocumentsTaskLock)
	if err != nil {
		tm.logError("[CRON] RunPayoutDocuments: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunPayoutDocuments: une autre instance traite les payouts")
		return
	}
	defer release()

	res, err := tm.PayoutService.ProcessPending(ctx)
	if err != nil {
		tm.logError("[CRON] RunPayoutDocuments: lecture des payouts échouée", zap.Error(err))
		return
	}
	if res.Done+res.Retry+res.Failed > 0 {
		tm.logInfo("[CRON] RunPayoutDocuments: terminé",
			zap.Int("envoyes", res.Done), zap.Int("a_retenter", res.Retry), zap.Int("abandonnes", res.Failed))
	}
}
