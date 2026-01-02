package order_life_cycle

import (
	"context"
	"encoding/json"
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
		http.Error(w, "error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"status": "1"})
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
		http.Error(w, "error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "1"})
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "1"})
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(result)
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
		http.Error(w, `{"status":"-2","error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
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
		http.Error(w, `{"status":"0","error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
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

	err := h.ordersLifeCycleService.DenyOrder(r.Context(), models.DenyOrderInput{
		OrderID:            orderID,
		DeletionReasonID:   req.DeletionReasonID,
		DeletionReasonType: req.DeletionReasonType,
		DeletionComment:    req.DeletionComment,
		UserID:             req.UserID,
		MerchantID:         req.MerchantID,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

	err := h.ordersLifeCycleService.DeleteOrder(r.Context(), models.DenyOrderInput{
		OrderID:          orderID,
		MerchantID:       req.MerchantID,
		UserID:           req.UserID,
		DeletionReasonID: req.DeletionReasonID,
		DeletionComment:  req.DeletionComment,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	delete_order := models.HandlerDefaultResponse{
		ID: "10",
		Data: models.CashRegisterHistoryResponse{
			Status: "success",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(delete_order)
}

func (h *OrdersLifeCycleHandler) SetDelivered(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	orderID := chi.URLParam(r, "order_id")

	err := h.ordersLifeCycleService.SetDelivered(ctx, token, orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
