package employees

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"welloresto-api/internal/models"
)

func (h *Handler) ListEmployeePositions(w http.ResponseWriter, r *http.Request) {
	activeRaw := strings.TrimSpace(r.URL.Query().Get("active"))
	var active *bool
	if activeRaw != "" {
		parsed := activeRaw == "1" || strings.EqualFold(activeRaw, "true")
		active = &parsed
	}
	items, err := h.svc.ListEmployeePositions(r.Context(), EmployeePositionListFilters{
		Search: strings.TrimSpace(r.URL.Query().Get("search")),
		Active: active,
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_employee_positions", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_employee_positions", map[string]interface{}{"status": "success", "positions": items})
}

func (h *Handler) GetEmployeePosition(w http.ResponseWriter, r *http.Request) {
	positionID := strings.TrimSpace(chi.URLParam(r, "id"))
	item, err := h.svc.GetEmployeePosition(r.Context(), positionID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_employee_position", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_employee_position", map[string]interface{}{"status": "success", "position": item})
}

func (h *Handler) CreateEmployeePosition(w http.ResponseWriter, r *http.Request) {
	var req EmployeePositionCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_employee_position", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.CreateEmployeePosition(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_employee_position", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_employee_position", map[string]interface{}{"status": "success", "position": item})
}

func (h *Handler) UpdateEmployeePosition(w http.ResponseWriter, r *http.Request) {
	positionID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req EmployeePositionUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_employee_position", models.ErrInvalidRequestBody)
		return
	}
	if req.Label == nil && req.SortOrder == nil && req.Active == nil {
		models.SendErrorJSON(w, "planning", "update_employee_position", models.ErrValidationError)
		return
	}
	item, err := h.svc.UpdateEmployeePosition(r.Context(), positionID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_employee_position", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_employee_position", map[string]interface{}{"status": "success", "position": item})
}

func (h *Handler) DeleteEmployeePosition(w http.ResponseWriter, r *http.Request) {
	positionID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeleteEmployeePosition(r.Context(), positionID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_employee_position", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_employee_position", map[string]interface{}{"status": "success"})
}
