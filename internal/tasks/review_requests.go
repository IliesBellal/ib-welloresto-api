package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/modules/reviewrequests"

	"go.uber.org/zap"
)

const (
	reviewRequestsRunBudget = 4 * time.Minute
	// reviewRequestsTaskLock : une seule instance envoie à la fois, pour
	// qu'aucun client ne reçoive deux demandes.
	reviewRequestsTaskLock = "scannorder:review-requests-task"
)

// ReviewRequestsRunner envoie les demandes d'avis dues (reviewrequests.Service).
type ReviewRequestsRunner interface {
	Run(ctx context.Context) (reviewrequests.RunResult, error)
}

// RunReviewRequests : toutes les 15 minutes (docs/scannorder-seo.md). Seuls
// les établissements qui ont activé la demande d'avis sont concernés.
func (tm *TasksManager) RunReviewRequests() {
	if tm.ReviewRequests == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reviewRequestsRunBudget)
	defer cancel()

	release, ok, err := fiscal.TryLock(ctx, tm.DB, reviewRequestsTaskLock)
	if err != nil {
		tm.logError("[CRON] RunReviewRequests: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunReviewRequests: une autre instance envoie les demandes")
		return
	}
	defer release()

	res, err := tm.ReviewRequests.Run(ctx)
	if reviewrequests.IsConfigError(err) {
		tm.logDebug("[CRON] RunReviewRequests: mailer ou SCANNORDER_BASE_URL absent")
		return
	}
	if err != nil {
		tm.logError("[CRON] RunReviewRequests: lecture des commandes échouée", zap.Error(err))
		return
	}
	if res.Sent+res.Skipped+res.Failed > 0 {
		tm.logInfo("[CRON] RunReviewRequests: terminé",
			zap.Int("envoyees", res.Sent), zap.Int("ignorees", res.Skipped), zap.Int("echecs", res.Failed))
	}
}
