package order_life_cycle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
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
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "reopen_closed_order", map[string]string{"error": "missing_parameter"})
		return
	}

	err := h.ordersLifeCycleService.ReopenClosedOrder(ctx, token, orderID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "reopen_closed_order", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "reopen_closed_order", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) AddPayment(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

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

	err := h.ordersLifeCycleService.AddPayment(ctx, token, orderID, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "add_payment", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "add_payment", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) GetPayments(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	orderID := chi.URLParam(r, "order_id")

	payments, err := h.ordersLifeCycleService.GetPayments(r.Context(), token, orderID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "get_payments", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "get_payments", map[string]interface{}{
		"payments": payments,
	})
}

func (h *OrdersLifeCycleHandler) DeletePayment(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	paymentID := chi.URLParam(r, "payment_id")
	orderID := chi.URLParam(r, "order_id")

	err := h.ordersLifeCycleService.DisablePayment(ctx, token, orderID, paymentID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "delete_payment", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "delete_payment", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) SetDistributedProducts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	var req models.SetDistributedProductsRequest
	json.NewDecoder(r.Body).Decode(&req)

	// force orderID from URL
	req.OrderID = orderID

	resp, err := h.ordersLifeCycleService.SetDistributedProducts(ctx, token, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "set_distributed_products", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "set_distributed_products", resp)
}

func (h *OrdersLifeCycleHandler) BackToProduction(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	var req models.SetDistributedProductsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "back_to_production", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.ordersLifeCycleService.BackToProduction(ctx, token, orderID, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "back_to_production", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "back_to_production", result)
}

func (h *OrdersLifeCycleHandler) AcceptOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "accept_order", map[string]string{"error": "missing_parameter"})
		return
	}

	// Call service (non blocking), but return result of immediate DB update
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := h.ordersLifeCycleService.AcceptOrder(ctx2, token, orderID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "accept_order", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "accept_order", res)
}

func (h *OrdersLifeCycleHandler) StartDelivery(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

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

	resp, err := h.ordersLifeCycleService.StartDelivery(r.Context(), token, orderID, userID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "start_delivery", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "start_delivery", resp)
}

func (h *OrdersLifeCycleHandler) DenyOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

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

	res, err := h.ordersLifeCycleService.DenyOrder(r.Context(), token, orderID, req)

	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "deny_order", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "deny_order", res)
}

func (h *OrdersLifeCycleHandler) SetReadyForDistribution(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

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
		OrderID:    orderID,
		MerchantID: req.MerchantID,
		UserID:     req.UserID,
	})

	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "set_ready_for_distribution", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "set_ready_for_distribution", models.CashRegisterHistoryResponse{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) DeleteOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

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

	err := h.ordersLifeCycleService.SetOrderDeleted(r.Context(), token, models.DenyOrderInput{
		OrderID:          orderID,
		MerchantID:       req.MerchantID,
		UserID:           req.UserID,
		DeletionReasonID: req.DeletionReasonID,
		DeletionComment:  req.DeletionComment,
	})

	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "delete_order", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "delete_order", models.CashRegisterHistoryResponse{
		Status: "success",
	})
}

func (h *OrdersLifeCycleHandler) SetDelivered(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	orderID := chi.URLParam(r, "order_id")

	err := h.ordersLifeCycleService.SetDelivered(ctx, token, orderID)
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

		models.SendJSON(w, http.StatusInternalServerError, "order_life_cycle", "set_delivered", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "order_life_cycle", "set_delivered", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}
