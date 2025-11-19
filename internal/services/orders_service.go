package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type OrdersService struct {
	ordersRepo *repositories.LegacyOrdersRepository
	userRepo   *repositories.UserRepository // used to resolve token -> merchant id
}

func NewOrdersService(ordersRepo *repositories.LegacyOrdersRepository, userRepo *repositories.UserRepository) *OrdersService {
	return &OrdersService{
		ordersRepo: ordersRepo,
		userRepo:   userRepo,
	}
}

// GetPendingOrders resolves token -> merchant, then fetch pending orders (legacy)
func (s *OrdersService) GetPendingOrders(ctx context.Context, token string, app string) (*models.PendingOrdersResponse, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.ordersRepo.GetPendingOrders(ctx, user.MerchantID, app)
}

// GetDeliverySessions returns delivery sessions (no orders)
func (s *OrdersService) GetDeliverySessions(ctx context.Context, token string) ([]models.DeliverySession, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	return s.ordersRepo.GetDeliverySessions(ctx, user.MerchantID)
}
