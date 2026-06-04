package swaps

import (
	"encoding/json"
	"net/http"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	sharedpkg "welloresto-api/internal/modules/planning/shared"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListCurrentUserShiftSwapRequests(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	currentEmployeeID, items, err := h.svc.ListCurrentUserShiftSwapRequests(r.Context(), status)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_current_user_shift_swap_requests", err)
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	approvalMode := settingspkg.ShiftSwapApprovalModeManagerRequired
	if user != nil {
		if mode, modeErr := h.svc.getShiftSwapApprovalMode(r.Context(), user.MerchantID); modeErr == nil {
			approvalMode = mode
		}
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_current_user_shift_swap_requests", map[string]interface{}{
		"status":              "success",
		"current_employee_id": currentEmployeeID,
		"approval_mode":       approvalMode,
		"shift_swap_requests": items,
	})
}

func (h *Handler) CreateCurrentUserShiftSwapRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequesterShiftID string  `json:"requester_shift_id"`
		TargetEmployeeID string  `json:"target_employee_id"`
		TargetShiftID    string  `json:"target_shift_id"`
		Reason           *string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendErrorJSON(w, "planning", "create_current_user_shift_swap_request", models.ErrInvalidRequestBody)
		return
	}
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_current_user_shift_swap_request", models.ErrUnauthorized)
		return
	}
	// Force requester from token — body requester_employee_id is never trusted
	requesterEmployeeID, err := sharedpkg.ResolvePlanningEmployeeID(r.Context(), h.svc.employeeRepo, user.MerchantID, "me", user.MerchantRightsID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_current_user_shift_swap_request", models.ErrPlanningEmployeeNotFound)
		return
	}
	// Delegate fully to the existing service — it verifies shift ownership, pending status, employee existence
	createReq := PlanningShiftSwapRequestCreateRequest{
		RequesterEmployeeID: requesterEmployeeID,
		RequesterShiftID:    strings.TrimSpace(body.RequesterShiftID),
		TargetEmployeeID:    strings.TrimSpace(body.TargetEmployeeID),
		TargetShiftID:       strings.TrimSpace(body.TargetShiftID),
		Reason:              body.Reason,
	}
	created, err := h.svc.CreatePlanningShiftSwapRequest(r.Context(), createReq)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_current_user_shift_swap_request", err)
		return
	}
	// Return self view: reuse list self and find the created item
	_, items, listErr := h.svc.ListCurrentUserShiftSwapRequests(r.Context(), "")
	if listErr == nil {
		for _, it := range items {
			if it.ID == created.ID {
				models.SendJSON(w, http.StatusCreated, "planning", "create_current_user_shift_swap_request", map[string]interface{}{"status": "success", "shift_swap_request": it})
				return
			}
		}
	}
	// Fallback: return the raw created object if self-view lookup failed
	models.SendJSON(w, http.StatusCreated, "planning", "create_current_user_shift_swap_request", map[string]interface{}{"status": "success", "shift_swap_request": created})
}

func (h *Handler) ListPlanningShiftSwapRequests(w http.ResponseWriter, r *http.Request) {
	pagination, err := sharedpkg.ParsePlanningPagination(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_shift_swap_requests", err)
		return
	}
	items, metadata, err := h.svc.ListPlanningShiftSwapRequests(r.Context(), PlanningShiftSwapRequestListFilters{
		RequesterEmployeeID: strings.TrimSpace(r.URL.Query().Get("requester_employee_id")),
		TargetEmployeeID:    strings.TrimSpace(r.URL.Query().Get("target_employee_id")),
		Status:              strings.TrimSpace(r.URL.Query().Get("status")),
		Page:                pagination.Page,
		PageSize:            pagination.PageSize,
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_shift_swap_requests", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_shift_swap_requests", map[string]interface{}{"status": "success", "shift_swap_requests": items, "pagination": metadata})
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

func (h *Handler) AcceptPlanningShiftSwapRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", models.ErrUnauthorized)
		return
	}

	// Load the request
	req, err := h.svc.GetPlanningShiftSwapRequest(r.Context(), requestID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", err)
		return
	}

	// Guard A: only allowed in target_employee_required mode
	mode, err := h.svc.getShiftSwapApprovalMode(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", err)
		return
	}
	if mode != settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", models.ErrPlanningShiftSwapApprovalForbidden)
		return
	}

	// Guard B: only pending
	if strings.ToLower(strings.TrimSpace(req.Status)) != "pending" {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", models.ErrPlanningShiftSwapStatusInvalid)
		return
	}

	// Ensure actor is the target (will check member id from token)
	if err := h.svc.ensureTargetEmployeeApproval(r.Context(), user.MerchantID, req.TargetEmployeeID, user.MerchantRightsID); err != nil {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", err)
		return
	}

	status := "approved"
	updateReq := PlanningShiftSwapRequestUpdateRequest{Status: &status}
	if _, err := h.svc.UpdatePlanningShiftSwapRequest(r.Context(), requestID, updateReq); err != nil {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", err)
		return
	}

	// Return self view: reuse list self and find the updated item
	_, items, err := h.svc.ListCurrentUserShiftSwapRequests(r.Context(), "")
	if err != nil {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", err)
		return
	}
	var out *PlanningShiftSwapRequestSelfView
	for _, it := range items {
		if it.ID == requestID {
			out = &it
			break
		}
	}
	if out == nil {
		models.SendErrorJSON(w, "planning", "accept_planning_shift_swap_request", models.ErrPlanningShiftSwapRequestNotFound)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "accept_planning_shift_swap_request", map[string]interface{}{"status": "success", "shift_swap_request": out})
}

func (h *Handler) RejectPlanningShiftSwapRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "reject_planning_shift_swap_request", models.ErrUnauthorized)
		return
	}

	// Load the request
	req, err := h.svc.GetPlanningShiftSwapRequest(r.Context(), requestID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "reject_planning_shift_swap_request", err)
		return
	}

	// Guard A: only allowed in target_employee_required mode
	mode, err := h.svc.getShiftSwapApprovalMode(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "reject_planning_shift_swap_request", err)
		return
	}
	if mode != settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired {
		models.SendErrorJSON(w, "planning", "reject_planning_shift_swap_request", models.ErrPlanningShiftSwapApprovalForbidden)
		return
	}

	// Guard B: only pending
	if strings.ToLower(strings.TrimSpace(req.Status)) != "pending" {
		models.SendErrorJSON(w, "planning", "reject_planning_shift_swap_request", models.ErrPlanningShiftSwapStatusInvalid)
		return
	}

	// Ensure actor is the target (will check member id from token)
	if err := h.svc.ensureTargetEmployeeApproval(r.Context(), user.MerchantID, req.TargetEmployeeID, user.MerchantRightsID); err != nil {
		models.SendErrorJSON(w, "planning", "reject_planning_shift_swap_request", err)
		return
	}

	status := "rejected"
	updateReq := PlanningShiftSwapRequestUpdateRequest{Status: &status}
	item, err := h.svc.UpdatePlanningShiftSwapRequest(r.Context(), requestID, updateReq)
	if err != nil {
		models.SendErrorJSON(w, "planning", "reject_planning_shift_swap_request", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "planning", "reject_planning_shift_swap_request", map[string]interface{}{"status": "success", "shift_swap_request": item})
}
