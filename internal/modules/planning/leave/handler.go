package leave

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListPlanningLeaveRequests(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPlanningLeaveRequests(r.Context(), PlanningLeaveRequestListFilters{
		EmployeeID: strings.TrimSpace(r.URL.Query().Get("employee_id")),
		Status:     strings.TrimSpace(r.URL.Query().Get("status")),
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_leave_requests", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_leave_requests", map[string]interface{}{"status": "success", "leave_requests": items})
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
