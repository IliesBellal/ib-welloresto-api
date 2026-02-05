package tasks

import "log"

// SendBookingReminders : Rappel pour les réservations à venir
func (tm *TasksManager) SendBookingReminders() {
	log.Println("[CRON] Démarrage: SendBookingReminders")

	// TODO: Select bookings dans l'intervalle de temps
	// TODO: Boucle -> tm.EmailService.SendBookingReminder(...)

	log.Println("[CRON] Terminé: SendBookingReminders")
}

// SendLoyaltyProgrammReminder : Rappel points fidélité (mentionné dans votre script PHP)
func (tm *TasksManager) SendLoyaltyProgrammReminder() {
	log.Println("[CRON] Démarrage: SendLoyaltyProgrammReminder")

	// TODO: Logique de rappel fidélité

	log.Println("[CRON] Terminé: SendLoyaltyProgrammReminder")
}
