package order_life_cycle

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/redis"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/deliveroo"
	"welloresto-api/internal/modules/delivery_sessions"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/orders"
	"welloresto-api/internal/modules/receipt"
	"welloresto-api/internal/modules/ubereats"
	"welloresto-api/internal/utils/dbutils"
	receiptUtils "welloresto-api/internal/utils/receipt"

	"go.uber.org/zap"
)

type OrdersLifeCycleService struct {
	db                   *sql.DB
	auditService         audit.AuditService
	ordersLifeCycleRepo  *OrdersLifeCycleRepository
	ordersService        *orders.OrdersService
	deliverySessionsRepo *delivery_sessions.DeliverySessionsRepository
	uberSvc              *ubereats.UberEatsService
	deliverooSvc         *deliveroo.DeliverooService
	log                  *zap.Logger
	notificationsService *notification.NotificationService
	stripeManager        *stripeclient.StripeManager
	customersService     *customers.CustomersService
	redis                *redis.Client
	receiptService       receipt.ReceiptService
}

func NewOrdersLifeCycleService(ordersRepo *OrdersLifeCycleRepository, stripeSvc *stripeclient.StripeManager, uberSvc *ubereats.UberEatsService, deliverooSvc *deliveroo.DeliverooService, deliverySessionsRepo *delivery_sessions.DeliverySessionsRepository, log *zap.Logger, notificationsService *notification.NotificationService, customersService *customers.CustomersService, redis *redis.Client, auditService audit.AuditService, orders *orders.OrdersService, receiptService receipt.ReceiptService, db *sql.DB) *OrdersLifeCycleService {
	return &OrdersLifeCycleService{
		ordersLifeCycleRepo:  ordersRepo,
		deliverySessionsRepo: deliverySessionsRepo,
		uberSvc:              uberSvc,
		deliverooSvc:         deliverooSvc,
		log:                  log,
		notificationsService: notificationsService,
		stripeManager:        stripeSvc,
		customersService:     customersService,
		redis:                redis,
		receiptService:       receiptService,
		auditService:         auditService,
		db:                   db,
		ordersService:        orders,
	}
}

// Helper de type défini pour passer tes fonctions métiers
type OrderMutationFunc func(txCtx context.Context) error

func (s *OrdersLifeCycleService) ExecuteOrderMutation(ctx context.Context, MerchantID, UserID, orderID, action, resourceType string, work OrderMutationFunc) error {
	log := logger.FromContext(ctx)

	err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// 1. Snapshot AVANT
		var oldOrder interface{}
		oldOrders, errBefore := s.ordersService.ComputeGetOrder(txCtx, MerchantID, orderID)
		if errBefore == nil && len(oldOrders.Orders) > 0 {
			oldOrder = oldOrders.Orders[0]
		}

		// 4. Nettoyage Cache Redis (dans la transaction)
		if s.redis != nil {
			key := helpers.GetRedisOrderKey(MerchantID, orderID)
			s.redis.Delete(txCtx, key) // Note: ctx ou txCtx, Redis s'en fiche un peu, mais Delete est synchrone
			log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
		}

		// 2. Exécution de l'action métier
		if err := work(txCtx); err != nil {
			return err
		}

		// 3. Snapshot APRÈS
		var newOrder interface{}
		newOrders, errAfter := s.ordersService.ComputeGetOrder(txCtx, MerchantID, orderID)
		if errAfter == nil && len(newOrders.Orders) > 0 {
			newOrder = newOrders.Orders[0]
		}

		// 5. Enregistrement Audit sécurisé (Chaîné)
		return s.auditService.LogChange(txCtx, MerchantID, UserID, action, resourceType, orderID, oldOrder, newOrder)
	})

	if err != nil {
		return err // Si la tx échoue, on retourne l'erreur (Rollback automatique)
	}

	// --- 6. ACTIONS POST-COMMIT (Effets de bord) ---
	s.notificationsService.SendNotificationAsync(MerchantID, orderID, notification.NotificationTypeOrderUpdate)

	return nil
}

func (s *OrdersLifeCycleService) OrderStillOpen(ctx context.Context, orderID string) (bool, error) {
	return s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
}

func (s *OrdersLifeCycleService) DeleteOrder(ctx context.Context, in models.DenyOrderInput) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, in.OrderID)
	if err != nil {
		return err
	}
	if !orderStillOpen {
		return models.ErrOrderClosed
	}

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
	if err := s.customersService.ReactivateRewards(ctx, in.OrderID); err != nil {
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
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(user.MerchantID, in.OrderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
	}

	// Send notif
	// disabled because it is sent somewher else
	//s.notificationsService.SendNotificationAsync(in.MerchantID, in.OrderID, notification.NotificationTypeOrderUpdate)

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

func (s *OrdersLifeCycleService) SetOrderDeleted(ctx context.Context, in models.DenyOrderInput) error {
	// Comme in nécessite MerchantID et UserID avant exécution
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	in.MerchantID = user.MerchantID
	in.UserID = user.UserID

	return s.ExecuteOrderMutation(ctx, user.MerchantID, user.UserID, in.OrderID, models.ActionOrderDelete, models.ResourceOrder, func(txCtx context.Context) error {
		// Pareil, DeleteOrder doit bien utiliser le txCtx
		return s.DeleteOrder(txCtx, in)
	})
}

func (s *OrdersLifeCycleService) HandlerFiscalReceiptGeneration(ctx context.Context, merchantID, orderID string) error {

	// 2) --- Récupération des données complètes ---
	fullOrders, err := s.ordersService.ComputeGetOrder(ctx, merchantID, orderID)
	if err != nil {
		return fmt.Errorf("failed to fetch order details for receipt: %w", err)
	}

	fullOrder := fullOrders.Orders[0] // On suppose que la commande existe et qu'on a un seul résultat

	// 3) --- Construction des Snapshots ---
	itemsSnap := receiptUtils.BuildItemsSnapshot(fullOrder.Products, *fullOrder.OrderType)
	paymentsSnap := receiptUtils.BuildPaymentsSnapshot(fullOrder.Payments)

	// 4) --- Génération du Reçu Fiscal ---
	if err := s.receiptService.GenerateFiscalReceipt(ctx, &fullOrder, itemsSnap, paymentsSnap); err != nil {
		return fmt.Errorf("failed to generate fiscal receipt: %w", err)
	}

	return nil
}

func (s *OrdersLifeCycleService) DeliverOrder(ctx context.Context, UserID, MerchantID, orderID string) error {

	// 1) Mettre la commande en Delivered (local DB updates)
	// orderMeta contient Brand, BrandOrderID, etc.
	orderMeta, err := s.ordersLifeCycleRepo.SetDeliveredLocal(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.HandlerFiscalReceiptGeneration(ctx, MerchantID, orderID)

	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return err
	}

	// 5) Handle integration (Deliveroo, UberEats, etc.)
	switch orderMeta.Brand {
	case models.BrandUberEats:
		// No endpoint to call...
		return nil

	case models.BrandDeliveroo:
		if orderMeta.BrandOrderID != nil {
			go s.deliverooSvc.SetCollected(*orderMeta.BrandOrderID)
		}
		return nil

	default:
		return nil
	}
}

func (s *OrdersLifeCycleService) SetDelivered(ctx context.Context, orderID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
	if err != nil {
		return err
	}
	if !orderStillOpen {
		return models.ErrOrderClosed
	}

	return s.ExecuteOrderMutation(ctx, user.MerchantID, user.UserID, orderID, models.ActionOrderClose, models.ResourceOrder, func(txCtx context.Context) error {
		if err := s.customersService.ProcessOrderLoyalty(txCtx, orderID); err != nil {
			return err
		}
		return s.DeliverOrder(txCtx, user.UserID, user.MerchantID, orderID)
	})
}

func (s *OrdersLifeCycleService) SetDeliveredExternal(ctx context.Context, MerchantID, UserID, orderID string) error {
	// We don't check if order is still opened as it can be already closed by the merchant but we receive the delivery confirmation from the integrator (ex: Uber Eats)
	return s.ExecuteOrderMutation(ctx, MerchantID, UserID, orderID, models.ActionOrderClose, models.ResourceOrder, func(txCtx context.Context) error {
		if err := s.customersService.ProcessOrderLoyalty(txCtx, orderID); err != nil {
			return err
		}
		return s.DeliverOrder(txCtx, UserID, MerchantID, orderID)
	})
}

func (s *OrdersLifeCycleService) ReopenClosedOrder(ctx context.Context, orderID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
	if err != nil {
		return err
	}
	if orderStillOpen {
		return models.ErrOrderOpen
	}

	return s.ExecuteOrderMutation(ctx, user.MerchantID, user.UserID, orderID, models.ActionOrderReopen, models.ResourceOrder, func(txCtx context.Context) error {
		user, _ := middleware.UserFromContext(txCtx)
		return s.ordersLifeCycleRepo.ReopenClosedOrder(txCtx, user.MerchantID, orderID, user.UserID)
	})
}

func (s *OrdersLifeCycleService) AddPayment(ctx context.Context, orderID string, req *models.PaymentRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
	if err != nil {
		return err
	}
	if !orderStillOpen {
		return models.ErrOrderClosed
	}

	req.OrderID = orderID

	activeRegister, err := s.ordersLifeCycleRepo.GetActiveCashRegisterID(ctx, req.DeviceID)
	if err != nil && req.CashRegisterID == nil {
		log.Error(err.Error())
		return models.ErrNoCashRegisterOpen
	}

	payment := models.Payment{
		//PaymentID:     newPaymentID,
		OrderID:        req.OrderID,
		MerchantID:     user.MerchantID,
		UserID:         user.UserID,
		CashRegisterID: activeRegister, // On l'attache au registre d'AUJOURD'HUI
		MOP:            req.MOP,
		Amount:         req.Amount,
		OperationType:  models.OperationTypeSale,
		Comment:        &req.Comment,
		Code:           &req.Code,
	}

	return s.CreatePayment(ctx, payment)
}

func (s *OrdersLifeCycleService) CreatePayment(ctx context.Context, payment models.Payment) error {
	return s.ExecuteOrderMutation(ctx, payment.MerchantID, payment.UserID, payment.OrderID, models.ActionPaymentAdded, models.ResourcePayment, func(txCtx context.Context) error {
		return s.ordersLifeCycleRepo.AddPayment(txCtx, payment)
	})
}

func (s *OrdersLifeCycleService) GetPayments(ctx context.Context, orderID string) ([]models.Payment, error) {
	// Vérifier l'authentification (récupérer l'utilisateur depuis le contexte)
	_, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.ordersLifeCycleRepo.GetPaymentsForOrder(ctx, orderID)
}

func (s *OrdersLifeCycleService) DisablePayment(ctx context.Context, orderID, paymentID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
	if err != nil {
		return err
	}
	if !orderStillOpen {
		return models.ErrOrderClosed
	}

	log := logger.FromContext(ctx)

	// 1) Récupérer les informations du paiement
	paymentIntID, err := strconv.ParseInt(paymentID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid payment_id format: %w", err)
	}

	payment, err := s.ordersLifeCycleRepo.GetPayment(ctx, orderID, paymentIntID)
	if err != nil {
		return fmt.Errorf("failed to fetch payment: %w", err)
	}

	// 2) Vérifier qu'il ne s'agit pas d'un paiement DELIVEROO ou UBER_EATS (non annulable externalement)
	if payment.MOP == models.PaymentDeliveroo || payment.MOP == models.PaymentUberEats {
		return models.ErrCannotDisableExternalPayments
	}

	// 3) S'il s'agit d'un paiement Stripe, procéder à son annulation via l'API Stripe
	if payment.MOP == models.PaymentStripe {

		req := stripeclient.RefundRequest{
			IntentID:  *payment.IntentID,
			AccountID: *payment.AccountID,
		}

		go s.stripeManager.RefundOrCancelAsync(req)
	}

	// 4) Désactiver le paiement en base de données
	err = s.ordersLifeCycleRepo.DisablePayment(ctx, paymentID)
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(user.MerchantID, orderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
	}

	s.notificationsService.SendNotificationAsync(user.MerchantID, orderID, notification.NotificationTypeOrderUpdate)

	return err
}

func (s *OrdersLifeCycleService) SetDistributedProducts(ctx context.Context, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, req.OrderID)
	if err != nil {
		return nil, err
	}
	if !orderStillOpen {
		return nil, models.ErrOrderClosed
	}

	log := logger.FromContext(ctx)

	err = s.ordersLifeCycleRepo.SetDistributedProducts(ctx, user.UserID, user.MerchantID, req)
	if err != nil {
		return map[string]interface{}{
			"status": "-2",
			"error":  err.Error(),
		}, nil
	}
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(user.MerchantID, req.OrderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
	}

	// Notify
	s.notificationsService.SendNotificationAsync(user.MerchantID, req.OrderID, notification.NotificationTypeOrderUpdate)

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

func (s *OrdersLifeCycleService) BackToProduction(ctx context.Context, orderID string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	log := logger.FromContext(ctx)

	err = s.ordersLifeCycleRepo.MarkProductsBackToProduction(ctx, user.UserID, user.MerchantID, orderID, req.Products)
	if err != nil {
		return nil, err
	}
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(user.MerchantID, orderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *OrdersLifeCycleService) SetOrderAccepted(ctx context.Context, UserID, MerchantID, orderID string) (*models.HandlerDefaultResponseModelSet, error) {
	log := logger.FromContext(ctx)
	accept_order := &models.HandlerDefaultResponseModelSet{}

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
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(MerchantID, orderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
	}

	// 3) If brand is external, call integration ASYNC
	brand := orderMeta.Brand
	switch brand {
	case models.BrandUberEats:
		// call Uber Eats integration async
		log.Info("Async Call Uber Eats")
		go func(mID, oID string) {
			ctxTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := s.uberSvc.AcceptOrder(ctxTimeout, mID, oID); err != nil {
				s.log.Error("uber accept failed", zap.String("order_id", oID), zap.Error(err))
			}

			s.notificationsService.SendNotificationAsync(MerchantID, orderID, notification.NotificationTypeOrderUpdate)
		}(MerchantID, orderID)
	case models.BrandDeliveroo:
		if UserID != models.DeliverooWebhookUserID {
			go func(mID, oID string) {
				ctxTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				if err := s.deliverooSvc.AcceptOrder(ctxTimeout, MerchantID, orderID); err != nil {
					s.log.Error("deliveroo accept failed", zap.String("order_id", oID), zap.Error(err))
				}

				s.notificationsService.SendNotificationAsync(MerchantID, orderID, notification.NotificationTypeOrderUpdate)
			}(MerchantID, orderID)

		} else {
			s.notificationsService.SendNotificationAsync(MerchantID, orderID, notification.NotificationTypeOrderUpdate)
		}
	default:
		// Internal order — nothing else to do
		s.notificationsService.SendNotificationAsync(MerchantID, orderID, notification.NotificationTypeOrderUpdate)
	}

	accept_order.Status = "success"
	return accept_order, err
}

func (s *OrdersLifeCycleService) AcceptOrder(ctx context.Context, orderID string) (*models.HandlerDefaultResponseModelSet, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return &models.HandlerDefaultResponseModelSet{}, err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !orderStillOpen {
		return nil, models.ErrOrderClosed
	}

	return s.SetOrderAccepted(ctx, user.UserID, user.MerchantID, orderID)
}

func (s *OrdersLifeCycleService) StartDelivery(ctx context.Context, orderID string, userID string) (map[string]any, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return map[string]interface{}{"status": "0", "error": err.Error()}, err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !orderStillOpen {
		return nil, models.ErrOrderClosed
	}

	log := logger.FromContext(ctx)

	// 1) Update Wello DB
	integrationInfo, err := s.ordersLifeCycleRepo.MarkOrderAsDeliveryStarted(ctx, orderID, userID)
	if err != nil {
		return map[string]interface{}{"status": "0", "error": err.Error()}, err
	}
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(user.MerchantID, orderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
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
				//log.Println("UberEats StartDelivery error:", err)
			}
		}()

	case models.BrandDeliveroo:
		go func() {
			err := s.deliverooSvc.StartDeliverooDelivery(ctx, integrationInfo.BrandOrderID)
			if err != nil {
				//log.Println("Deliveroo StartDelivery error:", err)
			}
		}()
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *OrdersLifeCycleService) SetOrderDenied(ctx context.Context, OrderID string, in models.DenyOrderRequest) (map[string]string, error) {
	log := logger.FromContext(ctx)

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
	err = s.ordersLifeCycleRepo.DisablePayments(ctx, OrderID)
	if err != nil {
		return nil, fmt.Errorf("stripe cancel: %w", err)
	}
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(in.MerchantID, OrderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
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

			s.notificationsService.SendNotificationAsync(in.MerchantID, OrderID, notification.NotificationTypeOrderUpdate)
		}(merchantID, OrderID)
	case models.BrandDeliveroo:
		go func(mID, oID string) {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.deliverooSvc.DenyOrder(ctxTimeout, oID, in); err != nil {
				s.log.Error("deliveroo deny failed", zap.String("order_id", oID), zap.Error(err))
			}

			s.notificationsService.SendNotificationAsync(in.MerchantID, OrderID, notification.NotificationTypeOrderUpdate)
		}(merchantID, OrderID)
	default:
		// Internal order — nothing else to do
		s.notificationsService.SendNotificationAsync(in.MerchantID, OrderID, notification.NotificationTypeOrderUpdate)
	}

	return map[string]string{"status": "1"}, nil
}

func (s *OrdersLifeCycleService) DenyOrder(ctx context.Context, OrderID string, in models.DenyOrderRequest) (map[string]string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, OrderID)
	if err != nil {
		return nil, err
	}
	if !orderStillOpen {
		return nil, models.ErrOrderClosed
	}

	in.UserID = user.UserID
	in.MerchantID = user.MerchantID

	return s.SetOrderDenied(ctx, OrderID, in)
}

func (s *OrdersLifeCycleService) SetReadyForDistribution(ctx context.Context, in models.ReadyForDistributionInput) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, in.OrderID)
	if err != nil {
		return err
	}
	if !orderStillOpen {
		return models.ErrOrderClosed
	}

	in.UserID = user.UserID
	in.MerchantID = user.MerchantID

	// 1 → Wello local update
	if err := s.ordersLifeCycleRepo.SetReadyForDistribution(ctx, in.OrderID, in.MerchantID); err != nil {
		return err
	}
	if s.redis != nil {
		key := helpers.GetRedisOrderKey(in.MerchantID, in.OrderID)
		s.redis.Delete(ctx, key)
		log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
	}

	// 2 → Send notif
	s.notificationsService.SendNotificationAsync(in.MerchantID, in.OrderID, notification.NotificationTypeOrderUpdate)

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

func (s *OrdersLifeCycleService) UpdateProductionStatus(ctx context.Context, req *UpdateProductionStatusRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	log := logger.FromContext(ctx)

	// 1. Mise à jour en base de données
	affectedOrderIDs, err := s.ordersLifeCycleRepo.UpdateProductionStatus(ctx, user.MerchantID, req)
	if err != nil {
		return err
	}

	// 2. Invalidation du cache Redis (Déduplication)
	// On utilise un map pour ne traiter chaque OrderID qu'une seule fois
	processedIDs := make(map[string]bool)
	if s.redis != nil {
		for _, aOrderID := range affectedOrderIDs {
			if !processedIDs[aOrderID] {
				// Suppression de la clé centralisée
				key := helpers.GetRedisOrderKey(user.MerchantID, aOrderID)
				s.redis.Delete(ctx, key)
				log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")

				processedIDs[aOrderID] = true
			}
		}
	}

	// 3. Envoi des notifications
	// On boucle sur le map pour ne notifier qu'une fois par commande également
	for aOrderID := range processedIDs {
		s.notificationsService.SendNotificationAsync(user.MerchantID, aOrderID, notification.NotificationTypeOrderUpdate)
	}

	return nil
}

func (s *OrdersLifeCycleService) ProcessRefund(ctx context.Context, req models.RefundRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	// On démarre la transaction (j'utilise ta structure ExecuteOrderMutation)
	return s.ExecuteOrderMutation(ctx, user.MerchantID, user.UserID, req.OrderID, models.ActionOrderRefund, models.ResourceOrder, func(txCtx context.Context) error {
		log := logger.FromContext(txCtx)

		// 1. Vérifier qu'un registre de caisse est bien OUVERT pour ce Device/Merchant
		activeRegister, err := s.ordersLifeCycleRepo.GetActiveCashRegisterID(txCtx, req.DeviceID)
		if err != nil {
			return err
		}

		// 2. Récupérer le reçu fiscal d'origine
		originalReceipt, err := s.receiptService.GetReceiptByOrderID(txCtx, req.OrderID)
		if err != nil {
			log.Error(err.Error())
			return models.ErrReceiptNotFound
		}

		if originalReceipt.TotalTTC < req.Amount {
			log.Error(fmt.Sprintf("Refund amount is greater than original receipt total for receipt %s : %d > %d", originalReceipt.ReceiptID, req.Amount, originalReceipt.TotalTTC))
			return models.ErrRefoundMustBeLowerThanOriginalReceipt
		}

		// 3. Créer le paiement négatif
		// On force le montant en négatif ici. C'est la garantie backend.
		refundAmount := -req.Amount
		comment := fmt.Sprintf("Remboursement. Réf facture: %s. Motif: %s", originalReceipt.ReceiptNumber, req.Comment)

		refundPayment := models.Payment{
			//PaymentID:     helpers.GeneratePrefixedID("PAY"),
			OrderID:        req.OrderID,
			MerchantID:     user.MerchantID,
			UserID:         user.UserID,
			CashRegisterID: activeRegister, // On l'attache au registre d'AUJOURD'HUI
			MOP:            req.MOP,
			Amount:         refundAmount,
			OperationType:  models.OperationTypeRefund,
			Comment:        &comment,
		}

		if err := s.ordersLifeCycleRepo.AddPayment(txCtx, refundPayment); err != nil {
			return fmt.Errorf("échec de l'insertion du paiement : %w", err)
		}

		// 4. Générer le Reçu d'Avoir (Credit Note)
		// On crée un reçu avec un total négatif qui annule comptablement la vente
		err = s.receiptService.GenerateRefundReceipt(txCtx, user.MerchantID, req.OrderID, originalReceipt, refundAmount, req.MOP)
		if err != nil {
			return fmt.Errorf("échec de la génération de l'avoir fiscal : %w", err)
		}

		// (Optionnel) 5. Mettre à jour le statut de la commande si c'est un remboursement total
		// Si Amount remboursé == TotalTTC de la commande, alors Order.Status = "REFUNDED"
		// Sinon, Order.Status = "PARTIALLY_REFUNDED"

		return nil
	})
}

func (s *OrdersLifeCycleService) CreateOrder(ctx context.Context, req *models.RequestObject) (*models.CreateOrderResult, error) {
	log := logger.FromContext(ctx)

	result, err := s.ordersLifeCycleRepo.CreateOrder(ctx, req)

	if err != nil {
		log.Error(err.Error())
	} else {
		log.Info("🆕 New order created for merchant " + req.MerchantID + " : " + result.OrderID)
		s.notificationsService.SendNotificationAsync(req.MerchantID, result.OrderID, "NEW_ORDER")
	}

	return result, err
}

// This function will add Merchant ID and User ID to the payload
func (s *OrdersLifeCycleService) PrepareCreateOrder(ctx context.Context, req *models.RequestObject) (*models.CreateOrderResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.MerchantID = user.MerchantID
	req.Order.CreatedBy = &user.UserID

	return s.CreateOrder(ctx, req)
}

// This function will add Merchant_Id to the payload
func (s *OrdersLifeCycleService) PrepareUpdateOrder(ctx context.Context, req *models.RequestObject) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	orderStillOpen, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, *req.Order.OrderID)
	if err != nil {
		return err
	}
	if !orderStillOpen {
		return models.ErrOrderClosed
	}

	req.MerchantID = user.MerchantID
	req.Order.CreatedBy = &user.UserID

	return s.UpdateOrder(ctx, req)
}

func (s *OrdersLifeCycleService) UpdateOrder(ctx context.Context, req *models.RequestObject) error {
	log := logger.FromContext(ctx)

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	req.MerchantID = user.MerchantID

	// Tout ce qui est dans ce bloc est transactionnel !
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {

		// 1. Récupérer l'état AVANT (utilise txCtx pour rester dans la transaction)
		oldOrders, _ := s.ordersService.ComputeGetOrder(txCtx, user.MerchantID, *req.Order.OrderID)
		oldOrder := oldOrders.Orders[0] // on suppose qu'il y a toujours une commande, à adapter si besoin
		if err != nil {
			return err
		}

		// 2. Mettre à jour (utilise txCtx)
		if err := s.ordersLifeCycleRepo.UpdateOrder(txCtx, req); err != nil {
			return err
		}

		// Nettoyage Redis
		if s.redis != nil {
			key := helpers.GetRedisOrderKey(user.MerchantID, *req.Order.OrderID)
			s.redis.Delete(ctx, key)
			log.Info("🧠🚫 Order deleted from Redis cache 🚫🧠 (key: " + key + ")")
		}

		// 3. Récupérer l'état APRES
		// (ou construire le newOrder en mémoire si tu préfères éviter un SELECT)
		newOrders, err := s.ordersService.ComputeGetOrder(txCtx, user.MerchantID, *req.Order.OrderID)
		if err != nil {
			log.Error("failed to fetch updated order", zap.Error(err))
		}
		newOrder := newOrders.Orders[0] // on suppose qu'il y a toujours une commande, à adapter si besoin

		// 4. AUDIT : C'est dans la même transaction !
		err = s.auditService.LogChange(
			txCtx,
			user.MerchantID,
			user.UserID,
			models.ActionOrderUpdate,
			models.ResourceOrder,
			*req.Order.OrderID,
			oldOrder,
			newOrder,
		)
		if err != nil {
			return err // Si l'audit pète, ça fera un Rollback de tout le bloc !
		}

		return nil // Tout est bon, Commit !
	})

	if err != nil {
		log.Error("UpdateOrder transaction failed: " + err.Error())
		return err
	}

	// 5. Actions asynchrones / hors base de données (se font UNIQUEMENT si le commit a réussi)
	s.notificationsService.SendNotificationAsync(req.MerchantID, *req.Order.OrderID, notification.NotificationTypeOrderUpdate)

	return nil
}

func (s *OrdersLifeCycleService) ComputeEstimatedReady(ctx context.Context, id string) (string, error) {
	// On utilise 3 produits comme base pour l'estimation, mais cette logique peut être ajustée selon les besoins réels
	return s.ordersLifeCycleRepo.ComputeEstimatedReady(ctx, id, 3)
}
