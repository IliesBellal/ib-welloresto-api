package customers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// blockErrorStatus traduit une erreur de blocage en (statut HTTP, code). ok=false :
// erreur inattendue, à laisser à SendErrorJSON (500).
func blockErrorStatus(err error) (status int, code string, ok bool) {
	switch {
	case errors.Is(err, ErrBlockIdentityRequired):
		return http.StatusBadRequest, "identity_required", true
	case errors.Is(err, ErrBlockInvalidPhone):
		return http.StatusBadRequest, "invalid_phone", true
	case errors.Is(err, ErrBlockInvalidEmail):
		return http.StatusBadRequest, "invalid_email", true
	case errors.Is(err, ErrBlockReasonInvalid):
		return http.StatusBadRequest, "invalid_reason", true
	case errors.Is(err, ErrBlockCommentRequired):
		return http.StatusBadRequest, "comment_required", true
	case errors.Is(err, ErrCustomerNotFound):
		return http.StatusNotFound, "customer_not_found", true
	}
	return 0, "", false
}

// BlockCustomer gère POST /customers/block : bloque un client (existant ou
// non) pour la commande en ligne. Voir CustomersService.BlockCustomer.
func (h *CustomersHandler) BlockCustomer(w http.ResponseWriter, r *http.Request) {
	var req BlockCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "block", map[string]string{"error": "invalid_request"})
		return
	}

	result, err := h.svc.BlockCustomer(r.Context(), req)
	if err != nil {
		if status, code, ok := blockErrorStatus(err); ok {
			models.SendJSON(w, status, "customers", "block", map[string]string{"error": code})
			return
		}
		models.SendErrorJSON(w, "customers", "block", err)
		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	models.SendJSON(w, status, "customers", "block", map[string]interface{}{
		"status":        "success",
		"customer_id":   result.CustomerID,
		"created":       result.Created,
		"blocked_count": result.BlockedCount,
	})
}

// UnblockCustomer gère PATCH /customers/{customer_id}/unblock.
func (h *CustomersHandler) UnblockCustomer(w http.ResponseWriter, r *http.Request) {
	customerID := strings.TrimSpace(chi.URLParam(r, "customer_id"))
	if customerID == "" {
		models.SendJSON(w, http.StatusBadRequest, "customers", "unblock", map[string]string{"error": "missing_customer_id"})
		return
	}

	if err := h.svc.UnblockCustomer(r.Context(), customerID); err != nil {
		if status, code, ok := blockErrorStatus(err); ok {
			models.SendJSON(w, status, "customers", "unblock", map[string]string{"error": code})
			return
		}
		models.SendErrorJSON(w, "customers", "unblock", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "unblock", map[string]string{"status": "success"})
}
