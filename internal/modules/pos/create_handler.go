package pos

import (
	"encoding/json"
	"errors"
	"net/http"
	"welloresto-api/internal/models"
)

// CreateMerchant handles POST /pos/create.
func (h *POSHandler) CreateMerchant(w http.ResponseWriter, r *http.Request) {
	var req CreateMerchantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "create", map[string]string{"error": "invalid_request_body"})
		return
	}

	resp, err := h.service.CreateMerchant(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "user", "create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "pos", "create", resp)
}

// LinkUser handles POST /pos/link-user.
func (h *POSHandler) LinkUser(w http.ResponseWriter, r *http.Request) {
	var req LinkUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "pos", "link_user", err)
		return
	}

	resp, err := h.service.LinkUser(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrInvalidInput):
			models.SendJSON(w, http.StatusBadRequest, "pos", "link_user", map[string]string{"error": "invalid_input"})
		default:
			models.SendJSON(w, http.StatusInternalServerError, "pos", "link_user", map[string]string{"error": "internal_server_error"})
		}
		return
	}

	models.SendJSON(w, http.StatusCreated, "pos", "link_user", resp)
}
