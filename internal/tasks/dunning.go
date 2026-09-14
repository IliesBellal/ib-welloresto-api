package tasks

import (
	"context"

	"go.uber.org/zap"
)

// RunDunningCascade — LOT B B2b-1 : ré-évalue chaque merchant actuellement
// past_due et agit selon son état réel (jamais un envoi planifié à
// l'avance) — voir dunning.Service.RunCascade.
func (tm *TasksManager) RunDunningCascade() {
	if tm.DunningService == nil {
		tm.logWarn("[CRON] RunDunningCascade: service indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	errs := tm.DunningService.RunCascade(ctx)
	for _, err := range errs {
		tm.logError("[CRON] RunDunningCascade: erreur sur un marchand", zap.Error(err))
	}
	if len(errs) == 0 {
		tm.logInfo("[CRON] RunDunningCascade: terminé sans erreur")
	}
}

// RunTrialExpiryCheck — LOT B B2c-1 : rappels J-7/J-1 et révocation/retour
// SETUP des dérogations 'price' à échéance de trial. Appelé depuis le même
// créneau @hourly que RunDunningCascade (cmd/api/tasks.go) — pas un second
// enregistrement cron, voir docs/decisions.md.
func (tm *TasksManager) RunTrialExpiryCheck() {
	if tm.SubscriptionsService == nil {
		tm.logWarn("[CRON] RunTrialExpiryCheck: service indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	errs := tm.SubscriptionsService.RunTrialExpiryCheck(ctx)
	for _, err := range errs {
		tm.logError("[CRON] RunTrialExpiryCheck: erreur sur une dérogation", zap.Error(err))
	}
	if len(errs) == 0 {
		tm.logInfo("[CRON] RunTrialExpiryCheck: terminé sans erreur")
	}
}
