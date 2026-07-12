package tasks

import "log"

// SendLoyaltyProgrammReminder : Rappel points fidélité (mentionné dans votre script PHP)
func (tm *TasksManager) SendLoyaltyProgrammReminder() {
	log.Println("[CRON] Démarrage: SendLoyaltyProgrammReminder")

	// TODO: Logique de rappel fidélité

	log.Println("[CRON] Terminé: SendLoyaltyProgrammReminder")
}
