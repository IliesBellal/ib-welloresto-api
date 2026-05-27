package swaps

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

func (h *Handler) ListPlanningShiftSwapRequests(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPlanningShiftSwapRequests(r.Context(), PlanningShiftSwapRequestListFilters{
		RequesterEmployeeID: strings.TrimSpace(r.URL.Query().Get("requester_employee_id")),
		TargetEmployeeID:    strings.TrimSpace(r.URL.Query().Get("target_employee_id")),
		Status:              strings.TrimSpace(r.URL.Query().Get("status")),
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_shift_swap_requests", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_shift_swap_requests", map[string]interface{}{"status": "success", "shift_swap_requests": items})
}

func (h *Handler) GetPlanningShiftSwapRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	item, err := h.svc.GetPlanningShiftSwapRequest(r.Context(), requestID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_planning_shift_swap_request", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_planning_shift_swap_request", map[string]interface{}{"status": "success", "shift_swap_request": item})
}

func (h *Handler) CreatePlanningShiftSwapRequest(w http.ResponseWriter, r *http.Request) {
	var req PlanningShiftSwapRequestCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_shift_swap_request", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.CreatePlanningShiftSwapRequest(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_planning_shift_swap_request", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_planning_shift_swap_request", map[string]interface{}{"status": "success", "shift_swap_request": item})
}

func (h *Handler) UpdatePlanningShiftSwapRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningShiftSwapRequestUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_shift_swap_request", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.UpdatePlanningShiftSwapRequest(r.Context(), requestID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_planning_shift_swap_request", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_planning_shift_swap_request", map[string]interface{}{"status": "success", "shift_swap_request": item})
}

func (h *Handler) DeletePlanningShiftSwapRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeletePlanningShiftSwapRequest(r.Context(), requestID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_planning_shift_swap_request", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_planning_shift_swap_request", map[string]interface{}{"status": "success"})
}
