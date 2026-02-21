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
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	sessions, err := h.deliverySessionsService.GetPendingDeliverySessions(ctx, token)
	if err != nil {
		models.SendJSON(w, "delivery_sessions", "GetPendingDeliverySessions_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "delivery_sessions", "GetPendingDeliverySessions", map[string]interface{}{
		"status":            "success",
		"delivery_sessions": sessions,
	})
}

func (h *DeliverySessionsHandler) StartDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	var req models.DeliverySessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.deliverySessionsService.StartDeliverySession(ctx, token, &req)

	if err != nil {
		switch {
		case errors.Is(err, models.ErrInvalidToken):
			models.SendJSON(w, "delivery_sessions", "StartDeliverySession_error", map[string]string{"error": "invalid_token"})
			return

		case errors.Is(err, models.ErrDeliverySessionAlreadyActive):
			models.SendJSON(w, "delivery_sessions", "StartDeliverySession", map[string]interface{}{
				"status": "delivery_session_already_active",
			})
			return

		default:
			models.SendJSON(w, "delivery_sessions", "StartDeliverySession_error", map[string]string{"error": "internal_error"})
			return
		}
	}

	models.SendJSON(w, "delivery_sessions", "StartDeliverySession", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

func (h *DeliverySessionsHandler) CancelDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CancelDeliverySession(ctx, token, id)
	if err != nil {
		models.SendJSON(w, "delivery_sessions", "CancelDeliverySession_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "delivery_sessions", "CancelDeliverySession", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

func (h *DeliverySessionsHandler) CloseDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.CloseDeliverySession(ctx, token, id)
	if err != nil {
		models.SendJSON(w, "delivery_sessions", "CloseDeliverySession_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "delivery_sessions", "CloseDeliverySession", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

func (h *DeliverySessionsHandler) GetDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "delivery_session_id")

	resp, err := h.deliverySessionsService.GetDeliverySession(ctx, token, id)
	if err != nil {
		models.SendJSON(w, "delivery_sessions", "GetDeliverySession_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "delivery_sessions", "GetDeliverySession", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}
