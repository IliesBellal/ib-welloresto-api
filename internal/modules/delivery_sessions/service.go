package delivery_sessions

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/notification"
)

type DeliverySessionsService struct {
	deliverySessionsRepo *DeliverySessionsRepository
	userRepo             auth.AuthService
	notificationsService *notification.NotificationService
}

func NewDeliverySessionsService(deliverySessionsRepo *DeliverySessionsRepository, userRepo auth.AuthService, notificationsService *notification.NotificationService) *DeliverySessionsService {
	return &DeliverySessionsService{
		deliverySessionsRepo: deliverySessionsRepo,
		userRepo:             userRepo,
		notificationsService: notificationsService,
	}
}

// /delivery_sessions/pending

// GetPendingDeliverySessions returns delivery sessions (no orders)
func (s *DeliverySessionsService) GetPendingDeliverySessions(ctx context.Context, token string) ([]DeliverySession, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	if !user.ManageDelivery {
		return nil, errors.New("user not authorized to manage delivery sessions")
	}
	return s.deliverySessionsRepo.GetPendingDeliverySessions(ctx, user.MerchantID)
}

func (s *DeliverySessionsService) StartDeliverySession(ctx context.Context, token string, req *models.DeliverySessionRequest) (interface{}, error) {

	// 1. Check token → get user + merchant
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return map[string]string{"status": "invalid_token"}, nil
	}

	// 2. Delegate to repo
	session, err := s.deliverySessionsRepo.StartDeliverySession(ctx, req)
	if err != nil {
		return nil, err // ← propagation propre de l'erreur
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, session.DeliverySessionID, "UPDATE_DELIVERY_SESSION")

	return session, nil
}

func (s *DeliverySessionsService) CloseDeliverySession(ctx context.Context, token, sessionID string) (interface{}, error) {

	// 1. Check token → get user + merchant
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return map[string]string{"status": "invalid_token"}, nil
	}

	session, err := s.deliverySessionsRepo.CloseDeliverySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	_ = s.notificationsService.SendNotificationAsync(user.MerchantID, session.DeliverySessionID, "UPDATE_DELIVERY_SESSION")

	return s.deliverySessionsRepo.GetDeliverySession(ctx, session.MerchantID, sessionID)
}

func (s *DeliverySessionsService) GetDeliverySession(ctx context.Context, token, delivery_session_id string) (*models.DeliverySession, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	return s.deliverySessionsRepo.GetDeliverySession(ctx, user.MerchantID, delivery_session_id)
}

func (s *DeliverySessionsService) CancelDeliverySession(ctx context.Context, token, sessionID string) (interface{}, error) {

	// 1. Check token → get user + merchant
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return map[string]string{"status": "invalid_token"}, nil
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
