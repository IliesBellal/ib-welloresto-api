package orders

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
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
		models.SendJSON(w, "orders", "GetPendingOrders_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "GetPendingOrders", models.PendingOrdersData{
		Orders: orders.Orders,
	})
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
			models.SendJSON(w, "orders", "GetOrder_error", map[string]string{"error": "order not found"})
			return
		}
		models.SendJSON(w, "orders", "GetOrder_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "GetOrder", order)
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
		models.SendJSON(w, "orders", "GetOrders_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "GetOrders", models.PendingOrdersData{
		Orders: orders,
	})
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
		models.SendJSON(w, "orders", "UpdateMultipleProductsStatus_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "UpdateMultipleProductsStatus", map[string]string{"status": "ok"})
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
		models.SendJSON(w, "orders", "GetHistory_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "GetHistory", models.PendingOrdersData{
		Orders: orders,
	})
}

func (h *OrdersHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("PrepareCreateOrder bad request : " + err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := h.ordersService.PrepareCreateOrder(ctx, token, &req)
	if err != nil {
		log.Error("PrepareCreateOrder error : " + err.Error())
		models.SendJSON(w, "orders", "CreateOrder_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "CreateOrder", result)
}

func (h *OrdersHandler) UpdateOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("PrepareCreateOrder bad request : " + err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := h.ordersService.PrepareUpdateOrder(ctx, token, &req)
	if err != nil {
		log.Error("PrepareCreateOrder error : " + err.Error())
		models.SendJSON(w, "orders", "UpdateOrder_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "UpdateOrder", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersHandler) GetPricing(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	//log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result, err := h.ordersService.GetPricing(ctx, token, &req)
	if err != nil {
		models.SendJSON(w, "orders", "GetPricing_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "orders", "GetPricing", result)
}
