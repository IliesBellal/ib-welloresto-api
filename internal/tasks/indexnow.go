package tasks

import (
	"context"

	"go.uber.org/zap"
)

// IndexNowRunner signale aux moteurs IndexNow les pages ScanNOrder modifiées
// (scannorder.IndexNowNotifier).
type IndexNowRunner interface {
	Run(ctx context.Context) (int, error)
}

// RunIndexNow : toutes les 15 minutes (docs/scannorder-seo.md). Sans
// INDEXNOW_KEY le notifier ne fait rien : la tâche reste enregistrée sur tous
// les environnements, mais seule la prod signale ses pages.
func (tm *TasksManager) RunIndexNow() {
	if tm.IndexNow == nil {
		return
	}
	n, err := tm.IndexNow.Run(context.Background())
	if err != nil {
		tm.logError("[CRON] RunIndexNow: signalement échoué (renvoyé au prochain passage)", zap.Error(err))
		return
	}
	if n > 0 {
		tm.logInfo("[CRON] RunIndexNow: pages signalées", zap.Int("urls", n))
	}
}
