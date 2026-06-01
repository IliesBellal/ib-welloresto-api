package users

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/models"
)

// CreateUser handles POST /users/create.
func (h *UsersHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "users", "create", models.ErrInvalidRequestBody)
		return
	}

	userID, err := h.svc.CreateUser(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "users", "create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "users", "create", CreateUserResponse{UserID: userID})
}
