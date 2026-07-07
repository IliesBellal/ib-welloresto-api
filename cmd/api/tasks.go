package main

import (
	"welloresto-api/internal/tasks"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

func SetupTasks(
	log *zap.Logger,
	taskManager *tasks.TasksManager,
) {
	// 3. Configuration du Planificateur (Cron)
	c := cron.New()

	// CRON GLOBAL TOUJOURS DESACTIVE.
	// La tache d'expiration des reservations pending est prete :
	//   c.AddFunc("@hourly", func() { taskManager.ExpirePendingBookings() })
	// Pour l'activer manuellement plus tard, retirer le return ci-dessous puis
	// decommenter UNIQUEMENT cette ligne, sans reactiver les autres taches dormantes.
	return

	// Toutes les 15 minutes
	c.AddFunc("@every 15m", func() {
		taskManager.UpdateAverageDistributionTime()
		taskManager.SendBookingReminders()
	})

	// Toutes les heures
	c.AddFunc("@hourly", func() {
		taskManager.CloseOrders()
		taskManager.DenyOrders()
		taskManager.SendLoyaltyProgrammReminder()

		taskManager.CapturePayments()
		taskManager.CancelPayments()
	})

	// Toutes les heures
	c.AddFunc("@monthly", func() {
		taskManager.UpdatePopularProducts()
	})

	// Chaque nuit à 3h : recalcul des patterns market basket
	c.AddFunc("0 3 * * *", func() {
		taskManager.RecomputeUpsellPatterns()
	})

	// 1er du mois à 4h : purge des anciennes suggestions
	c.AddFunc("0 4 1 * *", func() {
		taskManager.CleanupOldUpsellSuggestions()
	})

	// Démarrage du Cron en arrière-plan
	c.Start()
	log.Info("✅ Système CRON démarré")
}
