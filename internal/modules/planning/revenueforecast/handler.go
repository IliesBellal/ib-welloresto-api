package revenueforecast

import (
	"encoding/json"
	"net/http"

	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) UpsertRevenueForecasts(w http.ResponseWriter, r *http.Request) {
	var req UpsertRevenueForecastsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "upsert_planning_revenue_forecast", models.ErrInvalidRequestBody)
		return
	}

	if err := h.svc.Upsert(r.Context(), req); err != nil {
		models.SendErrorJSON(w, "planning", "upsert_planning_revenue_forecast", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "planning", "upsert_planning_revenue_forecast", map[string]interface{}{
		"status": "success",
	})
}
