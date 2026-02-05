package tasks

import "log"

// UpdateAverageDistributionTime : Calcul complexe avec simulation de slots cuisine
func (tm *TasksManager) UpdateAverageDistributionTime() {
	log.Println("[CRON] Démarrage: UpdateAverageDistributionTime")

	// TODO: Implémenter la logique de slots parallèles
	// Récupérer les marchands
	// Simuler la file d'attente
	// Mettre à jour la DB

	log.Println("[CRON] Terminé: UpdateAverageDistributionTime")
}

// UpdateAverageDistributionTimeV1 : Calcul simplifié par commande
func (tm *TasksManager) UpdateAverageDistributionTimeV1() {
	log.Println("[CRON] Démarrage: UpdateAverageDistributionTimeV1")

	// TODO: Implémenter la logique séquentielle

	log.Println("[CRON] Terminé: UpdateAverageDistributionTimeV1")
}

// UpdatePopularProducts : Mise à jour des produits populaires (30 derniers jours)
func (tm *TasksManager) UpdatePopularProducts() {
	log.Println("[CRON] Démarrage: UpdatePopularProducts")

	// TODO: Reset is_popular = 0
	// TODO: Calculer le TOP 1 par catégorie
	// TODO: Calculer le TOP X global par marchand

	log.Println("[CRON] Terminé: UpdatePopularProducts")
}
