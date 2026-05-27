package settings

import (
	"encoding/json"
	"net/http"

	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.GetSettings(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_settings", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_settings", map[string]interface{}{"status": "success", "settings": settings})
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req PlanningSettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_settings", models.ErrInvalidRequestBody)
		return
	}
	settings, err := h.svc.UpdateSettings(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_settings", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_settings", map[string]interface{}{"status": "success", "settings": settings})
}
