package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"welloresto-api/internal/models"
)

// CreateUser handles POST /users/create.
func (h *UsersHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "users", "create", map[string]string{"error": "invalid_request_body"})
		return
	}

	userID, err := h.svc.CreateUser(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrInvalidInput):
			models.SendJSON(w, http.StatusBadRequest, "users", "create", map[string]string{"error": "invalid_input"})
		case errors.Is(err, models.ErrInvalidInputPasswordTooShort):
			models.SendJSON(w, http.StatusBadRequest, "users", "create", map[string]string{"error": err.Error()})
		default:
			models.SendJSON(w, http.StatusInternalServerError, "users", "create", map[string]string{"error": "internal_server_error"})
		}
		return
	}

	models.SendJSON(w, http.StatusCreated, "users", "create", CreateUserResponse{UserID: userID})
}
