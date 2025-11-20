package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type DeliverySessionsService struct {
	ordersRepo           *repositories.LegacyOrdersRepository
	deliverySessionsRepo *repositories.DeliverySessionsRepository
	userRepo             *repositories.UserRepository // used to resolve token -> merchant id
}

func NewDeliverySessionsService(deliverySessionsRepo *repositories.DeliverySessionsRepository, userRepo *repositories.UserRepository) *DeliverySessionsService {
	return &DeliverySessionsService{
		deliverySessionsRepo: deliverySessionsRepo,
		userRepo:             userRepo,
	}
}

// /delivery_sessions/pending

// GetDeliverySessions returns delivery sessions (no orders)
func (s *DeliverySessionsService) GetDeliverySessions(ctx context.Context, token string) ([]models.DeliverySession, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	return s.deliverySessionsRepo.GetDeliverySessions(ctx, user.MerchantID)
}
