package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
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

// GET /delivery_sessions/pending
func (h *DeliverySessionsHandler) GetPendingDeliverySessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	sessions, err := h.deliverySessionsService.GetPendingDeliverySessions(ctx, token)
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

func (h *DeliverySessionsHandler) StartDeliverySession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	var req models.DeliverySessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.deliverySessionsService.StartDeliverySession(ctx, token, &req)
	handleJSON(w, resp, err)
}

func (h *DeliverySessionsHandler) CancelDeliverySession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CancelDeliverySession(ctx, id)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, resp, http.StatusOK)
}

func (h *DeliverySessionsHandler) CloseDeliverySession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CloseDeliverySession(ctx, id)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, resp, http.StatusOK)
}
