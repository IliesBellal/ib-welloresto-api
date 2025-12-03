package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type DeliverySessionsService struct {
	deliverySessionsRepo *repositories.DeliverySessionsRepository
	userRepo             *repositories.UsersRepository // used to resolve token -> merchant id
}

func NewDeliverySessionsService(deliverySessionsRepo *repositories.DeliverySessionsRepository, userRepo *repositories.UsersRepository) *DeliverySessionsService {
	return &DeliverySessionsService{
		deliverySessionsRepo: deliverySessionsRepo,
		userRepo:             userRepo,
	}
}

// /delivery_sessions/pending

// GetPendingDeliverySessions returns delivery sessions (no orders)
func (s *DeliverySessionsService) GetPendingDeliverySessions(ctx context.Context, token string) ([]models.DeliverySession, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	return s.deliverySessionsRepo.GetPendingDeliverySessions(ctx, user.MerchantID)
}

func (s *DeliverySessionsService) StartDeliverySession(
	ctx context.Context,
	token string,
	req *models.DeliverySessionRequest,
) (interface{}, error) {

	// 1. Check token → get user + merchant
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return map[string]string{"status": "invalid_token"}, nil
	}

	// 2. Delegate to repo
	session, err := s.deliverySessionsRepo.StartDeliverySession(ctx, req)
	if err != nil {
		return map[string]interface{}{
			"status": "-1",
			"error":  err.Error(),
		}, nil
	}

	return session, nil
}

func (s *DeliverySessionsService) CloseDeliverySession(ctx context.Context, sessionID string) (interface{}, error) {

	session, err := s.deliverySessionsRepo.CloseDeliverySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// s.notifications.SendDeliverySessionUpdate(session.MerchantID, sessionID)

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

func (s *DeliverySessionsService) CancelDeliverySession(ctx context.Context, sessionID string) (interface{}, error) {

	// repo returns DeliverySession struct
	session, err := s.deliverySessionsRepo.CancelDeliverySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Notifications
	// s.notifications.SendCanceledDeliverySession(session.MerchantID, sessionID)

	// Return full delivery session object
	return s.deliverySessionsRepo.GetDeliverySession(ctx, session.MerchantID, sessionID)
}
