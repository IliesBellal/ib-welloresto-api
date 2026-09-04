package analytics

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type Handler struct {
	service *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{service: svc}
}

// GetRevenue POST /analytics/revenue — the CA tab.
func (h *Handler) GetRevenue(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_revenue", map[string]string{"error": "missing_token"})
		return
	}

	var req RevenueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_revenue", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetRevenue(r.Context(), req)
	if err != nil {
		writeError(w, "get_revenue", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_revenue", resp)
}

// GetOrders POST /analytics/orders — the Commandes tab.
func (h *Handler) GetOrders(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_orders", map[string]string{"error": "missing_token"})
		return
	}

	var req OrdersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_orders", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetOrders(r.Context(), req)
	if err != nil {
		writeError(w, "get_orders", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_orders", resp)
}

// GetPayments POST /analytics/payments — the Règlements tab.
func (h *Handler) GetPayments(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_payments", map[string]string{"error": "missing_token"})
		return
	}

	var req PaymentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_payments", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetPayments(r.Context(), req)
	if err != nil {
		writeError(w, "get_payments", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_payments", resp)
}

// GetVAT POST /analytics/vat — the TVA tab.
func (h *Handler) GetVAT(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_vat", map[string]string{"error": "missing_token"})
		return
	}

	var req VATRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_vat", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetVAT(r.Context(), req)
	if err != nil {
		writeError(w, "get_vat", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_vat", resp)
}

// writeError maps this package's sentinel errors to the HTTP status the
// contract promises the frontend — in particular, ErrMerchantNotAccessible
// must always reach the client as 403, never as a silently narrowed
// response (see ValidateRequestedMerchants's doc comment).
func writeError(w http.ResponseWriter, fnName string, err error) {
	switch {
	case errors.Is(err, models.ErrUnauthorized):
		models.SendJSON(w, http.StatusUnauthorized, "analytics", fnName, map[string]string{"error": "unauthorized"})
	case errors.Is(err, ErrMerchantNotAccessible):
		models.SendJSON(w, http.StatusForbidden, "analytics", fnName, map[string]string{"error": "merchant_not_accessible"})
	case errors.Is(err, ErrInvalidRequest):
		models.SendJSON(w, http.StatusBadRequest, "analytics", fnName, map[string]string{"error": "invalid_period"})
	default:
		models.SendJSON(w, http.StatusInternalServerError, "analytics", fnName, map[string]string{"error": err.Error()})
	}
}
