package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

// OrdersHandler handles orders endpoints
type OrdersLifeCycleHandler struct {
	ordersLifeCycleService  *services.OrdersLifeCycleService
	deliverySessionsService *services.DeliverySessionsService
}

func NewOrdersLifeCycleHandler(ordersService *services.OrdersLifeCycleService, deliverySessionsService *services.DeliverySessionsService) *OrdersLifeCycleHandler {
	return &OrdersLifeCycleHandler{
		ordersLifeCycleService:  ordersService,
		deliverySessionsService: deliverySessionsService,
	}
}

func (h *OrdersLifeCycleHandler) ReopenClosedOrder(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
	token := extractToken(r)
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
	token := extractToken(r)
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
	token := extractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	paymentID := chi.URLParam(r, "payment_id")

	err := h.ordersLifeCycleService.DisablePayment(ctx, token, paymentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "1"})
}

func (h *OrdersLifeCycleHandler) SetDistributedProducts(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
	ctx := r.Context()
	token := extractToken(r)
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
