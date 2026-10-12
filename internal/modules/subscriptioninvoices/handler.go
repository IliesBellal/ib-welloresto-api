package subscriptioninvoices

import (
	"errors"
	"net/http"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// Handler expose les factures d'abonnement au back-office.
type Handler struct {
	service *Service
}

// NewHandler construit le handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// List GET /billing/invoices — les factures d'abonnement envoyées à
// l'établissement de l'utilisateur, de la plus récente à la plus ancienne.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "subscription_invoices", "list", models.ErrUnauthorized)
		return
	}
	invoices, err := h.service.ListForMerchant(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "subscription_invoices", "list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "subscription_invoices", "list", map[string]any{"status": "1", "invoices": invoices})
}

// Download GET /billing/invoices/{invoice_id}/download — lien signé d'une heure.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "subscription_invoices", "download", models.ErrUnauthorized)
		return
	}
	link, err := h.service.DownloadLink(r.Context(), user.MerchantID, chi.URLParam(r, "invoice_id"))
	if errors.Is(err, ErrDocumentUnavailable) {
		models.SendErrorJSON(w, "subscription_invoices", "download", models.ErrNotFound)
		return
	}
	if err != nil {
		models.SendErrorJSON(w, "subscription_invoices", "download", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "subscription_invoices", "download", link)
}
