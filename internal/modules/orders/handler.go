package orders

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// OrdersHandler handles orders endpoints
type OrdersHandler struct {
	ordersService *OrdersService
}

func NewOrdersHandler(ordersService *OrdersService) *OrdersHandler {
	return &OrdersHandler{
		ordersService: ordersService,
	}
}

func (h *OrdersHandler) GetPendingOrders(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	app := r.URL.Query().Get("app")
	if app == "" {
		app = "WR_RECEPTION"
	}

	orders, err := h.ordersService.GetPendingOrders(ctx, token, app)
	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID: "10",
		Data: models.PendingOrdersData{
			Orders: orders.Orders,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *OrdersHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
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

func (h *OrdersHandler) GetOrders(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()

	var req models.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	orders, err := h.ordersService.GetOrders(ctx, token, &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID: "10",
		Data: models.PendingOrdersData{
			Orders: orders,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *OrdersHandler) UpdateMultipleProductsStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()

	var req models.MultipleProductsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.ordersService.UpdateMultipleProductsStatus(ctx, &req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, `{"status":"ok"}`, http.StatusOK)
}

func (h *OrdersHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

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
	orders, err := h.ordersService.GetHistory(ctx, token, req)

	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	order_history := models.HandlerDefaultResponse{
		ID: "10",
		Data: models.PendingOrdersData{
			Orders: orders,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order_history)
}

func (h *OrdersHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := h.ordersService.CreateOrder(ctx, token, &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *OrdersHandler) GetPricing(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result, err := h.ordersService.GetPricing(ctx, token, &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID:   "10",
		Data: result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
