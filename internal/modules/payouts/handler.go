package payouts

import (
	"errors"
	"net/http"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// Handler expose les justificatifs de versement au back-office.
type Handler struct {
	service *Service
}

// NewHandler construit le handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// List GET /accounting/payouts — les justificatifs envoyés à l'établissement de
// l'utilisateur, du plus récent au plus ancien.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "payouts", "list", models.ErrUnauthorized)
		return
	}
	payouts, err := h.service.ListForMerchant(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "payouts", "list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "payouts", "list", map[string]any{"status": "1", "payouts": payouts})
}

// Download GET /accounting/payouts/{payout_id}/{kind}/download — kind vaut
// "statement" (relevé) ou "invoice" (facture de commission). Renvoie un lien
// signé d'une heure.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "payouts", "download", models.ErrUnauthorized)
		return
	}
	kind := chi.URLParam(r, "kind")
	if kind != KindStatement && kind != KindInvoice {
		models.SendErrorJSON(w, "payouts", "download", models.ErrNotFound)
		return
	}

	link, err := h.service.DownloadLink(r.Context(), user.MerchantID, chi.URLParam(r, "payout_id"), kind)
	if errors.Is(err, ErrDocumentUnavailable) {
		models.SendErrorJSON(w, "payouts", "download", models.ErrNotFound)
		return
	}
	if err != nil {
		models.SendErrorJSON(w, "payouts", "download", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "payouts", "download", link)
}
