package main

import (
	"database/sql"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/modules/bookings"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/tasks"
	"welloresto-api/internal/webhook/stripe"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

func SetupTasks(log *zap.Logger, emailService *mailer.Service, orderService *order_life_cycle.OrdersLifeCycleService, stripeService *stripe.StripeWebhookService, bookingService *bookings.BookingsService, mysqlDB *sql.DB) {

	// 2. Initialisation du Gestionnaire de Tâches
	taskManager := tasks.NewTasksManager(mysqlDB, emailService, orderService, stripeService, bookingService)

	// 3. Configuration du Planificateur (Cron)
	c := cron.New()

	// --- Planning (Basé sur vos scripts PHP) ---

	// Toutes les minutes
	c.AddFunc("@every 1m", func() {
		taskManager.CapturePayments()
		taskManager.CancelPayments()
	})

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
		// Je rajoute celui-ci ici (probablement mensuel ou hebdo en réalité, à vérifier)
		taskManager.UpdatePopularProducts()
	})

	// Démarrage du Cron en arrière-plan
	c.Start()
	log.Info("✅ Système CRON démarré")
}
