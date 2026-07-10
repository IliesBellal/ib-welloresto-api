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

	// Réactivation sélective (Phase 5 Lot 1) : uniquement les 3 tâches
	// réservation préparées. Aucune autre tâche dormante n'est réactivée —
	// leurs appels restent commentés ci-dessous plutôt que protégés par un
	// return global, pour qu'un futur ajout de tâche n'ait pas à y penser.
	c.AddFunc("@hourly", func() { taskManager.ExpirePendingBookings() })
	c.AddFunc("@every 5m", func() { taskManager.ExpireWaitlistNotifications() })
	c.AddFunc("@every 30m", func() { taskManager.SendBookingReminders() })

	// Tâches dormantes hors périmètre réservation — NE PAS réactiver ici.
	// Pour les réactiver un jour, décommenter au cas par cas.
	//
	c.AddFunc("@every 1s", func() {
		taskManager.UpdateAverageDistributionTime()
	})

	c.AddFunc("@hourly", func() {
		taskManager.CloseOrders()
		taskManager.DenyOrders()
		taskManager.SendLoyaltyProgrammReminder()
		taskManager.CapturePayments()
		taskManager.CancelPayments()
	})
	c.AddFunc("@every 1s", func() {
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
	log.Info("✅ Système CRON démarré (réservation uniquement : ExpirePendingBookings @hourly, ExpireWaitlistNotifications @every 5m, SendBookingReminders @every 30m)")
}
