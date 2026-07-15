package tasks

// SendLoyaltyProgrammReminder : Rappel points fidélité — non implémenté.
// Log en Debug uniquement pour ne pas polluer les logs de prod à chaque heure.
func (tm *TasksManager) SendLoyaltyProgrammReminder() {
	tm.logDebug("[CRON] SendLoyaltyProgrammReminder: non implémenté, aucune action")
}
