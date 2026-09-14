package billing

import (
	"net/http"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetActivationStatus handles GET /v1/merchant/activation-status — B2b-3's
// bandeau data source (§7.6): "activation_state" (SETUP/LIVE) and
// "subscription_status" (setup/active/past_due/suspended), read fresh on
// every call so the banner disappears with no cache delay after the
// setup_intent.succeeded webhook.
func (h *Handler) GetActivationStatus(w http.ResponseWriter, r *http.Request) {
	const fnName = "get_activation_status"
	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "billing", fnName, models.ErrUnauthorized)
		return
	}

	st, err := h.svc.GetActivationStatus(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "billing", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "billing", fnName, map[string]interface{}{"status": "success", "activation_status": st})
}

// CreateSepaSetup handles POST /v1/billing/sepa/setup (client-facing,
// settings.manage — see routes.go).
func (h *Handler) CreateSepaSetup(w http.ResponseWriter, r *http.Request) {
	const fnName = "create_sepa_setup"
	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "billing", fnName, models.ErrUnauthorized)
		return
	}

	clientSecret, err := h.svc.CreateSepaSetup(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "billing", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "billing", fnName, map[string]interface{}{"status": "success", "client_secret": clientSecret})
}

// AttachBillingCustomer handles
// POST /v1/admin/merchants/{id}/billing-customer/attach-to/{other_merchant_id}
// (internal, RequirePlatformAdmin — see routes.go).
func (h *Handler) AttachBillingCustomer(w http.ResponseWriter, r *http.Request) {
	const fnName = "attach_billing_customer"
	merchantID := chi.URLParam(r, "id")
	otherMerchantID := chi.URLParam(r, "other_merchant_id")

	customer, err := h.svc.AttachBillingCustomer(r.Context(), merchantID, otherMerchantID)
	if err != nil {
		models.SendErrorJSON(w, "billing", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "billing", fnName, map[string]interface{}{"status": "success", "billing_customer": customer})
}

// DetachBillingCustomer handles
// POST /v1/admin/merchants/{id}/billing-customer/detach.
func (h *Handler) DetachBillingCustomer(w http.ResponseWriter, r *http.Request) {
	const fnName = "detach_billing_customer"
	merchantID := chi.URLParam(r, "id")

	customer, err := h.svc.DetachBillingCustomer(r.Context(), merchantID)
	if err != nil {
		models.SendErrorJSON(w, "billing", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "billing", fnName, map[string]interface{}{"status": "success", "billing_customer": customer})
}
