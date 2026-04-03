package main

import (
	"database/sql"
	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/modules/bookings"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/tasks"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

func SetupTasks(log *zap.Logger, emailService *mailer.Service, orderService *order_life_cycle.OrdersLifeCycleService, stripeService *stripeclient.StripeManager, bookingService *bookings.BookingsService, mysqlDB *sql.DB) {

	// 2. Initialisation du Gestionnaire de Tâches
	taskManager := tasks.NewTasksManager(mysqlDB, emailService, orderService, stripeService, bookingService)

	// 3. Configuration du Planificateur (Cron)
	c := cron.New()

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

	// Démarrage du Cron en arrière-plan
	c.Start()
	log.Info("✅ Système CRON démarré")
}
