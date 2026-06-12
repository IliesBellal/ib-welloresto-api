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

// GET /delivery_sessions/me - active delivery session of the calling delivery user
func (h *DeliverySessionsHandler) GetMyDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "get_my_session", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()

	resp, err := h.deliverySessionsService.GetMyActiveDeliverySession(ctx)
	if err != nil {
		if errors.Is(err, models.ErrNoActiveDeliverySession) {
			models.SendJSON(w, http.StatusNotFound, "delivery_sessions", "get_my_session", map[string]string{
				"status":  "error",
				"message": "no_active_delivery_session",
			})
			return
		}
		models.SendErrorJSON(w, "delivery_sessions", "get_my_session", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "get_my_session", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

// PATCH /delivery_sessions/me/stops/{order_id}/select
func (h *DeliverySessionsHandler) SelectDeliveryStop(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "select_stop", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	resp, err := h.deliverySessionsService.SelectDeliveryStop(ctx, orderID)
	if err != nil {
		writeDeliveryStopError(w, "select_stop", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "select_stop", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

// PATCH /delivery_sessions/me/stops/{order_id}/arrived
func (h *DeliverySessionsHandler) MarkDeliveryStopArrived(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "stop_arrived", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	resp, err := h.deliverySessionsService.MarkDeliveryStopArrived(ctx, orderID)
	if err != nil {
		writeDeliveryStopError(w, "stop_arrived", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "stop_arrived", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

// PATCH /delivery_sessions/me/stops/{order_id}/delivered
func (h *DeliverySessionsHandler) MarkDeliveryStopDelivered(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "stop_delivered", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	resp, err := h.deliverySessionsService.MarkDeliveryStopDelivered(ctx, orderID)
	if err != nil {
		writeDeliveryStopError(w, "stop_delivered", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "stop_delivered", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

// PATCH /delivery_sessions/me/stops/{order_id}/failed
func (h *DeliverySessionsHandler) MarkDeliveryStopFailed(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "stop_failed", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	var req DeliveryStopReasonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "delivery_sessions", "stop_failed", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.deliverySessionsService.MarkDeliveryStopFailed(ctx, orderID, &req)
	if err != nil {
		writeDeliveryStopError(w, "stop_failed", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "stop_failed", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

// PATCH /delivery_sessions/me/stops/{order_id}/cancel
func (h *DeliverySessionsHandler) CancelDeliveryStop(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "stop_cancel", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	orderID := chi.URLParam(r, "order_id")

	var req DeliveryStopReasonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "delivery_sessions", "stop_cancel", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.deliverySessionsService.CancelDeliveryStop(ctx, orderID, &req)
	if err != nil {
		writeDeliveryStopError(w, "stop_cancel", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "stop_cancel", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

// PATCH /delivery_sessions/me/close
func (h *DeliverySessionsHandler) CloseMyDeliverySession(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "delivery_sessions", "close_my_session", map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()

	resp, err := h.deliverySessionsService.CloseMyDeliverySession(ctx)
	if err != nil {
		writeDeliveryStopError(w, "close_my_session", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "delivery_sessions", "close_my_session", map[string]interface{}{
		"status":           "success",
		"delivery_session": resp,
	})
}

// writeDeliveryStopError maps per-stop transition errors (§3.2/§3.3) to their HTTP status/body.
func writeDeliveryStopError(w http.ResponseWriter, fnName string, err error) {
	switch {
	case errors.Is(err, models.ErrNoActiveDeliverySession):
		models.SendJSON(w, http.StatusNotFound, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "no_active_delivery_session",
		})
	case errors.Is(err, models.ErrDeliveryStopNotFound):
		models.SendJSON(w, http.StatusNotFound, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "stop_not_found",
		})
	case errors.Is(err, models.ErrDeliveryStopTerminal):
		models.SendJSON(w, http.StatusConflict, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "stop_terminal",
		})
	case errors.Is(err, models.ErrDeliveryStopNotCurrent):
		models.SendJSON(w, http.StatusConflict, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "not_current_stop",
		})
	case errors.Is(err, models.ErrDeliveryStopNotEnRoute):
		models.SendJSON(w, http.StatusConflict, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "stop_not_en_route",
		})
	case errors.Is(err, models.ErrDeliveryStopNotDeliverable):
		models.SendJSON(w, http.StatusConflict, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "stop_not_deliverable",
		})
	case errors.Is(err, models.ErrOrderNotFullyPaid):
		models.SendJSON(w, http.StatusConflict, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "order_not_fully_paid",
		})
	case errors.Is(err, models.ErrFailReasonRequired):
		models.SendJSON(w, http.StatusBadRequest, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "reason_required",
		})
	case errors.Is(err, models.ErrSessionHasPendingStops):
		models.SendJSON(w, http.StatusConflict, "delivery_sessions", fnName, map[string]string{
			"status": "error", "message": "session_has_pending_stops",
		})
	default:
		models.SendErrorJSON(w, "delivery_sessions", fnName, err)
	}
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
