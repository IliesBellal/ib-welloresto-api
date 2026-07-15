package tasks

import (
	"context"
	"database/sql"
	"log"
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

// logDebug / logInfo / logWarn / logError : wrappers nil-safe autour du
// zap.Logger injecté. Fallback stdlib si le logger est absent, pour qu'aucune
// tâche CRON ne panique ni ne perde ses logs.
func (tm *TasksManager) logDebug(msg string, fields ...zap.Field) {
	if tm.Logger != nil {
		tm.Logger.Debug(msg, fields...)
	}
}

func (tm *TasksManager) logInfo(msg string, fields ...zap.Field) {
	if tm.Logger != nil {
		tm.Logger.Info(msg, fields...)
		return
	}
	log.Println(msg)
}

func (tm *TasksManager) logWarn(msg string, fields ...zap.Field) {
	if tm.Logger != nil {
		tm.Logger.Warn(msg, fields...)
		return
	}
	log.Println(msg)
}

func (tm *TasksManager) logError(msg string, fields ...zap.Field) {
	if tm.Logger != nil {
		tm.Logger.Error(msg, fields...)
		return
	}
	log.Println(msg)
}

func (tm *TasksManager) ExpirePendingBookings() {
	if tm.BookingService == nil {
		tm.logWarn("booking pending expiration skipped: booking service unavailable")
		return
	}

	rows, err := tm.BookingService.ExpirePendingBookings(context.Background())
	if err != nil {
		tm.logError("booking pending expiration failed", zap.Error(err))
		return
	}

	tm.logInfo("booking pending expiration finished", zap.Int64("rows_affected", rows))
}

// ExpireWaitlistNotifications expire les entrées de liste d'attente notified
// dont le délai est dépassé et notifie l'entrée suivante. Même schéma dormant
// que ExpirePendingBookings : tâche prête, activée manuellement dans SetupTasks.
func (tm *TasksManager) ExpireWaitlistNotifications() {
	if tm.BookingService == nil {
		tm.logWarn("waitlist expiration skipped: booking service unavailable")
		return
	}

	rows, err := tm.BookingService.ExpireWaitlistNotifications(context.Background())
	if err != nil {
		tm.logError("waitlist expiration failed", zap.Error(err))
		return
	}

	tm.logInfo("waitlist expiration finished", zap.Int64("rows_affected", rows))
}

// SendBookingReminders envoie le rappel avant service (J-1 par défaut) aux
// réservations confirmed à venir qui n'en ont pas encore reçu. Même schéma
// dormant que les deux autres tâches réservation : prête, activée
// sélectivement dans SetupTasks.
func (tm *TasksManager) SendBookingReminders() {
	if tm.BookingService == nil {
		tm.logWarn("booking reminders skipped: booking service unavailable")
		return
	}

	rows, err := tm.BookingService.SendBookingReminders(context.Background())
	if err != nil {
		tm.logError("booking reminders failed", zap.Error(err))
		return
	}

	tm.logInfo("booking reminders finished", zap.Int64("rows_affected", rows))
}
