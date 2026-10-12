package payouts

import (
	"errors"
	"net/http"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// Handler expose les justificatifs de versement et les factures de commission au back-office.
type Handler struct {
	service *Service
}

// NewHandler construit le handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// List GET /accounting/payouts — les relevés envoyés à l'établissement de
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

// Download GET /accounting/payouts/{payout_id}/download — lien signé d'une
// heure vers le relevé du virement.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "payouts", "download", models.ErrUnauthorized)
		return
	}
	link, err := h.service.DownloadLink(r.Context(), user.MerchantID, chi.URLParam(r, "payout_id"))
	h.sendLink(w, "download", link, err)
}

// ListInvoices GET /accounting/commission-invoices — les factures de
// commission mensuelles envoyées à l'établissement, de la plus récente à la plus ancienne.
func (h *Handler) ListInvoices(w http.ResponseWriter, r *http.Request) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "payouts", "list_invoices", models.ErrUnauthorized)
		return
	}
	invoices, err := h.service.ListInvoicesForMerchant(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "payouts", "list_invoices", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "payouts", "list_invoices", map[string]any{"status": "1", "invoices": invoices})
}

// DownloadInvoice GET /accounting/commission-invoices/{number}/{kind}/download —
// kind vaut "invoice" (la facture) ou "recap" (le récapitulatif du mois). Lien
// signé d'une heure.
func (h *Handler) DownloadInvoice(w http.ResponseWriter, r *http.Request) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "payouts", "download_invoice", models.ErrUnauthorized)
		return
	}
	kind := chi.URLParam(r, "kind")
	if kind != KindInvoice && kind != KindRecap {
		models.SendErrorJSON(w, "payouts", "download_invoice", models.ErrNotFound)
		return
	}
	link, err := h.service.InvoiceLink(r.Context(), user.MerchantID, chi.URLParam(r, "number"), kind)
	h.sendLink(w, "download_invoice", link, err)
}

func (h *Handler) sendLink(w http.ResponseWriter, fn string, link *DownloadLink, err error) {
	if errors.Is(err, ErrDocumentUnavailable) {
		models.SendErrorJSON(w, "payouts", fn, models.ErrNotFound)
		return
	}
	if err != nil {
		models.SendErrorJSON(w, "payouts", fn, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "payouts", fn, link)
}
