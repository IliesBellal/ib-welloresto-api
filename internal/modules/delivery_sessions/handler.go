package delivery_sessions

import (
	"encoding/json"
	"errors"
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
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "pending", map[string]string{"error": "invalid_token"})
		return
	}

	ctx := r.Context()

	sessions, err := h.deliverySessionsService.GetPendingDeliverySessions(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "delivery_sessions", "pending", map[string]string{"error": err.Error()})
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
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "start", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.DeliverySessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "delivery_sessions", "start", map[string]string{"error": "invalid_body"})
		return
	}

	resp, err := h.deliverySessionsService.StartDeliverySession(ctx, token, &req)

	if err != nil {
		switch {
		case errors.Is(err, models.ErrInvalidToken):
			models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "start", map[string]string{"error": "invalid_token"})
			return

		case errors.Is(err, models.ErrDeliverySessionAlreadyActive):
			models.SendJSON(w, http.StatusOK, "delivery_sessions", "start", map[string]interface{}{
				"status": "delivery_session_already_active",
			})
			return

		default:
			models.SendJSON(w, http.StatusInternalServerError, "delivery_sessions", "start", map[string]string{"error": "internal_error"})
			return
		}
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "start", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

func (h *DeliverySessionsHandler) CancelDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "cancel", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CancelDeliverySession(ctx, token, id)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "delivery_sessions", "cancel", map[string]string{"error": err.Error()})
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
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "close", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CloseDeliverySession(ctx, token, id)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "delivery_sessions", "close", map[string]string{"error": err.Error()})
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
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "get", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.GetDeliverySession(ctx, token, id)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "delivery_sessions", "get", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "get", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}
