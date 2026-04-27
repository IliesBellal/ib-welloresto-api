package integrations

import (
	"database/sql"
	"errors"
	"net/http"

	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/stripe/stripe-go/v84"
)

// stripeError maps known error conditions to the error codes expected by the front-end.
// Returns (httpStatus, code).
func stripeError(err error) (int, string) {
	if errors.Is(err, sql.ErrNoRows) {
		return http.StatusNotFound, "account_not_found"
	}
	if err.Error() == "already_verified" {
		return http.StatusConflict, "already_verified"
	}

	// Stripe API errors
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) {
		switch stripeErr.Code {
		case stripe.ErrorCodeResourceMissing:
			return http.StatusNotFound, "account_not_found"
		case "link_expired":
			return http.StatusGone, "link_expired"
		}
		if stripeErr.HTTPStatusCode == http.StatusForbidden {
			return http.StatusForbidden, "account_not_accessible"
		}
	}

	return http.StatusInternalServerError, "internal_error"
}

// GetStripeStatus handles GET /integrations/stripe/status
func (h *Handler) GetStripeStatus(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	status, err := h.svc.GetStripeStatus(r.Context(), user.MerchantID)
	if err != nil {
		code, errCode := stripeError(err)
		models.SendJSON(w, code, "integrations", "get_stripe_status", map[string]string{"error": errCode})
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "get_stripe_status", map[string]interface{}{
		"data": map[string]string{"status": status},
	})
}

// CreateStripeOnboardingLink handles POST /integrations/stripe/onboarding-link
func (h *Handler) CreateStripeOnboardingLink(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	url, err := h.svc.CreateStripeOnboardingLink(r.Context(), user.MerchantID)
	if err != nil {
		code, errCode := stripeError(err)
		models.SendJSON(w, code, "integrations", "stripe_onboarding_link", map[string]string{"error": errCode})
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "stripe_onboarding_link", map[string]interface{}{
		"data": map[string]string{"url": url},
	})
}

// GetStripeBankAccounts handles GET /integrations/stripe/bank-accounts
func (h *Handler) GetStripeBankAccounts(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	accounts, err := h.svc.GetStripeBankAccounts(r.Context(), user.MerchantID)
	if err != nil {
		code, errCode := stripeError(err)
		if errors.Is(err, sql.ErrNoRows) {
			errCode = "account_not_created"
		}
		models.SendJSON(w, code, "integrations", "get_stripe_bank_accounts", map[string]string{"error": errCode})
		return
	}

	if accounts == nil {
		accounts = []stripeclient.BankAccountInfo{}
	}

	models.SendJSON(w, http.StatusOK, "integrations", "get_stripe_bank_accounts", map[string]interface{}{
		"data": map[string]interface{}{"accounts": accounts},
	})
}

// CreateStripeBankAccountLink handles POST /integrations/stripe/bank-account-link
func (h *Handler) CreateStripeBankAccountLink(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	url, err := h.svc.CreateStripeBankAccountLink(r.Context(), user.MerchantID)
	if err != nil {
		code, errCode := stripeError(err)
		models.SendJSON(w, code, "integrations", "stripe_bank_account_link", map[string]string{"error": errCode})
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "stripe_bank_account_link", map[string]interface{}{
		"data": map[string]string{"url": url},
	})
}

// GetStripeBalance handles GET /integrations/stripe/balance
func (h *Handler) GetStripeBalance(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	balance, err := h.svc.GetStripeBalance(r.Context(), user.MerchantID)
	if err != nil {
		code, errCode := stripeError(err)
		models.SendJSON(w, code, "integrations", "get_stripe_balance", map[string]string{"error": errCode})
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "get_stripe_balance", map[string]interface{}{
		"data": map[string]interface{}{
			"available": balance.Available,
			"pending":   balance.Pending,
		},
	})
}
