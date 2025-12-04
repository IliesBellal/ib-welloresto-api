package services

import (
	"context"
	"errors"
	"fmt"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type OrdersService struct {
	ordersRepo           *repositories.OrdersRepository
	deliverySessionsRepo *repositories.DeliverySessionsRepository
	userRepo             *repositories.UsersRepository // used to resolve token -> merchant id
}

func NewOrdersService(ordersRepo *repositories.OrdersRepository, deliverySessionsRepo *repositories.DeliverySessionsRepository, userRepo *repositories.UsersRepository) *OrdersService {
	return &OrdersService{
		ordersRepo:           ordersRepo,
		deliverySessionsRepo: deliverySessionsRepo,
		userRepo:             userRepo,
	}
}

func (s *OrdersService) ReopenClosedOrder(ctx context.Context, token, orderID string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("invalid token")
	}

	// Ici user.MerchantID et user.UserID sont récupérés automatiquement

	return s.ordersRepo.ReopenClosedOrder(ctx, user.MerchantID, orderID, user.UserID)
}

func (s *OrdersService) AddPayment(ctx context.Context, token string, orderID string, req *models.PaymentRequest) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return fmt.Errorf("invalid token")
	}

	// sécurité : orderID dans l’URL > orderID dans req
	req.OrderID = orderID

	return s.ordersRepo.AddPayment(ctx, user.MerchantID, user.UserID, req)
}

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

func (s *OrdersService) GetHistory(ctx context.Context, token string, req models.OrderHistoryRequest) ([]models.Order, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.ordersRepo.GetHistory(ctx, user.MerchantID, req)
}

func (s *OrdersService) GetPayments(ctx context.Context, token string, orderID string) ([]models.Payment, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	return s.ordersRepo.GetPaymentsForOrder(ctx, orderID)
}

func (s *OrdersService) DisablePayment(ctx context.Context, token string, paymentID string) error {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("invalid token")
	}

	return s.ordersRepo.DisablePayment(ctx, paymentID)
}

func (s *OrdersService) SetDistributedProducts(ctx context.Context, token string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return map[string]interface{}{"status": "-1", "error": "Invalid token"}, nil
	}

	err = s.ordersRepo.SetDistributedProducts(ctx, user.UserID, user.MerchantID, req)
	if err != nil {
		return map[string]interface{}{
			"status": "-2",
			"error":  err.Error(),
		}, nil
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *OrdersService) CreateOrder(ctx context.Context, token string, req *models.CreateOrderRequest) (*models.CreateOrderResult, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	req.MerchantID = user.MerchantID

	return s.ordersRepo.CreateOrder(ctx, req)
}
