package weektemplates

import (
	"encoding/json"
	"net/http"
	"strings"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListWeekTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListWeekTemplates(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "ListWeekTemplates", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_week_templates", map[string]interface{}{"status": "success", "week_templates": items})
}

func (h *Handler) GetWeekTemplate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	item, err := h.service.GetWeekTemplate(r.Context(), id)
	if err != nil {
		models.SendErrorJSON(w, "planning", "GetWeekTemplate", err)
		return
	}
	weekTemplate, shifts := splitWeekTemplateResponse(item)
	models.SendJSON(w, http.StatusOK, "planning", "get_week_template", map[string]interface{}{"status": "success", "week_template": weekTemplate, "week_template_shifts": shifts})
}

func (h *Handler) CreateWeekTemplate(w http.ResponseWriter, r *http.Request) {
	var req WeekTemplateCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_week_template", models.ErrInvalidRequestBody)
		return
	}

	item, err := h.service.CreateWeekTemplate(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_week_template", err)
		return
	}
	weekTemplate, shifts := splitWeekTemplateResponse(item)
	models.SendJSON(w, http.StatusCreated, "planning", "create_week_template", map[string]interface{}{"status": "success", "week_template": weekTemplate, "week_template_shifts": shifts})
}

func (h *Handler) CreateWeekTemplateFromWeek(w http.ResponseWriter, r *http.Request) {
	var req WeekTemplateFromWeekRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_week_template_from_week", models.ErrInvalidRequestBody)
		return
	}

	item, err := h.service.CreateWeekTemplateFromWeek(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_week_template_from_week", err)
		return
	}
	weekTemplate, shifts := splitWeekTemplateResponse(item)
	models.SendJSON(w, http.StatusCreated, "planning", "create_week_template_from_week", map[string]interface{}{"status": "success", "week_template": weekTemplate, "week_template_shifts": shifts})
}

func (h *Handler) PreviewWeekTemplateInstantiation(w http.ResponseWriter, r *http.Request) {
	templateID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req WeekTemplatePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "preview_week_template_instantiation", models.ErrInvalidRequestBody)
		return
	}

	preview, err := h.service.PreviewWeekTemplateInstantiation(r.Context(), templateID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "preview_week_template_instantiation", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "planning", "preview_week_template_instantiation", map[string]interface{}{"status": "success", "preview": preview})
}

func (h *Handler) InstantiateWeekTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req WeekTemplateInstantiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "instantiate_week_template", models.ErrInvalidRequestBody)
		return
	}

	result, err := h.service.InstantiateWeekTemplate(r.Context(), templateID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "instantiate_week_template", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "planning", "instantiate_week_template", map[string]interface{}{"status": "success", "result": result})
}

func (h *Handler) UpdateWeekTemplate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var req WeekTemplateUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_week_template", models.ErrInvalidRequestBody)
		return
	}

	item, err := h.service.UpdateWeekTemplate(r.Context(), id, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_week_template", err)
		return
	}
	weekTemplate, shifts := splitWeekTemplateResponse(item)
	models.SendJSON(w, http.StatusOK, "planning", "update_week_template", map[string]interface{}{"status": "success", "week_template": weekTemplate, "week_template_shifts": shifts})
}

func (h *Handler) DeleteWeekTemplate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.service.DeleteWeekTemplate(r.Context(), id); err != nil {
		models.SendErrorJSON(w, "planning", "delete_week_template", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_week_template", map[string]interface{}{"status": "success"})
}

func splitWeekTemplateResponse(item *WeekTemplate) (WeekTemplate, []WeekTemplateShift) {
	if item == nil {
		return WeekTemplate{}, []WeekTemplateShift{}
	}
	weekTemplate := *item
	shifts := make([]WeekTemplateShift, len(item.WeekTemplateShifts))
	copy(shifts, item.WeekTemplateShifts)
	weekTemplate.WeekTemplateShifts = nil
	return weekTemplate, shifts
}
