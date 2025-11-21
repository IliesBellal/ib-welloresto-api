package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type OrdersService struct {
	ordersRepo           *repositories.OrdersRepository
	deliverySessionsRepo *repositories.DeliverySessionsRepository
	userRepo             *repositories.UserRepository // used to resolve token -> merchant id
}

func NewOrdersService(ordersRepo *repositories.OrdersRepository, deliverySessionsRepo *repositories.DeliverySessionsRepository, userRepo *repositories.UserRepository) *OrdersService {
	return &OrdersService{
		ordersRepo:           ordersRepo,
		deliverySessionsRepo: deliverySessionsRepo,
		userRepo:             userRepo,
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

func (s *OrdersService) GetOrder(ctx context.Context, token, orderID string) (*models.Order, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.ordersRepo.GetOrder(ctx, user.MerchantID, orderID)
}
