package companies

import (
	"encoding/json"
	"net/http"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Resolve handles POST /v1/public/companies/resolve — public, IP-rate-limited
// (see Service.Resolve).
func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	var req ResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "companies", "resolve", models.ErrInvalidRequestBody)
		return
	}
	resp, err := h.svc.Resolve(r.Context(), helpers.ClientIP(r), req)
	if err != nil {
		models.SendErrorJSON(w, "companies", "resolve", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "companies", "resolve", resp)
}
