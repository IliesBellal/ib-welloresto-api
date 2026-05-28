package timeentries

import (
	"encoding/json"
	"net/http"
	"strings"

	"welloresto-api/internal/models"
	sharedpkg "welloresto-api/internal/modules/planning/shared"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListEmployeeTimeEntries(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	pagination, err := sharedpkg.ParsePlanningPagination(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_employee_time_entries", err)
		return
	}
	items, metadata, err := h.svc.ListEmployeeTimeEntries(r.Context(), employeeID, PlanningTimeEntryListFilters{Page: pagination.Page, PageSize: pagination.PageSize})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_employee_time_entries", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_employee_time_entries", map[string]interface{}{"status": "success", "time_entries": items, "pagination": metadata})
}

func (h *Handler) GetCurrentEmployeeTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	entry, err := h.svc.GetCurrentEmployeeTimeEntry(r.Context(), employeeID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_current_employee_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_current_employee_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}

func (h *Handler) StartEmployeeTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningTimeEntryStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "start_employee_time_entry", models.ErrInvalidRequestBody)
		return
	}
	entry, err := h.svc.StartEmployeeTimeEntry(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "start_employee_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "start_employee_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}

func (h *Handler) StopEmployeeTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningTimeEntryStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "stop_employee_time_entry", models.ErrInvalidRequestBody)
		return
	}
	entry, err := h.svc.StopEmployeeTimeEntry(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "stop_employee_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "stop_employee_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}
