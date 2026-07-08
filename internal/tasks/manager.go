package tasks

import (
	"context"
	"database/sql"
	aicache "welloresto-api/internal/ai/cache"
	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/modules/bookings"
	"welloresto-api/internal/modules/order_life_cycle"
	upsellModule "welloresto-api/internal/modules/upsell"

	"go.uber.org/zap"
)

// TasksManager centralise toutes les dépendances nécessaires aux tâches CRON
type TasksManager struct {
	DB             *sql.DB
	EmailService   *mailer.Service
	OrderService   *order_life_cycle.OrdersLifeCycleService
	StripeService  *stripeclient.StripeManager
	BookingService *bookings.BookingsService
	AICache        *aicache.Cache
	UpsellRepo     *upsellModule.Repository
	Logger         *zap.Logger
}

// NewTasksManager crée une nouvelle instance du gestionnaire avec les dépendances injectées
func NewTasksManager(
	db *sql.DB,
	email *mailer.Service,
	order *order_life_cycle.OrdersLifeCycleService,
	stripe *stripeclient.StripeManager,
	booking *bookings.BookingsService,
	aiCache *aicache.Cache,
	upsellRepo *upsellModule.Repository,
	logger *zap.Logger,
) *TasksManager {
	return &TasksManager{
		DB:             db,
		EmailService:   email,
		OrderService:   order,
		StripeService:  stripe,
		BookingService: booking,
		AICache:        aiCache,
		UpsellRepo:     upsellRepo,
		Logger:         logger,
	}
}

func (tm *TasksManager) ExpirePendingBookings() {
	if tm.BookingService == nil {
		if tm.Logger != nil {
			tm.Logger.Warn("booking pending expiration skipped: booking service unavailable")
		}
		return
	}

	rows, err := tm.BookingService.ExpirePendingBookings(context.Background())
	if err != nil {
		if tm.Logger != nil {
			tm.Logger.Error("booking pending expiration failed", zap.Error(err))
		}
		return
	}

	if tm.Logger != nil {
		tm.Logger.Info("booking pending expiration finished", zap.Int64("rows_affected", rows))
	}
}

// ExpireWaitlistNotifications expire les entrées de liste d'attente notified
// dont le délai est dépassé et notifie l'entrée suivante. Même schéma dormant
// que ExpirePendingBookings : tâche prête, activée manuellement dans SetupTasks.
func (tm *TasksManager) ExpireWaitlistNotifications() {
	if tm.BookingService == nil {
		if tm.Logger != nil {
			tm.Logger.Warn("waitlist expiration skipped: booking service unavailable")
		}
		return
	}

	rows, err := tm.BookingService.ExpireWaitlistNotifications(context.Background())
	if err != nil {
		if tm.Logger != nil {
			tm.Logger.Error("waitlist expiration failed", zap.Error(err))
		}
		return
	}

	if tm.Logger != nil {
		tm.Logger.Info("waitlist expiration finished", zap.Int64("rows_affected", rows))
	}
}
