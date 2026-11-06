package ubereats

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type UberHandler struct {
	svc *UberEatsService
}

func NewUberHandler(svc *UberEatsService) *UberHandler {
	return &UberHandler{svc: svc}
}

// writeOnboardingError traduit les erreurs de la connexion en codes que le
// back-office sait afficher.
func writeOnboardingError(w http.ResponseWriter, fn string, err error) {
	status, code := 0, ""
	switch {
	case errors.Is(err, ErrOnboardingNotConfigured):
		status, code = http.StatusServiceUnavailable, "not_configured"
	case errors.Is(err, ErrConnectSessionNotFound):
		status, code = http.StatusNotFound, "session_expired"
	case errors.Is(err, ErrNoStoreSelected):
		status, code = http.StatusBadRequest, "no_store_selected"
	case errors.Is(err, ErrAccountNotFound):
		status, code = http.StatusNotFound, "account_not_found"
	}
	if status == 0 {
		models.SendErrorJSON(w, "uber", fn, err)
		return
	}
	models.SendJSON(w, status, "uber", fn, map[string]string{"status": "0", "error": code})
}

// Connect : GET /integrations/uber-eats/connect — adresse d'autorisation Uber
// vers laquelle le back-office redirige le restaurateur.
func (h *UberHandler) Connect(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	authURL, err := h.svc.ConnectURL(user.MerchantID, user.UserID)
	if err != nil {
		writeOnboardingError(w, "connect", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "uber", "connect", AuthURLResponse{URL: authURL})
}

// Callback : GET /integrations/uber-eats/callback — retour d'Uber (public, le
// navigateur du restaurateur ; l'établissement est dans le state signé).
// Renvoie vers le back-office avec ?uber=select&session=… | disconnected |
// error&reason=…
func (h *UberHandler) Callback(w http.ResponseWriter, r *http.Request) {
	outcome := h.svc.HandleOAuthCallback(r.Context(), r.URL.Query())

	returnURL := h.svc.ConnectReturnURL()
	target, err := url.Parse(returnURL)
	if returnURL == "" || err != nil {
		models.SendJSON(w, http.StatusOK, "uber", "callback", map[string]string{
			"result": outcome.Result, "reason": outcome.Reason,
		})
		return
	}
	params := target.Query()
	params.Set("uber", outcome.Result)
	if outcome.Reason != "" {
		params.Set("reason", outcome.Reason)
	}
	if outcome.Session != "" {
		params.Set("session", outcome.Session)
	}
	target.RawQuery = params.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// GetConnectSession : GET /integrations/uber-eats/connect/sessions/{session_id}
// — restaurants du compte Uber Eats à choisir.
func (h *UberHandler) GetConnectSession(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	view, err := h.svc.GetConnectSession(r.Context(), user.MerchantID, chi.URLParam(r, "session_id"))
	if err != nil {
		writeOnboardingError(w, "get_connect_session", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "uber", "get_connect_session", view)
}

// CompleteConnect : POST /integrations/uber-eats/connect/sessions/{session_id}
// body {"store_ids": [...]} — connecte les restaurants choisis.
func (h *UberHandler) CompleteConnect(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var body struct {
		StoreIDs []string `json:"store_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		models.SendErrorJSON(w, "uber", "complete_connect", models.ErrInvalidRequestBody)
		return
	}
	results, err := h.svc.CompleteConnect(r.Context(), user.MerchantID, user.UserID, chi.URLParam(r, "session_id"), body.StoreIDs)
	if err != nil {
		writeOnboardingError(w, "complete_connect", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "uber", "complete_connect", map[string]any{"results": results})
}

// Disconnect : POST /integrations/uber-eats/disconnect?store_id= — adresse
// d'autorisation Uber qui, au retour, retire l'intégration du restaurant
// (store_id absent : compte principal). Uber exige le token du restaurateur
// pour ce retrait, d'où le passage par sa page de connexion.
func (h *UberHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	authURL, err := h.svc.DisconnectURL(r.Context(), user.MerchantID, user.UserID, r.URL.Query().Get("store_id"))
	if err != nil {
		writeOnboardingError(w, "disconnect", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "uber", "disconnect", AuthURLResponse{URL: authURL})
}

// ListMenuSyncs : GET /integrations/uber-eats/syncs?store_id=&limit=
// Dernières publications du menu de l'établissement connecté.
func (h *UberHandler) ListMenuSyncs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	syncs, err := h.svc.ListMenuSyncs(r.Context(), user.MerchantID, r.URL.Query().Get("store_id"), limit)
	if err != nil {
		models.SendErrorJSON(w, "integrations", "list_uber_eats_syncs", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "integrations", "list_uber_eats_syncs", map[string]any{"syncs": syncs})
}
