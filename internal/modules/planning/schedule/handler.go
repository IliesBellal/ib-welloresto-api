package schedule

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

func (h *Handler) ListPlanningWeeks(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPlanningWeeks(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_weeks", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_weeks", map[string]interface{}{"status": "success", "weeks": items})
}

func (h *Handler) GetPlanningWeek(w http.ResponseWriter, r *http.Request) {
	weekID := strings.TrimSpace(chi.URLParam(r, "id"))
	week, err := h.svc.GetPlanningWeek(r.Context(), weekID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_planning_week", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_planning_week", map[string]interface{}{"status": "success", "week": week})
}

func (h *Handler) CreatePlanningWeek(w http.ResponseWriter, r *http.Request) {
	var req PlanningWeekCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_week", models.ErrInvalidRequestBody)
		return
	}
	week, err := h.svc.CreatePlanningWeek(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_week", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_planning_week", map[string]interface{}{"status": "success", "week": week})
}

func (h *Handler) UpdatePlanningWeek(w http.ResponseWriter, r *http.Request) {
	weekID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningWeekUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_week", models.ErrInvalidRequestBody)
		return
	}
	week, err := h.svc.UpdatePlanningWeek(r.Context(), weekID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_week", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_planning_week", map[string]interface{}{"status": "success", "week": week})
}

func (h *Handler) DeletePlanningWeek(w http.ResponseWriter, r *http.Request) {
	weekID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeletePlanningWeek(r.Context(), weekID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_planning_week", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_planning_week", map[string]interface{}{"status": "success"})
}

func (h *Handler) ListPlanningShifts(w http.ResponseWriter, r *http.Request) {
	weekID := strings.TrimSpace(chi.URLParam(r, "id"))
	items, err := h.svc.ListPlanningShifts(r.Context(), weekID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_shifts", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_shifts", map[string]interface{}{"status": "success", "shifts": items})
}

func (h *Handler) ListPlanningShiftsByDateRange(w http.ResponseWriter, r *http.Request) {
	startDate := strings.TrimSpace(r.URL.Query().Get("start_date"))
	endDate := strings.TrimSpace(r.URL.Query().Get("end_date"))
	items, err := h.svc.ListPlanningShiftsByDateRange(r.Context(), startDate, endDate)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_shifts_by_date_range", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_shifts_by_date_range", map[string]interface{}{"status": "success", "shifts": items})
}

func (h *Handler) GetPlanningShift(w http.ResponseWriter, r *http.Request) {
	shiftID := strings.TrimSpace(chi.URLParam(r, "id"))
	shift, err := h.svc.GetPlanningShift(r.Context(), shiftID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_planning_shift", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_planning_shift", map[string]interface{}{"status": "success", "shift": shift})
}

func (h *Handler) CreatePlanningShift(w http.ResponseWriter, r *http.Request) {
	weekID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningShiftCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_shift", models.ErrInvalidRequestBody)
		return
	}
	shift, err := h.svc.CreatePlanningShift(r.Context(), weekID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_shift", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_planning_shift", map[string]interface{}{"status": "success", "shift": shift})
}

func (h *Handler) UpdatePlanningShift(w http.ResponseWriter, r *http.Request) {
	shiftID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningShiftUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_shift", models.ErrInvalidRequestBody)
		return
	}
	shift, err := h.svc.UpdatePlanningShift(r.Context(), shiftID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_shift", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_planning_shift", map[string]interface{}{"status": "success", "shift": shift})
}

func (h *Handler) DeletePlanningShift(w http.ResponseWriter, r *http.Request) {
	shiftID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeletePlanningShift(r.Context(), shiftID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_planning_shift", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_planning_shift", map[string]interface{}{"status": "success"})
}
