package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/fiscal"

	"go.uber.org/zap"
)

const (
	// accountingSyncRunBudget borne un passage horaire.
	accountingSyncRunBudget = 20 * time.Minute
	// accountingSyncTaskLock : une seule instance envoie à la fois (les autres
	// passent leur tour) — l'API Pennylane n'a pas d'idempotence, deux
	// instances en parallèle créeraient des doublons.
	accountingSyncTaskLock = "accounting-sync:task"
)

// RunAccountingSync envoie, pour chaque établissement relié à un logiciel de
// comptabilité, l'écriture de ventes de chaque clôture fiscale journalière pas
// encore envoyée (docs/accounting-sync.md).
func (tm *TasksManager) RunAccountingSync() {
	if tm.AccountingSyncService == nil {
		tm.logError("[CRON] RunAccountingSync: service de synchronisation comptable indisponible")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), accountingSyncRunBudget)
	defer cancel()

	release, ok, err := fiscal.TryLock(ctx, tm.DB, accountingSyncTaskLock)
	if err != nil {
		tm.logError("[CRON] RunAccountingSync: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunAccountingSync: une autre instance synchronise")
		return
	}
	defer release()

	res, err := tm.AccountingSyncService.RunAll(ctx)
	if err != nil {
		tm.logError("[CRON] RunAccountingSync: lecture des connexions échouée", zap.Error(err))
		return
	}
	if res.Sent+res.Failed+res.Uncertain > 0 {
		tm.logInfo("[CRON] RunAccountingSync: terminé",
			zap.Int("connexions", res.Connections), zap.Int("envoyes", res.Sent),
			zap.Int("en_echec", res.Failed), zap.Int("a_verifier", res.Uncertain))
	}
}
