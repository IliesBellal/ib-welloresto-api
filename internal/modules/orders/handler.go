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
		models.SendJSON(w, http.StatusUnauthorized, "orders", "get_pending_orders", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	app := r.URL.Query().Get("app")
	if app == "" {
		app = "WR_RECEPTION"
	}

	orders, err := h.ordersService.GetPendingOrders(ctx, token, app)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "pending", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "pending", models.PendingOrdersData{
		Orders: orders.Orders,
	})
}

func (h *OrdersHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "get_order", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_order", map[string]string{"error": "missing_parameter"})
		return
	}

	order, err := h.ordersService.GetOrder(ctx, token, orderID)
	if err != nil {
		if err == sql.ErrNoRows {
			models.SendJSON(w, http.StatusNotFound, "orders", "get_order", map[string]string{"error": "order not found"})
			return
		}
		logger.FromContext(ctx).Error(err.Error())
		models.SendJSON(w, http.StatusInternalServerError, "orders", "get_order", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_order", order)
}

func (h *OrdersHandler) GetOrders(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "get_orders", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	var req models.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_orders", map[string]string{"error": "invalid_body"})
		return
	}

	orders, err := h.ordersService.GetOrders(ctx, token, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "get_orders", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_orders", models.PendingOrdersData{
		Orders: orders,
	})
}

func (h *OrdersHandler) UpdateMultipleProductsStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "update_multiple_products_status", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	var req models.MultipleProductsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "orders", "update_multiple_products_status", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.ordersService.UpdateMultipleProductsStatus(ctx, &req); err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "update_multiple_products_status", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "update_multiple_products_status", map[string]string{"status": "ok"})
}

func (h *OrdersHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "get_history", map[string]string{"error": "missing_token"})
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
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_history", map[string]string{"error": "invalid_body"})
		return
	}
	orders, err := h.ordersService.GetHistory(ctx, token, req)

	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "get_history", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_history", models.PendingOrdersData{
		Orders: orders,
	})
}

func (h *OrdersHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "create_order", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("PrepareCreateOrder bad request : " + err.Error())
		models.SendJSON(w, http.StatusBadRequest, "orders", "create_order", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.ordersService.PrepareCreateOrder(ctx, token, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "create_order", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "create_order", result)
}

func (h *OrdersHandler) UpdateOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "update_order", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("PrepareCreateOrder bad request : " + err.Error())
		models.SendJSON(w, http.StatusBadRequest, "orders", "update_order", map[string]string{"error": "invalid_body"})
		return
	}

	orderID := chi.URLParam(r, "order_id")
	req.Order.OrderID = &orderID

	err := h.ordersService.PrepareUpdateOrder(ctx, token, &req)
	if err != nil {
		log.Error("PrepareCreateOrder error : " + err.Error())
		models.SendJSON(w, http.StatusInternalServerError, "orders", "update_order", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "update_order", models.HandlerDefaultResponseModelSet{
		Status: "success",
	})
}

func (h *OrdersHandler) GetPricing(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "get_pricing", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	//log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_pricing", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.ordersService.GetPricing(ctx, token, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "get_pricing", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_pricing", result)
}
