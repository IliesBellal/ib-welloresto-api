package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"welloresto-api/internal/services"
)

// OrdersHandler handles orders endpoints
type OrdersHandler struct {
	ordersService *services.OrdersService
}

func NewOrdersHandler(ordersService *services.OrdersService) *OrdersHandler {
	return &OrdersHandler{ordersService: ordersService}
}

// helper to extract token either from Authorization header (Bearer ...) or token query param
func extractToken(r *http.Request) string {
	// Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		// allow "Bearer <token>" or raw token
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			return strings.TrimSpace(auth[7:])
		}
		return strings.TrimSpace(auth)
	}
	// fallback to query param token (legacy)
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
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

// GET /delivery/sessions
func (h *OrdersHandler) GetDeliverySessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	sessions, err := h.ordersService.GetDeliverySessions(ctx, token)
	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"delivery_sessions": sessions,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
