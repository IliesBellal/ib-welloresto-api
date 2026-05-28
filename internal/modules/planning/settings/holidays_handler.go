package settings

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"welloresto-api/internal/models"
)

func (h *Handler) ListPlanningHolidays(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPlanningHolidays(r.Context(), PlanningHolidayListFilters{
		StartDate: strings.TrimSpace(r.URL.Query().Get("start_date")),
		EndDate:   strings.TrimSpace(r.URL.Query().Get("end_date")),
	})
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_holidays", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_holidays", map[string]interface{}{"status": "success", "holidays": items})
}

func (h *Handler) PatchPlanningHolidayOverride(w http.ResponseWriter, r *http.Request) {
	holidayDate := strings.TrimSpace(chi.URLParam(r, "date"))
	var req PlanningHolidayOverridePatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "patch_planning_holiday_override", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.PatchPlanningHolidayOverride(r.Context(), holidayDate, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "patch_planning_holiday_override", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "patch_planning_holiday_override", map[string]interface{}{"status": "success", "holiday": item})
}

func (h *Handler) DeletePlanningHolidayOverride(w http.ResponseWriter, r *http.Request) {
	holidayDate := strings.TrimSpace(chi.URLParam(r, "date"))
	if err := h.svc.DeletePlanningHolidayOverride(r.Context(), holidayDate); err != nil {
		models.SendErrorJSON(w, "planning", "delete_planning_holiday_override", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_planning_holiday_override", map[string]interface{}{"status": "success"})
}
