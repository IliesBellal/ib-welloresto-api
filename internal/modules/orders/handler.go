package orders

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/upsell"

	"github.com/go-chi/chi/v5"
)

// OrdersHandler handles orders endpoints
type OrdersHandler struct {
	ordersService *OrdersService
	upsellService *upsell.Service
}

func NewOrdersHandler(ordersService *OrdersService, upsellService *upsell.Service) *OrdersHandler {
	return &OrdersHandler{
		ordersService: ordersService,
		upsellService: upsellService,
	}
}

func (h *OrdersHandler) GetPendingOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	app := r.URL.Query().Get("app")
	if app == "" {
		app = "WR_RECEPTION"
	}

	orders, err := h.ordersService.GetPendingOrders(ctx, app)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "pending", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "pending", models.PendingOrdersData{
		Orders: orders.Orders,
	})
}

func (h *OrdersHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_order", map[string]string{"error": "missing_parameter"})
		return
	}

	order, err := h.ordersService.GetOrder(ctx, orderID)
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
	ctx := r.Context()

	var req models.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_orders", map[string]string{"error": "invalid_body"})
		return
	}

	orders, err := h.ordersService.GetOrders(ctx, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "get_orders", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_orders", models.PendingOrdersData{
		Orders: orders,
	})
}

func (h *OrdersHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req models.OrderHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_history", map[string]string{"error": "invalid_body"})
		return
	}
	orders, err := h.ordersService.GetHistory(ctx, req)

	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "get_history", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_history", orders)
}

func (h *OrdersHandler) GetPricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	//log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_pricing", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.ordersService.GetPricing(ctx, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "orders", "get_pricing", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_pricing", result)
}

// GetUpsell handles POST /orders/upsell.
func (h *OrdersHandler) GetUpsell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("GetUpsell bad request: " + err.Error())
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_upsell", map[string]string{"error": "invalid_body"})
		return
	}

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		models.SendJSON(w, http.StatusUnauthorized, "orders", "get_upsell", map[string]string{"error": "unauthorized"})
		return
	}

	if req.Order == nil || len(req.Order.Products) == 0 {
		models.SendJSON(w, http.StatusBadRequest, "orders", "get_upsell", map[string]string{"error": "empty_cart"})
		return
	}

	cartProducts := make([]models.ProductEntry, 0, len(req.Order.Products))
	for _, p := range req.Order.Products {
		qty := p.Quantity
		cartProducts = append(cartProducts, models.ProductEntry{
			ProductID: p.ProductID,
			Name:      p.ProductName,
			Price:     int64(p.Price),
			Quantity:  &qty,
		})
	}

	result, err := h.upsellService.GenerateUpsell(ctx, user.MerchantID, cartProducts)
	if err != nil {
		log.Error("GetUpsell service error: " + err.Error())
		models.SendJSON(w, http.StatusOK, "orders", "get_upsell", map[string]interface{}{
			"suggestion_id": "",
			"suggestions":   []interface{}{},
			"source":        "error_fallback",
		})
		return
	}

	models.SendJSON(w, http.StatusOK, "orders", "get_upsell", result)
}
