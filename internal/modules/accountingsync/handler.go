package accountingsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

const handlerModule = "accounting_sync"

// syncNowTimeout borne une synchronisation manuelle (40 jours au plus, 300 ms
// entre deux envois, plus les appels à l'outil).
const syncNowTimeout = 3 * time.Minute

// Handler expose la synchronisation comptable au back-office.
type Handler struct {
	service *Service
	// returnURL : page du back-office où l'on revient après l'autorisation OAuth.
	returnURL string
}

// NewHandler construit le handler. returnURL peut être vide (retour en JSON).
func NewHandler(service *Service, returnURL string) *Handler {
	return &Handler{service: service, returnURL: returnURL}
}

func (h *Handler) merchant(w http.ResponseWriter, r *http.Request, fn string) (string, bool) {
	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendErrorJSON(w, handlerModule, fn, models.ErrUnauthorized)
		return "", false
	}
	return user.MerchantID, true
}

// writeError traduit une erreur du service en réponse : les erreurs de saisie
// ou de configuration sont renvoyées avec un message exploitable à l'écran.
func (h *Handler) writeError(w http.ResponseWriter, fn string, err error) {
	var unknown *UnknownAccountsError
	var rejected *RejectedError
	status, code := 0, ""
	switch {
	case errors.As(err, &unknown):
		status, code = http.StatusUnprocessableEntity, "unknown_accounts"
	case errors.Is(err, ErrInvalidMapping):
		status, code = http.StatusBadRequest, "invalid_mapping"
	case errors.Is(err, ErrUnknownProvider), errors.Is(err, ErrAuthMethod):
		status, code = http.StatusBadRequest, "invalid_provider"
	case errors.Is(err, ErrAlreadyConnected):
		status, code = http.StatusConflict, "already_connected"
	case errors.Is(err, ErrAuthRejected):
		status, code = http.StatusUnprocessableEntity, "credentials_rejected"
	case errors.Is(err, ErrNotConnected), errors.Is(err, ErrItemNotFound):
		models.SendErrorJSON(w, handlerModule, fn, models.ErrNotFound)
		return
	case errors.Is(err, ErrItemNotRetryable):
		status, code = http.StatusConflict, "not_retryable"
	case errors.Is(err, ErrNotActive):
		status, code = http.StatusConflict, "not_active"
	case errors.Is(err, ErrNotConfigured):
		status, code = http.StatusServiceUnavailable, "not_configured"
	case errors.As(err, &rejected):
		status, code = http.StatusUnprocessableEntity, "provider_rejected"
	}
	if status == 0 {
		models.SendErrorJSON(w, handlerModule, fn, err)
		return
	}
	models.SendJSON(w, status, handlerModule, fn, map[string]any{"status": "0", "error": code, "message": err.Error()})
}

// Get GET /accounting-integrations — le catalogue des outils et la connexion de l'établissement.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "get")
	if !ok {
		return
	}
	conn, err := h.service.Connection(r.Context(), merchantID)
	if err != nil {
		h.writeError(w, "get", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "get", map[string]any{
		"status": "1", "providers": h.service.Catalog(), "connection": conn,
	})
}

// ConnectToken POST /accounting-integrations/{provider}/connect/token — body {"token": "..."}.
func (h *Handler) ConnectToken(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "connect_token")
	if !ok {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		models.SendErrorJSON(w, handlerModule, "connect_token", models.ErrInvalidRequestBody)
		return
	}
	conn, err := h.service.ConnectWithToken(r.Context(), merchantID, chi.URLParam(r, "provider"), body.Token)
	if err != nil {
		h.writeError(w, "connect_token", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "connect_token", map[string]any{"status": "1", "connection": conn})
}

// ConnectOAuth GET /accounting-integrations/{provider}/connect/oauth — l'adresse
// d'autorisation chez l'outil, vers laquelle le back-office redirige l'utilisateur.
func (h *Handler) ConnectOAuth(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "connect_oauth")
	if !ok {
		return
	}
	authURL, err := h.service.OAuthStartURL(r.Context(), merchantID, chi.URLParam(r, "provider"))
	if err != nil {
		h.writeError(w, "connect_oauth", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "connect_oauth", map[string]any{"status": "1", "url": authURL})
}

// OAuthCallback GET /accounting-integrations/oauth/callback — retour de l'outil
// après l'autorisation (public : la requête vient du navigateur, l'établissement
// est retrouvé dans le paramètre « state » authentifié).
func (h *Handler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result := "ok"
	reason := ""
	if e := q.Get("error"); e != "" {
		result, reason = "error", "denied"
	} else if _, err := h.service.OAuthComplete(r.Context(), q.Get("state"), q.Get("code")); err != nil {
		result = "error"
		switch {
		case errors.Is(err, ErrInvalidState):
			reason = "invalid_state"
		case errors.Is(err, ErrAlreadyConnected):
			reason = "already_connected"
		default:
			reason = "failed"
		}
	}
	if h.returnURL == "" {
		models.SendJSON(w, http.StatusOK, handlerModule, "oauth_callback", map[string]any{"status": map[bool]string{true: "1", false: "0"}[result == "ok"], "result": result, "reason": reason})
		return
	}
	target, err := url.Parse(h.returnURL)
	if err != nil {
		models.SendErrorJSON(w, handlerModule, "oauth_callback", err)
		return
	}
	params := target.Query()
	params.Set("accounting", result)
	if reason != "" {
		params.Set("reason", reason)
	}
	target.RawQuery = params.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// SaveMapping PUT /accounting-integrations/mapping — la correspondance de comptes ;
// la valide chez l'outil et active la synchronisation.
func (h *Handler) SaveMapping(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "save_mapping")
	if !ok {
		return
	}
	var m Mapping
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&m); err != nil {
		models.SendErrorJSON(w, handlerModule, "save_mapping", models.ErrInvalidRequestBody)
		return
	}
	conn, err := h.service.SaveMapping(r.Context(), merchantID, m)
	if err != nil {
		h.writeError(w, "save_mapping", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "save_mapping", map[string]any{"status": "1", "connection": conn})
}

// Disconnect DELETE /accounting-integrations.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "disconnect")
	if !ok {
		return
	}
	if err := h.service.Disconnect(r.Context(), merchantID); err != nil {
		h.writeError(w, "disconnect", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "disconnect", map[string]any{"status": "1"})
}

// Items GET /accounting-integrations/items — journal des jours envoyés.
func (h *Handler) Items(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "items")
	if !ok {
		return
	}
	items, err := h.service.Items(r.Context(), merchantID)
	if err != nil {
		h.writeError(w, "items", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "items", map[string]any{"status": "1", "items": items})
}

// SyncNow POST /accounting-integrations/sync — envoie tout de suite les journées
// clôturées en attente. Le traitement va au bout même si le navigateur se
// déconnecte en route (les envois déjà faits chez l'outil doivent être enregistrés).
func (h *Handler) SyncNow(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "sync_now")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), syncNowTimeout)
	defer cancel()
	res, err := h.service.SyncNow(ctx, merchantID)
	if err != nil {
		h.writeError(w, "sync_now", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "sync_now", map[string]any{
		"status": "1", "sent": res.Sent, "failed": res.Failed, "uncertain": res.Uncertain, "busy": res.Busy,
	})
}

// RetryItem POST /accounting-integrations/items/{date}/retry.
func (h *Handler) RetryItem(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := h.merchant(w, r, "retry_item")
	if !ok {
		return
	}
	date := strings.TrimSpace(chi.URLParam(r, "date"))
	if len(date) != 10 {
		models.SendErrorJSON(w, handlerModule, "retry_item", models.ErrInvalidInput)
		return
	}
	if err := h.service.RetryItem(r.Context(), merchantID, date); err != nil {
		h.writeError(w, "retry_item", err)
		return
	}
	models.SendJSON(w, http.StatusOK, handlerModule, "retry_item", map[string]any{"status": "1"})
}
