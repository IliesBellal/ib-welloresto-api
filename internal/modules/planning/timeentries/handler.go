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

func (h *Handler) ListPlanningTimeEntries(w http.ResponseWriter, r *http.Request) {
	pagination, err := sharedpkg.ParsePlanningPagination(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_time_entries", err)
		return
	}
	items, metadata, err := h.svc.ListPlanningTimeEntries(r.Context(), PlanningTimeEntryListFilters{
		From:       strings.TrimSpace(r.URL.Query().Get("from")),
		To:         strings.TrimSpace(r.URL.Query().Get("to")),
		EmployeeID: strings.TrimSpace(r.URL.Query().Get("employee_id")),
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_time_entries", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_time_entries", map[string]interface{}{"status": "success", "time_entries": items, "pagination": metadata})
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

func (h *Handler) CreateEmployeeTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningTimeEntryManualCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_employee_time_entry", models.ErrInvalidRequestBody)
		return
	}
	entry, err := h.svc.CreateEmployeeTimeEntry(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_employee_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_employee_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}

func (h *Handler) UpdateEmployeeTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	entryID := strings.TrimSpace(chi.URLParam(r, "entry_id"))
	var req PlanningTimeEntryCorrectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_employee_time_entry", models.ErrInvalidRequestBody)
		return
	}
	entry, err := h.svc.UpdateEmployeeTimeEntry(r.Context(), employeeID, entryID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_employee_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_employee_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}

func (h *Handler) DeleteEmployeeTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	entryID := strings.TrimSpace(chi.URLParam(r, "entry_id"))
	var req PlanningTimeEntryDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "delete_employee_time_entry", models.ErrInvalidRequestBody)
		return
	}
	if err := h.svc.DeleteEmployeeTimeEntry(r.Context(), employeeID, entryID, req); err != nil {
		models.SendErrorJSON(w, "planning", "delete_employee_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_employee_time_entry", map[string]interface{}{"status": "success"})
}

func (h *Handler) ListCurrentUserTimeEntries(w http.ResponseWriter, r *http.Request) {
	employeeID, err := h.svc.ResolveCurrentEmployeeID(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_time_entries", err)
		return
	}
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if date == "" {
		models.SendErrorJSON(w, "planning", "list_current_user_time_entries", models.ErrPlanningInvalidDate)
		return
	}
	if _, parseErr := sharedpkg.ParsePlanningDate(date); parseErr != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_time_entries", parseErr)
		return
	}
	pagination, err := sharedpkg.ParsePlanningPagination(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_time_entries", err)
		return
	}
	items, metadata, err := h.svc.ListPlanningTimeEntries(r.Context(), PlanningTimeEntryListFilters{
		From:       date,
		To:         date,
		EmployeeID: employeeID,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_time_entries", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_current_user_time_entries", map[string]interface{}{"status": "success", "time_entries": items, "pagination": metadata})
}

func (h *Handler) GetCurrentUserTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID, err := h.svc.ResolveCurrentEmployeeID(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_current_user_time_entry", err)
		return
	}
	entry, err := h.svc.GetCurrentEmployeeTimeEntry(r.Context(), employeeID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_current_user_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_current_user_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}

func (h *Handler) StartCurrentUserTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID, err := h.svc.ResolveCurrentEmployeeID(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "start_current_user_time_entry", err)
		return
	}
	var req PlanningTimeEntryStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "start_current_user_time_entry", models.ErrInvalidRequestBody)
		return
	}
	entry, err := h.svc.StartEmployeeTimeEntry(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "start_current_user_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "start_current_user_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}

func (h *Handler) StopCurrentUserTimeEntry(w http.ResponseWriter, r *http.Request) {
	employeeID, err := h.svc.ResolveCurrentEmployeeID(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "stop_current_user_time_entry", err)
		return
	}
	var req PlanningTimeEntryStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "stop_current_user_time_entry", models.ErrInvalidRequestBody)
		return
	}
	entry, err := h.svc.StopEmployeeTimeEntry(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "stop_current_user_time_entry", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "stop_current_user_time_entry", map[string]interface{}{"status": "success", "time_entry": entry})
}

func (h *Handler) ListCurrentUserTeamWeekShifts(w http.ResponseWriter, r *http.Request) {
	weekStart := strings.TrimSpace(r.URL.Query().Get("week_start"))
	weekID := strings.TrimSpace(r.URL.Query().Get("week_id"))

	currentEmployeeID, resolvedWeekID, items, comments, err := h.svc.ListCurrentUserTeamWeekShifts(r.Context(), weekStart, weekID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_team_week_shifts", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "planning", "list_current_user_team_week_shifts", map[string]interface{}{
		"status":              "success",
		"current_employee_id": currentEmployeeID,
		"week_id":             resolvedWeekID,
		"shifts":              items,
		"day_comments":        comments,
	})
}
