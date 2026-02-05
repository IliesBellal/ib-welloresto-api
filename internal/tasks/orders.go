package tasks

import "log"

// CloseOrders : Ferme les commandes payées et livrées après délai
func (tm *TasksManager) CloseOrders() {
	log.Println("[CRON] Démarrage: CloseOrders")

	// TODO: Select commandes éligibles
	// TODO: Boucle -> tm.OrderService.SetDelivered(...)

	log.Println("[CRON] Terminé: CloseOrders")
}

// DenyOrders : Refuse les commandes en attente depuis trop longtemps
func (tm *TasksManager) DenyOrders() {
	log.Println("[CRON] Démarrage: DenyOrders")

	// TODO: Select commandes PENDING_APPROVAL > délai
	// TODO: Boucle -> tm.OrderService.SetOrderDenied(...)

	log.Println("[CRON] Terminé: DenyOrders")
}
