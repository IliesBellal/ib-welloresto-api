package services

import (
	"context"
	"errors"
	"fmt"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type OrdersLifeCycleService struct {
	ordersRepo           *repositories.OrdersLifeCycleRepository
	deliverySessionsRepo *repositories.DeliverySessionsRepository
	userRepo             *repositories.UsersRepository // used to resolve token -> merchant id
}

func NewOrdersLifeCycleService(ordersRepo *repositories.OrdersLifeCycleRepository, deliverySessionsRepo *repositories.DeliverySessionsRepository, userRepo *repositories.UsersRepository) *OrdersLifeCycleService {
	return &OrdersLifeCycleService{
		ordersRepo:           ordersRepo,
		deliverySessionsRepo: deliverySessionsRepo,
		userRepo:             userRepo,
	}
}

func (s *OrdersLifeCycleService) ReopenClosedOrder(ctx context.Context, token, orderID string) error {
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

func (s *OrdersLifeCycleService) AddPayment(ctx context.Context, token string, orderID string, req *models.PaymentRequest) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return fmt.Errorf("invalid token")
	}

	// sécurité : orderID dans l’URL > orderID dans req
	req.OrderID = orderID

	return s.ordersRepo.AddPayment(ctx, user.MerchantID, user.UserID, req)
}

func (s *OrdersLifeCycleService) GetPayments(ctx context.Context, token string, orderID string) ([]models.Payment, error) {
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

func (s *OrdersLifeCycleService) DisablePayment(ctx context.Context, token string, paymentID string) error {
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

func (s *OrdersLifeCycleService) SetDistributedProducts(ctx context.Context, token string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {

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

func (s *OrdersLifeCycleService) BackToProduction(ctx context.Context, token, orderID string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	err = s.ordersRepo.MarkProductsBackToProduction(ctx, user.UserID, user.MerchantID, orderID, req.Products)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}
