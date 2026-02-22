package delivery_sessions

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// OrdersHandler handles orders endpoints
type DeliverySessionsHandler struct {
	deliverySessionsService *DeliverySessionsService
}

func NewDeliverySessionsHandler(deliverySessionsService *DeliverySessionsService) *DeliverySessionsHandler {
	return &DeliverySessionsHandler{
		deliverySessionsService: deliverySessionsService,
	}
}

// GET /delivery_sessions/pending
func (h *DeliverySessionsHandler) GetPendingDeliverySessions(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "pending", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()

	sessions, err := h.deliverySessionsService.GetPendingDeliverySessions(ctx, token)
	if err != nil {
		models.SendErrorJSON(w, "delivery_sessions", "pending", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "pending", map[string]interface{}{
		"status":            "success",
		"delivery_sessions": sessions,
	})
}

func (h *DeliverySessionsHandler) StartDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "start", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()

	var req models.DeliverySessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "delivery_sessions", "start", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.deliverySessionsService.StartDeliverySession(ctx, token, &req)
	if err != nil {
		models.SendErrorJSON(w, "delivery_sessions", "start", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "start", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

func (h *DeliverySessionsHandler) CancelDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "cancel", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CancelDeliverySession(ctx, token, id)
	if err != nil {
		models.SendErrorJSON(w, "delivery_sessions", "cancel", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "cancel", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

func (h *DeliverySessionsHandler) CloseDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "close", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CloseDeliverySession(ctx, token, id)
	if err != nil {
		models.SendErrorJSON(w, "delivery_sessions", "close", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "close", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

func (h *DeliverySessionsHandler) GetDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "get", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.GetDeliverySession(ctx, token, id)
	if err != nil {
		models.SendErrorJSON(w, "delivery_sessions", "get", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "get", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}
