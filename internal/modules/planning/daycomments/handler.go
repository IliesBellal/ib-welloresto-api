package daycomments

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

func (h *Handler) ListPlanningDayComments(w http.ResponseWriter, r *http.Request) {
	startDate := strings.TrimSpace(r.URL.Query().Get("start_date"))
	endDate := strings.TrimSpace(r.URL.Query().Get("end_date"))

	items, err := h.svc.ListByDateRange(r.Context(), startDate, endDate)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_day_comments", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_day_comments", map[string]interface{}{"status": "success", "day_comments": items})
}

func (h *Handler) UpsertPlanningDayComment(w http.ResponseWriter, r *http.Request) {
	date := strings.TrimSpace(chi.URLParam(r, "date"))
	var req PlanningDayCommentUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "upsert_planning_day_comment", models.ErrInvalidRequestBody)
		return
	}
	comment, err := h.svc.Upsert(r.Context(), date, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "upsert_planning_day_comment", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "upsert_planning_day_comment", map[string]interface{}{"status": "success", "day_comment": comment})
}

func (h *Handler) DeletePlanningDayComment(w http.ResponseWriter, r *http.Request) {
	date := strings.TrimSpace(chi.URLParam(r, "date"))
	if err := h.svc.Delete(r.Context(), date); err != nil {
		models.SendErrorJSON(w, "planning", "delete_planning_day_comment", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_planning_day_comment", map[string]interface{}{"status": "success"})
}
