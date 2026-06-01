package employees

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

func (h *Handler) ListEmployees(w http.ResponseWriter, r *http.Request) {
	activeRaw := strings.TrimSpace(r.URL.Query().Get("active"))
	positionID := strings.TrimSpace(r.URL.Query().Get("position_id"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	unlinked := parseOptionalEmployeeBool(r.URL.Query().Get("unlinked"))
	if positionID == "" {
		positionID = strings.TrimSpace(r.URL.Query().Get("position"))
	}
	var active *bool
	if activeRaw != "" {
		parsed := activeRaw == "1" || strings.EqualFold(activeRaw, "true")
		active = &parsed
	}
	pagination, err := sharedpkg.ParsePlanningPagination(r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_employees", err)
		return
	}
	items, metadata, err := h.svc.ListEmployees(r.Context(), EmployeeListFilters{
		Search:       strings.TrimSpace(r.URL.Query().Get("search")),
		Active:       active,
		PositionID:   positionID,
		ContractType: strings.TrimSpace(r.URL.Query().Get("contract")),
		UserID:       userID,
		Unlinked:     unlinked,
		Page:         pagination.Page,
		PageSize:     pagination.PageSize,
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_employees", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_employees", map[string]interface{}{"status": "success", "employees": items, "pagination": metadata})
}

func (h *Handler) GetEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	item, err := h.svc.GetEmployee(r.Context(), employeeID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_employee", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_employee", map[string]interface{}{"status": "success", "employee": item})
}

func (h *Handler) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	var req EmployeeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_employee", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.CreateEmployee(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_employee", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_employee", map[string]interface{}{"status": "success", "employee": item})
}

func (h *Handler) UpdateEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req EmployeeUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "update_employee", models.ErrInvalidRequestBody)
		return
	}
	if err := RequireAtLeastOneEmployeeField(req); err != nil {
		models.SendErrorJSON(w, "planning", "update_employee", models.ErrValidationError)
		return
	}
	item, err := h.svc.UpdateEmployee(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "update_employee", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "update_employee", map[string]interface{}{"status": "success", "employee": item})
}

func (h *Handler) DeleteEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeleteEmployee(r.Context(), employeeID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_employee", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_employee", map[string]interface{}{"status": "success"})
}

func (h *Handler) LinkEmployeeUser(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req EmployeeUserLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "link_employee_user", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.LinkEmployeeUser(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "link_employee_user", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "link_employee_user", map[string]interface{}{"status": "success", "employee": item})
}

func (h *Handler) UnlinkEmployeeUser(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	item, err := h.svc.UnlinkEmployeeUser(r.Context(), employeeID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "unlink_employee_user", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "unlink_employee_user", map[string]interface{}{"status": "success", "employee": item})
}

func parseOptionalEmployeeBool(raw string) *bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed := raw == "1" || strings.EqualFold(raw, "true")
	return &parsed
}
