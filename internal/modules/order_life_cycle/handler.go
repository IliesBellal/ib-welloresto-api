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
		http.Error(w, "missing order_id", http.StatusBadRequest)
		return
	}

	err := h.ordersLifeCycleService.ReopenClosedOrder(ctx, token, orderID)
	if err != nil {
		models.SendJSON(w, "order_life_cycle", "ReopenClosedOrder_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "ReopenClosedOrder", models.HandlerDefaultResponseModelSet{
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
		http.Error(w, "missing order_id", http.StatusBadRequest)
		return
	}

	var req models.PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	err := h.ordersLifeCycleService.AddPayment(ctx, token, orderID, &req)
	if err != nil {
		models.SendJSON(w, "order_life_cycle", "AddPayment_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "AddPayment", models.HandlerDefaultResponseModelSet{
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
		models.SendJSON(w, "order_life_cycle", "GetPayments_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "GetPayments", map[string]interface{}{
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
		models.SendJSON(w, "order_life_cycle", "DeletePayment_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "DeletePayment", models.HandlerDefaultResponseModelSet{
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
		models.SendJSON(w, "order_life_cycle", "SetDistributedProducts_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "SetDistributedProducts", resp)
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
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	result, err := h.ordersLifeCycleService.BackToProduction(ctx, token, orderID, &req)
	if err != nil {
		models.SendJSON(w, "order_life_cycle", "BackToProduction_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "BackToProduction", result)
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
		http.Error(w, `{"status":"-1","error":"missing order_id"}`, http.StatusBadRequest)
		return
	}

	// Call service (non blocking), but return result of immediate DB update
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := h.ordersLifeCycleService.AcceptOrder(ctx2, token, orderID)
	if err != nil {
		models.SendJSON(w, "order_life_cycle", "AcceptOrder_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "AcceptOrder", res)
}

func (h *OrdersLifeCycleHandler) StartDelivery(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		http.Error(w, `{"status":"-1","error":"missing order_id"}`, http.StatusBadRequest)
		return
	}

	userID := r.URL.Query().Get("user_id") // nécessaire, identique au PHP
	if userID == "" {
		http.Error(w, `{"status":"-1","error":"missing user_id"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.ordersLifeCycleService.StartDelivery(r.Context(), token, orderID, userID)
	if err != nil {
		models.SendJSON(w, "order_life_cycle", "StartDelivery_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "StartDelivery", resp)
}

func (h *OrdersLifeCycleHandler) DenyOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		http.Error(w, `{"status":"-1","error":"invalid order_id"}`, http.StatusBadRequest)
		return
	}

	var req models.DenyOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	res, err := h.ordersLifeCycleService.DenyOrder(r.Context(), token, orderID, req)

	if err != nil {
		models.SendJSON(w, "order_life_cycle", "DenyOrder_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "DenyOrder", res)
}

func (h *OrdersLifeCycleHandler) SetReadyForDistribution(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		http.Error(w, `{"status":"-1","error":"invalid order_id"}`, http.StatusBadRequest)
		return
	}

	var req models.ReadyForDistributionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	err := h.ordersLifeCycleService.SetReadyForDistribution(r.Context(), models.ReadyForDistributionInput{
		OrderID:    orderID,
		MerchantID: req.MerchantID,
		UserID:     req.UserID,
	})

	if err != nil {
		models.SendJSON(w, "order_life_cycle", "SetReadyForDistribution_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "SetReadyForDistribution", models.CashRegisterHistoryResponse{
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
		http.Error(w, `{"status":"-1","error":"invalid order_id"}`, http.StatusBadRequest)
		return
	}

	var req models.DenyOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
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
		models.SendJSON(w, "order_life_cycle", "DeleteOrder_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "DeleteOrder", models.CashRegisterHistoryResponse{
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
			models.SendJSON(w, "order_life_cycle", "SetDelivered_error", map[string]interface{}{
				"error": "order_not_fully_paid",
				"details": map[string]interface{}{
					"order_id":    notPaidErr.OrderID,
					"paid_amount": notPaidErr.PaidAmount,
					"price":       notPaidErr.Price,
				},
			})
			return
		}

		models.SendJSON(w, "order_life_cycle", "SetDelivered_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "order_life_cycle", "SetDelivered", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}
