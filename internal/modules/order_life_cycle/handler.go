package order_life_cycle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/delivery_sessions"
	"welloresto-api/internal/modules/notification"

	"github.com/go-chi/chi/v5"
)

// OrdersHandler handles orders endpoints
type OrdersLifeCycleHandler struct {
	ordersLifeCycleService  *OrdersLifeCycleService
	deliverySessionsService *delivery_sessions.DeliverySessionsService
	notificationsService    *notification.NotificationService
}

func NewOrdersLifeCycleHandler(ordersService *OrdersLifeCycleService, deliverySessionsService *delivery_sessions.DeliverySessionsService, notificationsService *notification.NotificationService) *OrdersLifeCycleHandler {
	return &OrdersLifeCycleHandler{
		ordersLifeCycleService:  ordersService,
		deliverySessionsService: deliverySessionsService,
		notificationsService:    notificationsService,
	}
}

func (h *OrdersLifeCycleHandler) ReopenClosedOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "reopen_closed_order", map[string]string{"error": "missing_parameter"})
		return
	}

	err := h.ordersLifeCycleService.ReopenClosedOrder(ctx, orderID)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "reopen_closed_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "reopen_closed_order", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) AddPayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "add_payment", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "add_payment", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.ordersLifeCycleService.AddPayment(ctx, orderID, &req)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "add_payment", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "add_payment", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) GetPayments(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")

	payments, err := h.ordersLifeCycleService.GetPayments(r.Context(), orderID)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "get_payments", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "get_payments", map[string]interface{}{
		"payments": payments,
	})
}

func (h *OrdersLifeCycleHandler) DeletePayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	paymentID := chi.URLParam(r, "payment_id")
	orderID := chi.URLParam(r, "order_id")

	err := h.ordersLifeCycleService.DisablePayment(ctx, orderID, paymentID)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "delete_payment", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "delete_payment", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) SetDistributedProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	var req models.SetDistributedProductsRequest
	json.NewDecoder(r.Body).Decode(&req)

	// force orderID from URL
	req.OrderID = orderID

	resp, err := h.ordersLifeCycleService.SetDistributedProducts(ctx, &req)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "set_distributed_products", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "set_distributed_products", resp)
}

func (h *OrdersLifeCycleHandler) BackToProduction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	var req models.SetDistributedProductsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "back_to_production", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.ordersLifeCycleService.BackToProduction(ctx, orderID, &req)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "back_to_production", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "back_to_production", result)
}

func (h *OrdersLifeCycleHandler) AcceptOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "accept_order", map[string]string{"error": "missing_parameter"})
		return
	}

	// Call service (non blocking), but return result of immediate DB update
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := h.ordersLifeCycleService.AcceptOrder(ctx2, orderID)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "accept_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "accept_order", res)
}

func (h *OrdersLifeCycleHandler) StartDelivery(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "start_delivery", map[string]string{"error": "missing_parameter"})
		return
	}

	userID := r.URL.Query().Get("user_id") // nécessaire, identique au PHP
	if userID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "start_delivery", map[string]string{"error": "missing_parameter"})
		return
	}

	resp, err := h.ordersLifeCycleService.StartDelivery(r.Context(), orderID, userID)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "start_delivery", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "start_delivery", resp)
}

func (h *OrdersLifeCycleHandler) DenyOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "deny_order", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.DenyOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "deny_order", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.ordersLifeCycleService.DenyOrder(r.Context(), orderID, req)

	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "deny_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "deny_order", nil)
}

func (h *OrdersLifeCycleHandler) SetReadyForDistribution(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "set_ready_for_distribution", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.ReadyForDistributionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "set_ready_for_distribution", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.ordersLifeCycleService.SetReadyForDistribution(r.Context(), models.ReadyForDistributionInput{
		OrderID: orderID,
	})

	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "set_ready_for_distribution", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "set_ready_for_distribution", models.CashRegisterHistoryResponse{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) DeleteOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "delete_order", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.DenyOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "delete_order", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.ordersLifeCycleService.SetOrderDeleted(r.Context(), models.DenyOrderInput{
		OrderID:          orderID,
		MerchantID:       req.MerchantID,
		UserID:           req.UserID,
		DeletionReasonID: req.DeletionReasonID,
		DeletionComment:  req.DeletionComment,
	})

	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "delete_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "delete_order", models.CashRegisterHistoryResponse{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) SetDelivered(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	err := h.ordersLifeCycleService.SetDelivered(ctx, orderID)
	if err != nil {

		var notPaidErr *models.OrderNotFullyPaidError
		if errors.As(err, &notPaidErr) {
			models.SendJSON(w, http.StatusNotAcceptable, "order_life_cycle", "set_delivered", map[string]interface{}{
				"error": "order_not_fully_paid",
				"details": map[string]interface{}{
					"order_id":    notPaidErr.OrderID,
					"paid_amount": notPaidErr.PaidAmount,
					"price":       notPaidErr.Price,
				},
			})
			return
		}

		models.SendErrorJSON(w, "order_life_cycle", "set_delivered", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "set_delivered", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) UpdateProductionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req UpdateProductionStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "update_production_status", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.ordersLifeCycleService.UpdateProductionStatus(ctx, &req)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "update_production_status", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "update_production_status", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) HandleRefund(w http.ResponseWriter, r *http.Request) {
	// 2. Décodage de la requête
	var req models.RefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Sécurité : On s'assure que le montant est strictement positif dans la requête
	// (on l'inversera dans le backend pour garantir qu'on ne fait pas de fausse vente)
	if req.Amount <= 0 {
		models.SendErrorJSON(w, "order_life_cycle", "refund", models.ErrRefoundMustBeGreaterThanZero)
		return
	}
	if req.DeviceID == "" {
		models.SendErrorJSON(w, "order_life_cycle", "refund", models.ErrDeviceIDMissing)
		return
	}
	if req.MOP == "" {
		models.SendErrorJSON(w, "order_life_cycle", "refund", models.ErrMOPMissing)
		return
	}

	req.OrderID = chi.URLParam(r, "order_id")

	// 3. Appel du service métier
	err := h.ordersLifeCycleService.ProcessRefund(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "refund", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "refund", map[string]string{"status": "success", "message": "Refund processed successfully"})
}

func (h *OrdersLifeCycleHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("PrepareCreateOrder bad request : " + err.Error())
		models.SendErrorJSON(w, "orders", "create_order", err)
		return
	}

	result, err := h.ordersLifeCycleService.PrepareCreateOrder(ctx, &req)
	if err != nil {
		models.SendErrorJSON(w, "orders", "create_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "create_order", result)
}

func (h *OrdersLifeCycleHandler) UpdateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("PrepareCreateOrder bad request : " + err.Error())
		models.SendErrorJSON(w, "orders", "update_order", err)
		return
	}

	orderID := chi.URLParam(r, "order_id")
	req.Order.OrderID = &orderID

	err := h.ordersLifeCycleService.PrepareUpdateOrder(ctx, &req)
	if err != nil {
		log.Error("PrepareCreateOrder error : " + err.Error())
		models.SendErrorJSON(w, "orders", "update_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "update_order", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}
