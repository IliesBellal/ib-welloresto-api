package leave

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

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

func (h *Handler) CreateCurrentUserLeaveRequest(w http.ResponseWriter, r *http.Request) {
	var req PlanningLeaveRequestSelfCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_current_user_leave_request", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.CreateCurrentUserLeaveRequest(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_current_user_leave_request", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_current_user_leave_request", map[string]interface{}{"status": "success", "leave_request": item})
}

func (h *Handler) ListCurrentUserLeaveRequests(w http.ResponseWriter, r *http.Request) {
	pagination, err := sharedpkg.ParsePlanningPagination(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_leave_requests", err)
		return
	}
	fromDate, toDate, err := parsePlanningLeaveDateOverlapRange(r)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_leave_requests", err)
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	items, metadata, err := h.svc.ListCurrentUserLeaveRequestsPaginated(r.Context(), PlanningLeaveRequestListFilters{
		Status:   status,
		Page:     pagination.Page,
		PageSize: pagination.PageSize,
		FromDate: fromDate,
		ToDate:   toDate,
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_leave_requests", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_current_user_leave_requests", map[string]interface{}{"status": "success", "leave_requests": items, "pagination": metadata})
}

func (h *Handler) ListPlanningLeaveRequests(w http.ResponseWriter, r *http.Request) {
	pagination, err := sharedpkg.ParsePlanningPagination(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_leave_requests", err)
		return
	}
	fromDate, toDate, err := parsePlanningLeaveDateOverlapRange(r)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_leave_requests", err)
		return
	}
	items, metadata, err := h.svc.ListPlanningLeaveRequests(r.Context(), PlanningLeaveRequestListFilters{
		EmployeeID: strings.TrimSpace(r.URL.Query().Get("employee_id")),
		Status:     strings.TrimSpace(r.URL.Query().Get("status")),
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		FromDate:   fromDate,
		ToDate:     toDate,
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_leave_requests", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_leave_requests", map[string]interface{}{"status": "success", "leave_requests": items, "pagination": metadata})
}

func parsePlanningLeaveDateOverlapRange(r *http.Request) (*time.Time, *time.Time, error) {
	rawFrom := strings.TrimSpace(r.URL.Query().Get("from"))
	rawTo := strings.TrimSpace(r.URL.Query().Get("to"))

	var fromDate *time.Time
	var toDate *time.Time

	if rawFrom != "" {
		parsed, err := sharedpkg.ParsePlanningDate(rawFrom)
		if err != nil {
			return nil, nil, models.ErrPlanningLeaveInvalidRange
		}
		fromDate = &parsed
	}
	if rawTo != "" {
		parsed, err := sharedpkg.ParsePlanningDate(rawTo)
		if err != nil {
			return nil, nil, models.ErrPlanningLeaveInvalidRange
		}
		toDate = &parsed
	}

	if fromDate != nil && toDate != nil && toDate.Before(*fromDate) {
		return nil, nil, models.ErrPlanningLeaveInvalidRange
	}

	return fromDate, toDate, nil
}

func (h *Handler) GetPlanningLeaveRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	item, err := h.svc.GetPlanningLeaveRequest(r.Context(), requestID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_planning_leave_request", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_planning_leave_request", map[string]interface{}{"status": "success", "leave_request": item})
}

func (h *Handler) CreatePlanningLeaveRequest(w http.ResponseWriter, r *http.Request) {
	var req PlanningLeaveRequestCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_leave_request", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.CreatePlanningLeaveRequest(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_leave_request", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_planning_leave_request", map[string]interface{}{"status": "success", "leave_request": item})
}

func (h *Handler) UpdatePlanningLeaveRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningLeaveRequestUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_leave_request", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.UpdatePlanningLeaveRequest(r.Context(), requestID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_leave_request", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_planning_leave_request", map[string]interface{}{"status": "success", "leave_request": item})
}

func (h *Handler) DeletePlanningLeaveRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeletePlanningLeaveRequest(r.Context(), requestID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_planning_leave_request", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_planning_leave_request", map[string]interface{}{"status": "success"})
}

func (h *Handler) ListPlanningLeaveRequestConflictingShifts(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	items, err := h.svc.ListPlanningLeaveRequestConflictingShifts(r.Context(), requestID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_leave_request_conflicting_shifts", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_leave_request_conflicting_shifts", map[string]interface{}{"status": "success", "conflicting_shifts": items})
}
