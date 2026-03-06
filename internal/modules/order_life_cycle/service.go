package order_life_cycle

import (
	"context"
	"fmt"
	"log"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/deliveroo"
	"welloresto-api/internal/modules/delivery_sessions"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/ubereats"

	"go.uber.org/zap"
)

type OrdersLifeCycleService struct {
	ordersLifeCycleRepo  *OrdersLifeCycleRepository
	deliverySessionsRepo *delivery_sessions.DeliverySessionsRepository
	uberSvc              *ubereats.UberEatsService
	deliverooSvc         *deliveroo.DeliverooService
	userRepo             auth.AuthService
	log                  *zap.Logger
	notificationsService *notification.NotificationService
	customersRepo        *customers.CustomersRepository
}

func NewOrdersLifeCycleService(ordersRepo *OrdersLifeCycleRepository, uberSvc *ubereats.UberEatsService, deliverooSvc *deliveroo.DeliverooService,
	deliverySessionsRepo *delivery_sessions.DeliverySessionsRepository, userRepo auth.AuthService,
	log *zap.Logger, notificationsService *notification.NotificationService, customersRepo *customers.CustomersRepository) *OrdersLifeCycleService {
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

func (s *OrdersLifeCycleService) DeliverOrder(ctx context.Context, UserID, MerchantID, orderID string) error {
	log := logger.FromContext(ctx)

	// 2) Mettre la commande en Delivered (local DB updates)
	order, err := s.ordersLifeCycleRepo.SetDeliveredLocal(ctx, orderID)
	if err != nil {
		return err
	}

	// 3) Notify app
	_ = s.notificationsService.SendNotificationAsync(MerchantID, orderID, "UPDATE_ORDER")

	// 4) HandleWebhook integration
	switch order.Brand {
	case models.BrandUberEats:
		if order.FulfillmentType == "DELIVERY_BY_RESTAURANT" {
			//TODO Check API Uber Eats pour ajouter le endpoint correspondant (ne semble pas exister)
			//return s.uberSvc.SetDelivered(ctx, merchantID, *order.BrandOrderID)
		}
		return nil

	case models.BrandDeliveroo:
		if order.FulfillmentType == "DELIVERY_BY_RESTAURANT" {
			log.Warn("Delivery by restaurant - No BYOC implemented for DELIVEROO")
			// Not coded in PHP -> return simple OK or your logic
			return nil
		}
		return s.deliverooSvc.SetCollected(ctx, *order.BrandOrderID)

	default:
		return nil
	}
}

func (s *OrdersLifeCycleService) SetDelivered(ctx context.Context, token, orderID string) error {
	//log := logger.FromContext(ctx)

	// 1) Auth
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	return s.DeliverOrder(ctx, user.UserID, user.MerchantID, orderID)
}

func (s *OrdersLifeCycleService) ReopenClosedOrder(ctx context.Context, token, orderID string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	// Ici user.MerchantID et user.UserID sont récupérés automatiquement

	err = s.ordersLifeCycleRepo.ReopenClosedOrder(ctx, user.MerchantID, orderID, user.UserID)

	s.notificationsService.SendNotificationAsync(user.MerchantID, orderID, "UPDATE_ORDER")

	return err
}

func (s *OrdersLifeCycleService) AddPayment(ctx context.Context, token string, orderID string, req *models.PaymentRequest) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	// sécurité : orderID dans l’URL > orderID dans req
	req.OrderID = orderID

	err = s.ordersLifeCycleRepo.AddPayment(ctx, user.MerchantID, user.UserID, req)

	s.notificationsService.SendNotificationAsync(user.MerchantID, orderID, "UPDATE_ORDER")

	return err
}

func (s *OrdersLifeCycleService) GetPayments(ctx context.Context, token string, orderID string) ([]models.Payment, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.ordersLifeCycleRepo.GetPaymentsForOrder(ctx, orderID)
}

func (s *OrdersLifeCycleService) DisablePayment(ctx context.Context, token, orderID, paymentID string) error {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	// TODO
	// Créer un GetPayment pour récupérer les meta data du paiement et vérifier qu'il ne s'agit pas d'un paiement Uber Eats ou Deliveroo non annulable
	// S'il s'agit d'un paiement Stripe, procéder à son annulation via l'API

	err = s.ordersLifeCycleRepo.DisablePayment(ctx, paymentID)

	s.notificationsService.SendNotificationAsync(user.MerchantID, orderID, "UPDATE_ORDER")

	return err
}

func (s *OrdersLifeCycleService) SetDistributedProducts(ctx context.Context, token string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	err = s.ordersLifeCycleRepo.SetDistributedProducts(ctx, user.UserID, user.MerchantID, req)
	if err != nil {
		return map[string]interface{}{
			"status": "-2",
			"error":  err.Error(),
		}, nil
	}

	// Notify
	s.notificationsService.SendNotificationAsync(user.MerchantID, req.OrderID, "UPDATE_ORDER")

	// 3 → Async integrations
	brand, err := s.ordersLifeCycleRepo.GetOrderBrand(ctx, req.OrderID)
	if err != nil {
		return nil, err
	}

	switch brand {
	case models.BrandUberEats:
		go s.uberSvc.SetOrderReady(user.UserID, user.MerchantID, req.OrderID, false)

	case models.BrandDeliveroo:
		go s.deliverooSvc.ReadyForCollection(req.OrderID)
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *OrdersLifeCycleService) BackToProduction(ctx context.Context, token, orderID string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	err = s.ordersLifeCycleRepo.MarkProductsBackToProduction(ctx, user.UserID, user.MerchantID, orderID, req.Products)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *OrdersLifeCycleService) SetOrderAccepted(ctx context.Context, UserID, MerchantID, orderID string) (models.HandlerDefaultResponseModelSet, error) {
	log := logger.FromContext(ctx)
	accept_order := models.HandlerDefaultResponseModelSet{}

	// 1) Get brand and merchant (we need merchant id to call integrators)
	orderMeta, err := s.ordersLifeCycleRepo.GetOrderBrandAndMerchant(ctx, orderID)
	if err != nil {
		log.Error("WEBHOOK DELIVEROO - " + err.Error())
		accept_order.Status = "error"
		return accept_order, err
	}

	log.Info("OrderLifeCycle.SetOrderAccepted - GetOrderBrandAndMerchant : " + orderMeta.BrandOrderID + " - " + orderMeta.Brand + " (merchant: " + orderMeta.MerchantID + ")")

	// 2) Update local order immediately (set OPEN, PENDING, ACCEPTED as in PHP)
	if err := s.ordersLifeCycleRepo.SetOrderAcceptedLocal(ctx, orderID); err != nil {
		accept_order.Status = "error"
		return accept_order, err
	}

	// 3) If brand is external, call integration ASYNC
	brand := orderMeta.Brand
	switch brand {
	case models.BrandUberEats:
		// call Uber Eats integration async
		log.Info("Async Call Uber Eats")
		go func(mID, oID string) {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.uberSvc.AcceptOrder(ctxTimeout, mID, oID); err != nil {
				s.log.Error("uber accept failed", zap.String("order_id", oID), zap.Error(err))
			}

			s.notificationsService.SendNotificationAsync(MerchantID, orderID, "UPDATE_ORDER")
		}(MerchantID, orderID)
	case models.BrandDeliveroo:
		if UserID != "WEBHOOK_DELIVEROO" {
			go func(mID, oID string) {
				ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := s.deliverooSvc.AcceptOrder(ctxTimeout, MerchantID, orderID); err != nil {
					s.log.Error("deliveroo accept failed", zap.String("order_id", oID), zap.Error(err))
				}

				s.notificationsService.SendNotificationAsync(MerchantID, orderID, "UPDATE_ORDER")
			}(MerchantID, orderID)

		} else {
			s.notificationsService.SendNotificationAsync(MerchantID, orderID, "UPDATE_ORDER")
		}
	default:
		// Internal order — nothing else to do
		s.notificationsService.SendNotificationAsync(MerchantID, orderID, "UPDATE_ORDER")
	}

	accept_order.Status = "success"
	return accept_order, err
}

func (s *OrdersLifeCycleService) AcceptOrder(ctx context.Context, token, orderID string) (models.HandlerDefaultResponseModelSet, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	accept_order := models.HandlerDefaultResponseModelSet{}
	if err != nil {
		accept_order.Status = "error"
		return accept_order, err
	}
	if user == nil {
		accept_order.Status = "error"
		return accept_order, models.ErrUnauthorized
	}

	return s.SetOrderAccepted(ctx, user.UserID, user.MerchantID, orderID)
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
	case models.BrandUberEats:
		go func() {
			//TODO recherche le bon endpoint chez Uber Eats
			//err := s.uberSvc.SetOrderStarted(ctx, integrationInfo.MerchantID, integrationInfo.BrandOrderID)
			if err != nil {
				log.Println("UberEats StartDelivery error:", err)
			}
		}()

	case models.BrandDeliveroo:
		go func() {
			err := s.deliverooSvc.StartDeliverooDelivery(ctx, integrationInfo.BrandOrderID)
			if err != nil {
				log.Println("Deliveroo StartDelivery error:", err)
			}
		}()
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *OrdersLifeCycleService) SetOrderDenied(ctx context.Context, OrderID string, in models.DenyOrderRequest) (map[string]string, error) {

	// 1) Get brand and merchant (we need merchant id to call integrators)
	orderMeta, err := s.ordersLifeCycleRepo.GetOrderBrandAndMerchant(ctx, OrderID)
	if err != nil {
		return nil, err
	}

	// 2) Update local order immediately and update payments
	err = s.ordersLifeCycleRepo.DenyOrderLocal(ctx,
		OrderID,
		in.DeletionReasonID,
		in.DeletionComment,
	)
	if err != nil {
		return nil, err
	}

	// Cancel stripe payments
	err = s.ordersLifeCycleRepo.CancelStripePayments(ctx, OrderID)
	if err != nil {
		return nil, fmt.Errorf("stripe cancel: %w", err)
	}

	// 3) If brand is external, call integration ASYNC
	brand := orderMeta.Brand
	merchantID := orderMeta.MerchantID
	switch brand {
	case models.BrandUberEats:
		// call Uber Eats integration async
		go func(mID, oID string) {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.uberSvc.DenyOrder(ctxTimeout, mID, oID, in.DeletionReasonID, in.DeletionReasonType, in.DeletionComment); err != nil {
				s.log.Error("uber deny failed", zap.String("order_id", oID), zap.Error(err))
			}

			s.notificationsService.SendNotificationAsync(in.MerchantID, OrderID, "UPDATE_ORDER")
		}(merchantID, OrderID)
	case models.BrandDeliveroo:
		go func(mID, oID string) {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.deliverooSvc.DenyOrder(ctxTimeout, oID, in); err != nil {
				s.log.Error("deliveroo deny failed", zap.String("order_id", oID), zap.Error(err))
			}

			s.notificationsService.SendNotificationAsync(in.MerchantID, OrderID, "UPDATE_ORDER")
		}(merchantID, OrderID)
	default:
		// Internal order — nothing else to do
		s.notificationsService.SendNotificationAsync(in.MerchantID, OrderID, "UPDATE_ORDER")
	}

	return map[string]string{"status": "1"}, nil
}

func (s *OrdersLifeCycleService) DenyOrder(ctx context.Context, token, OrderID string, in models.DenyOrderRequest) (map[string]string, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	in.UserID = user.UserID
	in.MerchantID = user.MerchantID

	return s.SetOrderDenied(ctx, OrderID, in)
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
	case models.BrandUberEats:
		go s.uberSvc.SetOrderReady(in.UserID, in.MerchantID, in.OrderID, false)

	case models.BrandDeliveroo:
		go s.deliverooSvc.ReadyForCollection(in.OrderID)
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
	case models.BrandUberEats:
		go s.uberSvc.CancelOrder(ctx, in.MerchantID, in.OrderID, in.DeletionReasonID, in.DeletionReasonType, in.DeletionComment)

	case models.BrandDeliveroo:
		// Eviter de rappeler l'api quand c'est une suppression par webhook
		if in.UserID != "WEBHOOK_DELIVEROO" {
			in_changed := models.DenyOrderRequest{
				DeletionComment:    in.DeletionComment,
				DeletionReasonType: in.DeletionReasonType,
				DeletionReasonID:   in.DeletionReasonID,
			}
			go s.deliverooSvc.CancelOrder(ctx, in.UserID, in.OrderID, in_changed)
		}
	}

	return nil
}

func (s *OrdersLifeCycleService) SetOrderDeleted(ctx context.Context, token string, in models.DenyOrderInput) error {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	in.MerchantID = user.MerchantID
	in.UserID = user.UserID

	return s.DeleteOrder(ctx, in)
}
