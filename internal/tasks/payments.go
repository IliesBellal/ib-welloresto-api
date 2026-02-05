package tasks

import "log"

// CapturePayments : Valide les paiements Stripe différés
func (tm *TasksManager) CapturePayments() {
	log.Println("[CRON] Démarrage: CapturePayments")

	// TODO: Select paiements REQUIRES_CONFIRMATION
	// TODO: Boucle -> tm.StripeService.CapturePayment(...)

	log.Println("[CRON] Terminé: CapturePayments")
}

// CancelPayments : Annule les empreintes bancaires si commande annulée
func (tm *TasksManager) CancelPayments() {
	log.Println("[CRON] Démarrage: CancelPayments")

	// TODO: Select paiements liés à des commandes DENIED/CANCELED
	// TODO: Boucle -> tm.StripeService.CancelPayment(...)

	log.Println("[CRON] Terminé: CancelPayments")
}
