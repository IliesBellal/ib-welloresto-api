package tasks

import (
	"context"

	"go.uber.org/zap"

	"welloresto-api/internal/modules/assistant"
)

// RunAssistantLearning — apprentissage autonome de l'assistant
// (docs/assistant.md) : recompte les signaux, promeut, rétrograde, passe en
// global. Sans les tables (migration 206 pas encore appliquée), l'échec est
// journalisé et sans conséquence.
func (tm *TasksManager) RunAssistantLearning() {
	if tm.DB == nil {
		tm.logWarn("[CRON] RunAssistantLearning: base indisponible, tâche ignorée")
		return
	}
	report, err := assistant.RunLearning(context.Background(), tm.DB)
	if err != nil {
		tm.logError("[CRON] RunAssistantLearning: échec", zap.Error(err))
		return
	}
	tm.logInfo("[CRON] RunAssistantLearning: terminé", zap.Any("report", report))
}

// CleanupAssistantData — conservation RGPD de l'assistant : texte libre
// retiré après 90 jours, événements et sessions supprimés après 400 jours.
func (tm *TasksManager) CleanupAssistantData() {
	if tm.DB == nil {
		tm.logWarn("[CRON] CleanupAssistantData: base indisponible, tâche ignorée")
		return
	}
	log := tm.Logger
	if log == nil {
		log = zap.L()
	}
	assistant.RunRetention(context.Background(), tm.DB, log)
}
