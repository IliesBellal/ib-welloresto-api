package shifttemplates

import (
	"encoding/json"
	"net/http"
	"strings"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListShiftTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListShiftTemplates(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_shift_templates", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_shift_templates", map[string]interface{}{"status": "success", "shift_templates": items})
}

func (h *Handler) CreateShiftTemplate(w http.ResponseWriter, r *http.Request) {
	var req ShiftTemplateCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_shift_template", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.CreateShiftTemplate(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_shift_template", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_shift_template", map[string]interface{}{"status": "success", "shift_template": item})
}

func (h *Handler) UpdateShiftTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req ShiftTemplateUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_shift_template", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.UpdateShiftTemplate(r.Context(), templateID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_shift_template", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_shift_template", map[string]interface{}{"status": "success", "shift_template": item})
}

func (h *Handler) DeleteShiftTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeleteShiftTemplate(r.Context(), templateID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_shift_template", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_shift_template", map[string]interface{}{"status": "success"})
}
