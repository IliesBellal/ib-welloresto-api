package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/repositories"

	"go.uber.org/zap"
)

type OrdersLifeCycleService struct {
	ordersLifeCycleRepo  *repositories.OrdersLifeCycleRepository
	deliverySessionsRepo *repositories.DeliverySessionsRepository
	uberSvc              *UberEatsService
	deliverooSvc         *DeliverooService
	userRepo             *repositories.UsersRepository
	log                  *zap.Logger
	notificationsService *notification.NotificationService
	customersRepo        *repositories.CustomersRepository
}

func NewOrdersLifeCycleService(ordersRepo *repositories.OrdersLifeCycleRepository, uberSvc *UberEatsService, deliverooSvc *DeliverooService,
	deliverySessionsRepo *repositories.DeliverySessionsRepository, userRepo *repositories.UsersRepository,
	log *zap.Logger, notificationsService *notification.NotificationService, customersRepo *repositories.CustomersRepository) *OrdersLifeCycleService {
	return &OrdersLifeCycleService{
		ordersLifeCycleRepo:  ordersRepo,
		deliverySessionsRepo: deliverySessionsRepo,
		userRepo:             userRepo,
		uberSvc:              uberSvc,
		deliverooSvc:         deliverooSvc,
		log:                  log,
		notificationsService: notificationsService,
		customersRepo:        customersRepo,
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

	return s.ordersLifeCycleRepo.ReopenClosedOrder(ctx, user.MerchantID, orderID, user.UserID)
}

func (s *OrdersLifeCycleService) AddPayment(ctx context.Context, token string, orderID string, req *models.PaymentRequest) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return fmt.Errorf("invalid token")
	}

	// sécurité : orderID dans l’URL > orderID dans req
	req.OrderID = orderID

	return s.ordersLifeCycleRepo.AddPayment(ctx, user.MerchantID, user.UserID, req)
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
	return s.ordersLifeCycleRepo.GetPaymentsForOrder(ctx, orderID)
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

	return s.ordersLifeCycleRepo.DisablePayment(ctx, paymentID)
}

func (s *OrdersLifeCycleService) SetDistributedProducts(ctx context.Context, token string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return map[string]interface{}{"status": "-1", "error": "Invalid token"}, nil
	}

	err = s.ordersLifeCycleRepo.SetDistributedProducts(ctx, user.UserID, user.MerchantID, req)
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

	err = s.ordersLifeCycleRepo.MarkProductsBackToProduction(ctx, user.UserID, user.MerchantID, orderID, req.Products)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *OrdersLifeCycleService) AcceptOrder(ctx context.Context, token, orderID string) (map[string]string, error) {
	// 1) Get brand and merchant (we need merchant id to call integrators)
	orderMeta, err := s.ordersLifeCycleRepo.GetOrderBrandAndMerchant(ctx, orderID)
	if err != nil {
		return nil, err
	}

	// 2) Update local order immediately (set OPEN, PENDING, ACCEPTED as in PHP)
	if err := s.ordersLifeCycleRepo.SetOrderAcceptedLocal(ctx, orderID); err != nil {
		return nil, err
	}

	// Fire notification — placeholder (non blocking)
	go func() {
		/*
			ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.notifier.SendOrderUpdate(ctx2, orderMeta.MerchantID, orderID); err != nil {
				s.log.Warn("notify send failed", zap.Error(err), zap.String("order_id", orderID))
			}
		*/
	}()

	// 3) If brand is external, call integration ASYNC
	brand := orderMeta.Brand
	merchantID := orderMeta.MerchantID
	switch brand {
	case models.BrandUberEats:
		// call Uber Eats integration async
		go func(mID, oID string) {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.uberSvc.AcceptOrder(ctxTimeout, mID, oID); err != nil {
				s.log.Error("uber accept failed", zap.String("order_id", oID), zap.Error(err))
			}
		}(merchantID, orderID)
	case models.BrandDeliveroo:
		go func(mID, oID string) {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.deliverooSvc.AcceptOrder(ctxTimeout, merchantID, orderID); err != nil {
				s.log.Error("deliveroo accept failed", zap.String("order_id", oID), zap.Error(err))
			}
		}(merchantID, orderID)
	default:
		// Internal order — nothing else to do
	}

	return map[string]string{"status": "1"}, nil
}

func (s *OrdersLifeCycleService) StartDelivery(ctx context.Context, token string, orderID string, userID string) (map[string]interface{}, error) {

	// 1) Update Wello DB
	integrationInfo, err := s.ordersLifeCycleRepo.MarkOrderAsDeliveryStarted(ctx, orderID, userID)
	if err != nil {
		return map[string]interface{}{"status": "0", "error": err.Error()}, err
	}

	// 2) Send realtime update
	//s.notifier.SendOrderUpdate(integrationInfo.MerchantID, orderID)

	// 3) Branch Uber Eats / Deliveroo asynchronously
	switch integrationInfo.Brand {
	case "UBER_EATS":
		go func() {
			err := s.uberSvc.SetOrderStarted(ctx, integrationInfo.MerchantID, integrationInfo.BrandOrderID)
			if err != nil {
				log.Println("UberEats StartDelivery error:", err)
			}
		}()

	case "DELIVEROO":
		go func() {
			err := s.deliverooSvc.StartDeliverooDelivery(ctx, integrationInfo.BrandOrderID)
			if err != nil {
				log.Println("Deliveroo StartDelivery error:", err)
			}
		}()
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *OrdersLifeCycleService) DenyOrder(ctx context.Context, in models.DenyOrderInput) error {
	// Local DB update (atomic)
	err := s.ordersLifeCycleRepo.DenyOrderLocal(ctx,
		in.OrderID,
		in.DeletionReasonID,
		in.DeletionComment,
	)
	if err != nil {
		return fmt.Errorf("deny local: %w", err)
	}

	// Cancel stripe payments
	err = s.ordersLifeCycleRepo.CancelStripePayments(ctx, in.OrderID)
	if err != nil {
		return fmt.Errorf("stripe cancel: %w", err)
	}

	// Notify
	s.notificationsService.SendNotificationAsync(in.MerchantID, in.OrderID, "ORDER_UPDATE")

	// Dispatch to integration
	brand, err := s.ordersLifeCycleRepo.GetOrderBrand(ctx, in.OrderID)
	if err != nil {
		return err
	}

	switch brand {
	case "UBER_EATS":
		go s.uberSvc.DenyOrder(ctx, in.MerchantID, in.OrderID, in.DeletionReasonID, in.DeletionReasonType, in.DeletionComment)

	case "DELIVEROO":
		go s.deliverooSvc.DenyOrder(ctx, in.OrderID, in.DeletionReasonID, in.DeletionReasonType, in.DeletionComment)
	}

	return nil
}

func (s *OrdersLifeCycleService) SetReadyForDistribution(ctx context.Context, in models.ReadyForDistributionInput) error {
	// 1 → Wello local update
	if err := s.ordersLifeCycleRepo.SetReadyForDistribution(ctx, in.OrderID, in.MerchantID); err != nil {
		return err
	}

	// 2 → Send notif
	s.notificationsService.SendNotificationAsync(in.MerchantID, in.OrderID, "UPDATE_ORDER")

	// 3 → Async integrations
	brand, err := s.ordersLifeCycleRepo.GetOrderBrand(ctx, in.OrderID)
	if err != nil {
		return err
	}

	switch brand {
	case "UBER_EATS":
		go s.uberSvc.ReadyForHandoff(ctx, in.OrderID)

	case "DELIVEROO":
		go s.deliverooSvc.ReadyForCollection(ctx, in.OrderID)
	}

	return nil
}

func (s *OrdersLifeCycleService) DeleteOrder(ctx context.Context, in models.DenyOrderInput) error {

	// 1 — Local DB operations
	if err := s.ordersLifeCycleRepo.DeleteOrderLocal(
		ctx,
		in.OrderID,
		in.DeletionReasonID,
		in.DeletionComment,
	); err != nil {
		return err
	}

	// Reactivate rewards
	if err := s.customersRepo.ReactivateRewards(ctx, in.OrderID); err != nil {
		return fmt.Errorf("reactivate rewards: %w", err)
	}

	// Delete QR
	if err := s.ordersLifeCycleRepo.DeleteQRCode(ctx, in.OrderID); err != nil {
		return err
	}

	// Disable payments
	if err := s.ordersLifeCycleRepo.DisablePayments(ctx, in.OrderID); err != nil {
		return err
	}

	// Clear bookings
	if err := s.ordersLifeCycleRepo.ClearBookings(ctx, in.OrderID); err != nil {
		return err
	}

	// Send notif
	s.notificationsService.SendNotificationAsync(in.MerchantID, in.OrderID, "UPDATE_ORDER")

	// Integration
	brand, err := s.ordersLifeCycleRepo.GetOrderBrand(ctx, in.OrderID)
	if err != nil {
		return err
	}

	switch brand {
	case "UBER_EATS":
		go s.uberSvc.CancelOrder(ctx, in.MerchantID, in.OrderID, in.DeletionReasonID, in.DeletionComment)

	case "DELIVEROO":
		go s.deliverooSvc.CancelOrder(ctx, in.UserID, in.OrderID, in.DeletionReasonID, in.DeletionComment)
	}

	return nil
}
