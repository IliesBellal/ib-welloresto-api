package delivery_sessions

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/messaggio"
	"welloresto-api/internal/modules/notification"
)

// OrderDeliverer is the minimal interface this service needs to execute transition 3
// (§1.3 #3 / §3.4): fiscally close an order via order_life_cycle.OrdersLifeCycleService.
// Defined here, on the consumer side, because order_life_cycle already imports this
// package (for DeliverySessionsRepository) - importing it back here would create a cycle.
// Satisfied as-is by *order_life_cycle.OrdersLifeCycleService.
type OrderDeliverer interface {
	SetDelivered(ctx context.Context, orderID string) error
}

type DeliverySessionsService struct {
	deliverySessionsRepo *DeliverySessionsRepository
	notificationsService *notification.NotificationService
	orderDeliverer       OrderDeliverer
	smsService           messaggio.SMSService
}

func NewDeliverySessionsService(deliverySessionsRepo *DeliverySessionsRepository, notificationsService *notification.NotificationService, orderDeliverer OrderDeliverer, smsService messaggio.SMSService) *DeliverySessionsService {
	return &DeliverySessionsService{
		deliverySessionsRepo: deliverySessionsRepo,
		notificationsService: notificationsService,
		orderDeliverer:       orderDeliverer,
		smsService:           smsService,
	}
}

// sendDeliveryTrackingSMS notifies, by SMS, the customer of each order in the session that
// has a phone number on file. It runs entirely in a goroutine on its own background context
// so it never blocks or fails the delivery session creation that triggered it (fire-and-forget,
// mirroring the existing FCM push in SendNotificationAsync). Orders sharing the same phone
// number (e.g. several orders for the same customer in one session) are deduplicated so only
// one SMS is sent per number.
func (s *DeliverySessionsService) sendDeliveryTrackingSMS(merchantID string, orderIDs []string) {
	if len(orderIDs) == 0 {
		return
	}

	go func() {
		ctx := context.Background()
		log := logger.FromContext(ctx)

		merchantIDInt, err := strconv.ParseInt(merchantID, 10, 64)
		if err != nil {
			log.Error("sendDeliveryTrackingSMS: invalid merchant id " + merchantID + ": " + err.Error())
			return
		}

		phones, err := s.deliverySessionsRepo.GetOrderCustomerPhones(ctx, orderIDs)
		if err != nil {
			log.Error("sendDeliveryTrackingSMS: failed to load customer phones: " + err.Error())
			return
		}

		sentToPhone := make(map[string]bool, len(phones))
		for _, orderID := range orderIDs {
			phone, ok := phones[orderID]
			if !ok || sentToPhone[phone] {
				continue // no phone on file for this order, or that number already notified for another stop
			}
			sentToPhone[phone] = true

			if err := s.smsService.SendOrderTrackingSMS(ctx, merchantIDInt, orderID, phone); err != nil {
				log.Error("sendDeliveryTrackingSMS: failed to send tracking SMS for order " + orderID + ": " + err.Error())
			}
		}
	}()
}

// /delivery_sessions/pending

// GetPendingDeliverySessions returns delivery sessions (no orders)
func (s *DeliverySessionsService) GetPendingDeliverySessions(ctx context.Context, token string) ([]models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !user.ManageDelivery {
		return nil, models.ErrForbidden
	}

	return s.deliverySessionsRepo.GetPendingDeliverySessions(ctx, user.MerchantID)
}

func (s *DeliverySessionsService) StartDeliverySession(ctx context.Context, token string, req *models.DeliverySessionRequest) (interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !user.ManageDelivery {
		return nil, models.ErrForbidden
	}

	// 2. Delegate to repo
	session, err := s.deliverySessionsRepo.StartDeliverySession(ctx, req)
	if err != nil {
		return nil, err // ← propagation propre de l'erreur
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, session.DeliverySessionID, "UPDATE_DELIVERY_SESSION")

	orderIDs := make([]string, len(session.Orders))
	for i, o := range session.Orders {
		orderIDs[i] = o.OrderID
	}
	s.sendDeliveryTrackingSMS(user.MerchantID, orderIDs)

	return session, nil
}

func (s *DeliverySessionsService) CloseDeliverySession(ctx context.Context, token, sessionID string) (interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !user.ManageDelivery {
		return nil, models.ErrForbidden
	}

	session, err := s.deliverySessionsRepo.CloseDeliverySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, session.DeliverySessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetDeliverySession(ctx, session.MerchantID, sessionID)
}

// GetMyActiveDeliverySession returns the calling delivery user's currently active
// delivery session. Unlike the other methods of this service, it does not require
// the ManageDelivery permission - any authenticated user can fetch their own session.
func (s *DeliverySessionsService) GetMyActiveDeliverySession(ctx context.Context) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.deliverySessionsRepo.GetActiveDeliverySessionForUser(ctx, user.MerchantID, user.UserID)
}

// SelectDeliveryStop sets orderID as the current stop of the caller's active
// delivery session (transition 1, §3.2). Like GetMyActiveDeliverySession, no
// permission beyond authMiddleware is required - "/me" is always self-scoped.
func (s *DeliverySessionsService) SelectDeliveryStop(ctx context.Context, orderID string) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	sessionID, err := s.deliverySessionsRepo.SelectDeliveryStop(ctx, user.MerchantID, user.UserID, orderID)
	if err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetActiveDeliverySessionForUser(ctx, user.MerchantID, user.UserID)
}

// MarkDeliveryStopArrived transitions the caller's current stop from en_route to
// arrived (transition 2, manual path, §3.3).
func (s *DeliverySessionsService) MarkDeliveryStopArrived(ctx context.Context, orderID string) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	sessionID, err := s.deliverySessionsRepo.MarkDeliveryStopArrived(ctx, user.MerchantID, user.UserID, orderID)
	if err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetActiveDeliverySessionForUser(ctx, user.MerchantID, user.UserID)
}

// MarkDeliveryStopDelivered transitions the caller's current stop (en_route or arrived)
// to delivered (transition 3, §1.3 #3 / §3.4). It reuses
// OrdersLifeCycleService.SetDelivered as-is for the fiscal closure of the order
// (payment check, NF525 hash, state='CLOSED', possible session auto-close to 'done',
// §0.3) - that call runs in its own, separate transaction and emits UPDATE_ORDER on
// success.
//
// Non-atomic window (§3.4 "deux pièges", option (b)): SetDelivered commits first; the
// per-stop bookkeeping (mark delivered, reassign payment, advance current_order_id) is
// then applied in FinalizeDeliveredStop's own transaction. If the process crashes
// between the two, the order is already fiscally closed but the stop still shows
// en_route/arrived. This is safe to resume: SetDelivered is idempotent for an
// already-closed order (OrderStillOpen check short-circuits to just re-sending
// UPDATE_ORDER), and FinalizeDeliveredStop's UPDATE on delivery_session_order does not
// depend on the stop's previous status, so simply retrying this endpoint completes the
// transition.
func (s *DeliverySessionsService) MarkDeliveryStopDelivered(ctx context.Context, orderID string) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	sessionID, err := s.deliverySessionsRepo.ResolveDeliverableStop(ctx, user.MerchantID, user.UserID, orderID)
	if err != nil {
		return nil, err
	}

	if err := s.orderDeliverer.SetDelivered(ctx, orderID); err != nil {
		var notPaidErr *models.OrderNotFullyPaidError
		if errors.As(err, &notPaidErr) {
			return nil, models.ErrOrderNotFullyPaid
		}
		return nil, err
	}

	if err := s.deliverySessionsRepo.FinalizeDeliveredStop(ctx, sessionID, orderID); err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetDeliverySessionByIDForUser(ctx, user.MerchantID, user.UserID, sessionID)
}

// validateStopReason enforces the "reason required, <= 255 chars" rule shared by
// MarkDeliveryStopFailed and CancelDeliveryStop (§3.5/§3.6), returning the trimmed reason.
func validateStopReason(req *DeliveryStopReasonRequest) (string, error) {
	if req == nil {
		return "", models.ErrFailReasonRequired
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || len(reason) > 255 {
		return "", models.ErrFailReasonRequired
	}
	return reason, nil
}

// extractStructuredDeletionReason returns the optional deletion_reason_id/comment
// fields from req (034_delivery_stop_deletion_reason), trimmed, with empty values
// treated as absent (nil) so terminalizeDeliveryStop leaves the corresponding columns
// NULL - same as today when these fields aren't sent.
func extractStructuredDeletionReason(req *DeliveryStopReasonRequest) (deletionReasonID, comment *string) {
	if req == nil {
		return nil, nil
	}
	if req.DeletionReasonID != nil {
		if id := strings.TrimSpace(*req.DeletionReasonID); id != "" {
			deletionReasonID = &id
		}
	}
	if req.Comment != nil {
		if c := strings.TrimSpace(*req.Comment); c != "" {
			comment = &c
		}
	}
	return deletionReasonID, comment
}

// MarkDeliveryStopFailed transitions the caller's current stop (any non-terminal state)
// to failed (transition 4, §1.3 #4 / §3.5). orders.brand_status becomes
// 'DELIVERY_FAILED' and orders.state is left 'OPEN' (re-dispatchable).
func (s *DeliverySessionsService) MarkDeliveryStopFailed(ctx context.Context, orderID string, req *DeliveryStopReasonRequest) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	reason, err := validateStopReason(req)
	if err != nil {
		return nil, err
	}
	deletionReasonID, comment := extractStructuredDeletionReason(req)

	sessionID, err := s.deliverySessionsRepo.MarkDeliveryStopFailed(ctx, user.MerchantID, user.UserID, orderID, reason, deletionReasonID, comment)
	if err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetDeliverySessionByIDForUser(ctx, user.MerchantID, user.UserID, sessionID)
}

// CancelDeliveryStop transitions the caller's current stop (any non-terminal state) to
// canceled (transition 5, §1.3 #5 / §3.6, path (a) - no refund, left to the dispatcher).
// orders.brand_status becomes 'DELIVERY_CANCELED' and orders.state is left 'OPEN'
// (re-dispatchable).
func (s *DeliverySessionsService) CancelDeliveryStop(ctx context.Context, orderID string, req *DeliveryStopReasonRequest) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	reason, err := validateStopReason(req)
	if err != nil {
		return nil, err
	}
	deletionReasonID, comment := extractStructuredDeletionReason(req)

	sessionID, err := s.deliverySessionsRepo.CancelDeliveryStop(ctx, user.MerchantID, user.UserID, orderID, reason, deletionReasonID, comment)
	if err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetDeliverySessionByIDForUser(ctx, user.MerchantID, user.UserID, sessionID)
}

// CloseMyDeliverySession closes the caller's active delivery session once all of its
// stops are terminal (§1.5/§3.8), writing delivery_session.status='done'.
//
// If the session was already auto-closed by the last /delivered call (status='done',
// §0.3), it is no longer "active" (status = 'active') and the repo returns
// ErrNoActiveDeliverySession (404). This is intentional, not worked around: the app
// treats a 404 here as "already closed" = success.
func (s *DeliverySessionsService) CloseMyDeliverySession(ctx context.Context) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	sessionID, err := s.deliverySessionsRepo.CloseMyDeliverySession(ctx, user.MerchantID, user.UserID)
	if err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetDeliverySessionByIDForUser(ctx, user.MerchantID, user.UserID, sessionID)
}

func (s *DeliverySessionsService) GetDeliverySession(ctx context.Context, token, delivery_session_id string) (*models.DeliverySession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !user.ManageDelivery {
		return nil, models.ErrForbidden
	}
	return s.FetchDeliverySession(ctx, user.MerchantID, delivery_session_id)
}

func (s *DeliverySessionsService) FetchDeliverySession(ctx context.Context, merchantID, delivery_session_id string) (*models.DeliverySession, error) {
	return s.deliverySessionsRepo.GetDeliverySession(ctx, merchantID, delivery_session_id)
}

func (s *DeliverySessionsService) CancelDeliverySession(ctx context.Context, token, sessionID string) (interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !user.ManageDelivery {
		return nil, models.ErrForbidden
	}

	// repo returns DeliverySession struct
	session, err := s.deliverySessionsRepo.CancelDeliverySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Notifications
	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, session.DeliverySessionID, "UPDATE_DELIVERY_SESSION")

	// Return full delivery session object
	return s.deliverySessionsRepo.GetDeliverySession(ctx, session.MerchantID, sessionID)
}
