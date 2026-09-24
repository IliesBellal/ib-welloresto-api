package demorequest

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

// Slots handles GET /v1/public/demo-request/slots — public, appelé par
// DemoForm.astro pour peupler le tableau de créneaux avant toute soumission.
// Renvoie TOUTE la grille (voir Service.SlotGrid), pas seulement les
// créneaux libres — le site vitrine affiche les indisponibles grisés. Pas de
// rate limiting dédié : lecture seule, aucun honeypot à contourner, coût
// largement inférieur à Create.
func (h *Handler) Slots(w http.ResponseWriter, r *http.Request) {
	slots, err := h.svc.SlotGrid(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "demorequest", "slots", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "demorequest", "slots", AvailableSlotsResponse{Slots: slots})
}

// Create handles POST /v1/public/demo-request — public, IP-rate-limited
// inside the service (see Service.Create). Called by the site vitrine's
// DemoForm.astro before any account exists, so — like /v1/public/signup-context
// — it cannot sit behind authMiddleware.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateDemoRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "demorequest", "create", models.ErrInvalidRequestBody)
		return
	}
	if err := h.svc.Create(r.Context(), helpers.ClientIP(r), req); err != nil {
		models.SendErrorJSON(w, "demorequest", "create", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "demorequest", "create", map[string]interface{}{"status": "success"})
}
