package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

// OrdersHandler handles orders endpoints
type OrdersHandler struct {
	ordersService           *services.OrdersService
	deliverySessionsService *services.DeliverySessionsService
}

func NewOrdersHandler(ordersService *services.OrdersService, deliverySessionsService *services.DeliverySessionsService) *OrdersHandler {
	return &OrdersHandler{
		ordersService:           ordersService,
		deliverySessionsService: deliverySessionsService,
	}
}

// GET /orders/pending
func (h *OrdersHandler) GetPendingOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// read app param from query (default WR_RECEPTION)
	app := r.URL.Query().Get("app")
	if app == "" {
		// default to WR_RECEPTION as in legacy
		app = "WR_RECEPTION"
	}

	resp, err := h.ordersService.GetPendingOrders(ctx, token, app)
	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *OrdersHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := extractToken(r)

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		http.Error(w, "missing order_id", http.StatusBadRequest)
		return
	}

	order, err := h.ordersService.GetOrder(ctx, token, orderID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(order)
}

func (h *OrdersHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// read app param from query (default WR_RECEPTION)
	app := r.URL.Query().Get("app")
	if app == "" {
		// default to WR_RECEPTION as in legacy
		app = "WR_RECEPTION"
	}

	var req models.OrderHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	resp, err := h.ordersService.GetHistory(ctx, token, req)

	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *OrdersHandler) GetPayments(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")

	token := extractToken(r)

	payments, err := h.ordersService.GetPayments(r.Context(), token, orderID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"payments": payments,
	})
}

func (h *OrdersHandler) DeletePayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	paymentID := chi.URLParam(r, "payment_id")

	err := h.ordersService.DisablePayment(ctx, token, paymentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "1"})
}
