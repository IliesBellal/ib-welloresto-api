package tasks

import (
	"database/sql"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/modules/bookings"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/webhook/stripe"
)

// TasksManager centralise toutes les dépendances nécessaires aux tâches CRON
type TasksManager struct {
	DB             *sql.DB
	EmailService   *mailer.Service
	OrderService   *order_life_cycle.OrdersLifeCycleService
	StripeService  *stripe.StripeWebhookService
	BookingService *bookings.BookingsService
}

// NewTasksManager crée une nouvelle instance du gestionnaire avec les dépendances injectées
func NewTasksManager(
	db *sql.DB,
	email *mailer.Service,
	order *order_life_cycle.OrdersLifeCycleService,
	stripe *stripe.StripeWebhookService,
	booking *bookings.BookingsService,
) *TasksManager {
	return &TasksManager{
		DB:             db,
		EmailService:   email,
		OrderService:   order,
		StripeService:  stripe,
		BookingService: booking,
	}
}
