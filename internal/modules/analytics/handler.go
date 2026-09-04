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

// GetCancellations POST /analytics/cancellations — the Annulations tab's
// aggregate view (permission.ReportsSalesRead, see routes.go).
func (h *Handler) GetCancellations(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_cancellations", map[string]string{"error": "missing_token"})
		return
	}

	var req CancellationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_cancellations", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetCancellations(r.Context(), req)
	if err != nil {
		writeError(w, "get_cancellations", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_cancellations", resp)
}

// GetCancellationsByStaff POST /analytics/cancellations/by-staff — the
// Annulations tab's nominative per-server ranking
// (permission.ReportsStaffPerformanceRead, a different, more sensitive key
// than every other analytics route — see routes.go). A 403 here must hide
// the block on the frontend, never break the rest of the tab (PROMPT 10 §2).
func (h *Handler) GetCancellationsByStaff(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_cancellations_by_staff", map[string]string{"error": "missing_token"})
		return
	}

	var req CancellationsByStaffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_cancellations_by_staff", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetCancellationsByStaff(r.Context(), req)
	if err != nil {
		writeError(w, "get_cancellations_by_staff", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_cancellations_by_staff", resp)
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
