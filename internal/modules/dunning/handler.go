package dunning

import (
	"net/http"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RetryNow handles POST /v1/billing/retry-now — the back-office "réessayer
// maintenant" button (B2b-1): retries collection on the merchant's existing
// mandate, never asking for a new IBAN.
func (h *Handler) RetryNow(w http.ResponseWriter, r *http.Request) {
	const fnName = "retry_now"
	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "dunning", fnName, models.ErrUnauthorized)
		return
	}

	if err := h.svc.RetryNow(r.Context(), user.MerchantID); err != nil {
		models.SendErrorJSON(w, "dunning", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "dunning", fnName, map[string]interface{}{"status": "success"})
}
