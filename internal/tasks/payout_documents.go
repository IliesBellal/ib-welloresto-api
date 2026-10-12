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

// RunPayoutDocuments produit et envoie le relevé de versement des payouts
// Stripe payés (docs/payouts-justificatifs.md). Les
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

// commissionInvoicesTaskLock : une seule instance émet et envoie les factures
// mensuelles à la fois (les autres passent leur tour).
const commissionInvoicesTaskLock = "payouts:commission-invoices-task"

// RunCommissionInvoices émet la facture de commission et le récapitulatif du
// mois écoulé pour chaque établissement, à partir du 3 du mois, puis envoie par
// mail les factures émises dont l'envoi n'a pas encore abouti. Indépendant du
// rythme des virements : la facture additionne les virements non encore
// facturés arrivés avant le début du mois (docs/payouts-justificatifs.md).
func (tm *TasksManager) RunCommissionInvoices() {
	if tm.PayoutService == nil {
		tm.logError("[CRON] RunCommissionInvoices: service des justificatifs indisponible")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), payoutDocumentsRunBudget)
	defer cancel()

	release, ok, err := fiscal.TryLock(ctx, tm.DB, commissionInvoicesTaskLock)
	if err != nil {
		tm.logError("[CRON] RunCommissionInvoices: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunCommissionInvoices: une autre instance traite les factures")
		return
	}
	defer release()

	res, err := tm.PayoutService.RunMonthlyInvoices(ctx)
	if err != nil {
		tm.logError("[CRON] RunCommissionInvoices: échec", zap.Error(err))
		return
	}
	if res.Issued+res.Sent+res.Retry+res.Failed > 0 {
		tm.logInfo("[CRON] RunCommissionInvoices: terminé",
			zap.Int("emises", res.Issued), zap.Int("envoyees", res.Sent), zap.Int("a_retenter", res.Retry), zap.Int("abandonnees", res.Failed))
	}
}
