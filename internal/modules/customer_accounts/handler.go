package customer_accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

const module = "customer_accounts"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func customerIDParam(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "customer_id")), 10, 64)
	return id, err == nil && id > 0
}

// List GET /customer-accounts : comptes non soldés.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.svc.ListAccounts(r.Context())
	if err != nil {
		models.SendErrorJSON(w, module, "list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "list", map[string]interface{}{"status": "success", "accounts": accounts})
}

// Get GET /customer-accounts/{customer_id} : solde, plafond, historique.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerIDParam(r)
	if !ok {
		models.SendJSON(w, http.StatusBadRequest, module, "get", map[string]string{"error": "invalid_customer_id"})
		return
	}
	account, err := h.svc.GetAccount(r.Context(), customerID)
	if err != nil {
		models.SendErrorJSON(w, module, "get", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "get", map[string]interface{}{"status": "success", "account": account})
}

// Deposit POST /customer-accounts/{customer_id}/deposit.
func (h *Handler) Deposit(w http.ResponseWriter, r *http.Request) {
	h.operation(w, r, "deposit", h.svc.Deposit)
}

// Refund POST /customer-accounts/{customer_id}/refund (pos.refund).
func (h *Handler) Refund(w http.ResponseWriter, r *http.Request) {
	h.operation(w, r, "refund", h.svc.Refund)
}

// WriteOff POST /customer-accounts/{customer_id}/write-off (pos.refund, et
// rôle administrateur : voir Service.WriteOff).
func (h *Handler) WriteOff(w http.ResponseWriter, r *http.Request) {
	h.operation(w, r, "write_off", h.svc.WriteOff)
}

func (h *Handler) operation(w http.ResponseWriter, r *http.Request, fn string,
	apply func(ctx context.Context, customerID int64, req models.CustomerAccountOperationRequest) (*OperationResult, error)) {
	customerID, ok := customerIDParam(r)
	if !ok {
		models.SendJSON(w, http.StatusBadRequest, module, fn, map[string]string{"error": "invalid_customer_id"})
		return
	}
	var req models.CustomerAccountOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, module, fn, map[string]string{"error": "invalid_body"})
		return
	}
	result, err := apply(r.Context(), customerID, req)
	if err != nil {
		models.SendErrorJSON(w, module, fn, err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, fn, map[string]interface{}{
		"status":    "success",
		"operation": result.Operation,
		"account":   result.Account,
	})
}

// GetSettings GET /customer-accounts/settings.
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.GetSettings(r.Context())
	if err != nil {
		models.SendErrorJSON(w, module, "get_settings", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "get_settings", map[string]interface{}{"status": "success", "settings": settings})
}

// UpdateSettings PUT /customer-accounts/settings (settings.manage).
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req models.CustomerAccountSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, module, "update_settings", map[string]string{"error": "invalid_body"})
		return
	}
	settings, err := h.svc.UpdateSettings(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, module, "update_settings", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "update_settings", map[string]interface{}{"status": "success", "settings": settings})
}
