package customer_accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// UpdateCustomerSettings PUT /customer-accounts/{customer_id}/settings
// (customers.manage) : plafond propre au client, ardoise interdite.
func (h *Handler) UpdateCustomerSettings(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerIDParam(r)
	if !ok {
		models.SendJSON(w, http.StatusBadRequest, module, "update_customer_settings", map[string]string{"error": "invalid_customer_id"})
		return
	}
	var req models.CustomerAccountCustomerSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, module, "update_customer_settings", map[string]string{"error": "invalid_body"})
		return
	}
	account, err := h.svc.UpdateCustomerSettings(r.Context(), customerID, req)
	if err != nil {
		models.SendErrorJSON(w, module, "update_customer_settings", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "update_customer_settings", map[string]interface{}{"status": "success", "account": account})
}

// Aging GET /customer-accounts/aging : balance âgée des ardoises.
func (h *Handler) Aging(w http.ResponseWriter, r *http.Request) {
	aging, err := h.svc.Aging(r.Context())
	if err != nil {
		models.SendErrorJSON(w, module, "aging", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "aging", map[string]interface{}{"status": "success", "aging": aging})
}

// Monthly GET /customer-accounts/monthly?year=AAAA : totaux mensuels.
func (h *Handler) Monthly(w http.ResponseWriter, r *http.Request) {
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	months, err := h.svc.Monthly(r.Context(), year)
	if err != nil {
		models.SendErrorJSON(w, module, "monthly", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "monthly", map[string]interface{}{"status": "success", "months": months})
}

// Statement GET /customer-accounts/{customer_id}/statement?from=&to= : relevé PDF.
func (h *Handler) Statement(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerIDParam(r)
	if !ok {
		models.SendJSON(w, http.StatusBadRequest, module, "statement", map[string]string{"error": "invalid_customer_id"})
		return
	}
	pdf, name, err := h.svc.StatementPDF(r.Context(), customerID, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		models.SendErrorJSON(w, module, "statement", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

// SendStatement POST /customer-accounts/{customer_id}/statement/send
// {from, to, email} : relevé PDF par email.
func (h *Handler) SendStatement(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerIDParam(r)
	if !ok {
		models.SendJSON(w, http.StatusBadRequest, module, "send_statement", map[string]string{"error": "invalid_customer_id"})
		return
	}
	var req struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, module, "send_statement", map[string]string{"error": "invalid_body"})
		return
	}
	sentTo, err := h.svc.SendStatement(r.Context(), customerID, req.From, req.To, req.Email)
	if err != nil {
		models.SendErrorJSON(w, module, "send_statement", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "send_statement", map[string]interface{}{"status": "success", "email": sentTo})
}

// Remind POST /customer-accounts/{customer_id}/remind : relance immédiate.
func (h *Handler) Remind(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerIDParam(r)
	if !ok {
		models.SendJSON(w, http.StatusBadRequest, module, "remind", map[string]string{"error": "invalid_customer_id"})
		return
	}
	reminder, err := h.svc.Remind(r.Context(), customerID)
	if err != nil {
		models.SendErrorJSON(w, module, "remind", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "remind", map[string]interface{}{"status": "success", "reminder": reminder})
}

// PaymentLink POST /customer-accounts/{customer_id}/payment-link : lien de
// paiement en ligne (établissement avec un compte Stripe).
func (h *Handler) PaymentLink(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerIDParam(r)
	if !ok {
		models.SendJSON(w, http.StatusBadRequest, module, "payment_link", map[string]string{"error": "invalid_customer_id"})
		return
	}
	url, err := h.svc.PaymentLink(r.Context(), customerID)
	if err != nil {
		models.SendErrorJSON(w, module, "payment_link", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "payment_link", map[string]interface{}{"status": "success", "url": url})
}

// PayOnline GET /pay/account/{token} (public) : redirige vers la page de
// paiement Stripe pour la dette du moment.
func (h *Handler) PayOnline(w http.ResponseWriter, r *http.Request) {
	url, err := h.svc.StartOnlinePayment(r.Context(), chi.URLParam(r, "token"))
	switch {
	case err == nil:
		http.Redirect(w, r, url, http.StatusSeeOther)
	case errors.Is(err, models.ErrAccountNothingDue):
		writePayPage(w, http.StatusOK, "Rien à régler", "Votre compte est à jour : il n'y a rien à régler pour le moment. Merci !")
	case errors.Is(err, models.ErrNotFound):
		writePayPage(w, http.StatusNotFound, "Lien expiré", "Ce lien de paiement n'est plus valable. Demandez-en un nouveau à l'établissement.")
	default:
		logger.FromContext(r.Context()).Error("customer account online payment", zap.Error(err))
		writePayPage(w, http.StatusServiceUnavailable, "Paiement indisponible",
			"Le paiement en ligne est momentanément indisponible. Merci de réessayer plus tard ou de régler sur place.")
	}
}

// PayOnlineDone GET /pay/account/{token}/done?status= (public) : retour de Stripe.
func (h *Handler) PayOnlineDone(w http.ResponseWriter, r *http.Request) {
	name := h.svc.OnlinePaymentMerchantName(r.Context(), chi.URLParam(r, "token"))
	if r.URL.Query().Get("status") == "success" {
		msg := "Votre paiement est bien reçu. Merci !"
		if name != "" {
			msg = "Votre paiement à " + name + " est bien reçu. Merci !"
		}
		writePayPage(w, http.StatusOK, "Paiement reçu", msg)
		return
	}
	writePayPage(w, http.StatusOK, "Paiement annulé",
		"Le paiement n'a pas été effectué. Vous pouvez réessayer depuis le lien reçu, ou régler sur place.")
}

// writePayPage : page HTML minimale du lien de paiement public.
func writePayPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	t, m := html.EscapeString(title), html.EscapeString(message)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="fr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#f5f7fb;color:#050817;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0;padding:16px}main{background:#fff;border-radius:16px;padding:32px;max-width:420px;box-shadow:0 4px 24px rgba(5,8,23,.08);text-align:center}h1{font-size:22px;margin:0 0 12px}p{color:#5b6680;line-height:1.5;margin:0}</style>
</head><body><main><h1>%s</h1><p>%s</p></main></body></html>`, t, t, m)
}
