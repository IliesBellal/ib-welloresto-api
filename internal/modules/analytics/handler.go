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

// GetAccessibleMerchants GET /analytics/merchants — names the establishments
// the caller can select in the multi-establishment filter (PROMPT 24 Phase
// 1). Guarded by permission.POSAnalytics (routes.go), not
// permission.ReportsSalesRead like the rest of this package's routes: this
// endpoint IS the page-level pos.analytics gate's own data (which
// establishments does pos.analytics grant), not a sales figure.
func (h *Handler) GetAccessibleMerchants(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_accessible_merchants", map[string]string{"error": "missing_token"})
		return
	}

	resp, err := h.service.GetAccessibleMerchants(r.Context())
	if err != nil {
		writeError(w, "get_accessible_merchants", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_accessible_merchants", resp)
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

// GetProducts POST /analytics/products — the Produits tab (PROMPT 16),
// permission.ReportsSalesRead like the other five tabs (see routes.go).
func (h *Handler) GetProducts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_products", map[string]string{"error": "missing_token"})
		return
	}

	var req ProductsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_products", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetProducts(r.Context(), req)
	if err != nil {
		writeError(w, "get_products", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_products", resp)
}

// GetOptions POST /analytics/options — the Options tab (PROMPT 17),
// permission.ReportsSalesRead like the other tabs (see routes.go).
func (h *Handler) GetOptions(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_options", map[string]string{"error": "missing_token"})
		return
	}

	var req OptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_options", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetOptions(r.Context(), req)
	if err != nil {
		writeError(w, "get_options", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_options", resp)
}

// GetClients POST /analytics/clients — the Clients tab's aggregate view
// (permission.ReportsSalesRead like the other six tabs). Never the
// nominative Top Clients ranking — that is GetClientsTop below, behind
// permission.CustomersManage (see routes.go).
func (h *Handler) GetClients(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_clients", map[string]string{"error": "missing_token"})
		return
	}

	var req ClientsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_clients", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetClients(r.Context(), req)
	if err != nil {
		writeError(w, "get_clients", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_clients", resp)
}

// GetClientsTop POST /analytics/clients/top — the Clients tab's nominative
// Top Clients ranking (permission.CustomersManage, a different, more
// sensitive key than every other analytics route — see routes.go). A 403
// here must hide the block on the frontend, never break the rest of the tab
// (PROMPT 18 §2, same rule as GetCancellationsByStaff).
func (h *Handler) GetClientsTop(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_clients_top", map[string]string{"error": "missing_token"})
		return
	}

	var req ClientsTopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_clients_top", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetClientsTop(r.Context(), req)
	if err != nil {
		writeError(w, "get_clients_top", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_clients_top", resp)
}

// GetUpsell POST /analytics/upsell — the Vente additionnelle tab's aggregate
// view (permission.ReportsSalesRead like the other seven tabs).
func (h *Handler) GetUpsell(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_upsell", map[string]string{"error": "missing_token"})
		return
	}

	var req UpsellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_upsell", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetUpsell(r.Context(), req)
	if err != nil {
		writeError(w, "get_upsell", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_upsell", resp)
}

// GetUpsellByStaff POST /analytics/upsell/by-staff — the Vente additionnelle
// tab's nominative per-server ranking (permission.ReportsStaffPerformanceRead
// — see routes.go). A 403 here must hide the block on the frontend, never
// break the rest of the tab (same rule as GetCancellationsByStaff/
// GetClientsTop).
func (h *Handler) GetUpsellByStaff(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_upsell_by_staff", map[string]string{"error": "missing_token"})
		return
	}

	var req UpsellByStaffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_upsell_by_staff", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetUpsellByStaff(r.Context(), req)
	if err != nil {
		writeError(w, "get_upsell_by_staff", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_upsell_by_staff", resp)
}

// GetDiscounts POST /analytics/discounts — the Remises tab (PROMPT 22),
// permission.ReportsSalesRead like the other eight tabs. No nominative
// sibling route: this tab carries no per-staff breakdown (see service.go's
// doc comment).
func (h *Handler) GetDiscounts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_discounts", map[string]string{"error": "missing_token"})
		return
	}

	var req DiscountsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_discounts", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.service.GetDiscounts(r.Context(), req)
	if err != nil {
		writeError(w, "get_discounts", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "analytics", "get_discounts", resp)
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
	case errors.Is(err, ErrNominativeAccessDenied):
		models.SendJSON(w, http.StatusForbidden, "analytics", fnName, map[string]string{"error": "nominative_access_denied"})
	case errors.Is(err, ErrInvalidRequest):
		models.SendJSON(w, http.StatusBadRequest, "analytics", fnName, map[string]string{"error": "invalid_period"})
	default:
		models.SendJSON(w, http.StatusInternalServerError, "analytics", fnName, map[string]string{"error": err.Error()})
	}
}
