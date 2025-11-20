package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/services"
)

// OrdersHandler handles orders endpoints
type DeliverySessionsHandler struct {
	deliverySessionsService *services.DeliverySessionsService
}

func NewDeliverySessionsHandler(deliverySessionsService *services.DeliverySessionsService) *DeliverySessionsHandler {
	return &DeliverySessionsHandler{
		deliverySessionsService: deliverySessionsService,
	}
}

// GET /delivery/sessions

func (h *DeliverySessionsHandler) GetDeliverySessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	sessions, err := h.deliverySessionsService.GetDeliverySessions(ctx, token)
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
