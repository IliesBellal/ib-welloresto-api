package reviewrequests

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

const module = "review_requests"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ClientKey : limitation par visiteur des routes publiques (adresse réelle
// derrière le proxy de l'hébergeur).
func ClientKey(r *http.Request) (string, error) {
	return "review-optout:" + helpers.ProxyClientIP(r), nil
}

// GetInfo handles GET /scannorder/review-requests/{token} (public).
func (h *Handler) GetInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.Info(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		models.SendErrorJSON(w, module, "get_info", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "get_info", info)
}

// OptOut handles POST /scannorder/review-requests/{token}/opt-out (public).
func (h *Handler) OptOut(w http.ResponseWriter, r *http.Request) {
	h.setOptOut(w, r, true, "opt_out")
}

// OptIn handles POST /scannorder/review-requests/{token}/opt-in (public) :
// annule une désinscription faite par erreur.
func (h *Handler) OptIn(w http.ResponseWriter, r *http.Request) {
	h.setOptOut(w, r, false, "opt_in")
}

func (h *Handler) setOptOut(w http.ResponseWriter, r *http.Request, optedOut bool, fn string) {
	info, err := h.svc.SetOptOut(r.Context(), chi.URLParam(r, "token"), optedOut)
	if err != nil {
		models.SendErrorJSON(w, module, fn, err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, fn, info)
}

// SendTest handles POST /integrations/scannorder/seo/review-request/test :
// e-mail de test envoyé à l'utilisateur connecté.
func (h *Handler) SendTest(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	err := h.svc.SendTest(r.Context(), user.MerchantID, user.Email)
	switch {
	case errors.Is(err, ErrMissingReviewURL), errors.Is(err, ErrMissingRecipient):
		models.SendJSON(w, http.StatusBadRequest, module, "send_test", map[string]string{"error": err.Error()})
	case errors.Is(err, ErrNotConfigured):
		models.SendJSON(w, http.StatusServiceUnavailable, module, "send_test", map[string]string{"error": err.Error()})
	case err != nil:
		models.SendJSON(w, http.StatusBadGateway, module, "send_test", map[string]string{"error": "send_failed"})
	default:
		models.SendJSON(w, http.StatusOK, module, "send_test", map[string]string{"sent_to": user.Email})
	}
}
