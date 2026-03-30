package ubereats

import (
	"net/http"
	"welloresto-api/internal/middleware" // Pour récupérer le MerchantID
	"welloresto-api/internal/models"     // Pour SendJSON (ton helper de réponse)
)

type UberHandler struct {
	svc *UberEatsService
}

func NewUberHandler(svc *UberEatsService) *UberHandler {
	return &UberHandler{svc: svc}
}

const (
	redirectURI = "https://api.welloresto.com/api/integrations/uber/callback"
	callbackURL = "https://app.welloresto.com/settings/integrations"
)

// GetConnectURL renvoie l'URL vers laquelle le frontend doit rediriger l'utilisateur
func (h *UberHandler) GetConnectURL(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r) // Récupéré via ton AuthMiddleware

	// C'est l'URL de ton back-end qui recevra la réponse d'Uber
	redirectURI := redirectURI

	urlStr := h.svc.GenerateAuthURL(user.MerchantID, redirectURI)

	models.SendJSON(w, http.StatusOK, "uber", "connect", AuthURLResponse{URL: urlStr})
}

// Callback est appelé directement par Uber après la validation du restaurateur
func (h *UberHandler) Callback(w http.ResponseWriter, r *http.Request) {
	// Uber renvoie les paramètres en Query string (?code=xxx&state=yyy)
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state") // Contient le MerchantID
	redirectURI := redirectURI          // Doit être identique à la demande

	if err := h.svc.HandleCallback(r.Context(), code, state, redirectURI); err != nil {
		// Rediriger vers le front-end avec une erreur
		http.Redirect(w, r, callbackURL+"?error=uber_failed", http.StatusTemporaryRedirect)
		return
	}

	// Succès ! On redirige le restaurateur vers son dashboard avec un message de succès
	http.Redirect(w, r, callbackURL+"?success=uber_connected", http.StatusTemporaryRedirect)
}

// Disconnect coupe la liaison
func (h *UberHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	if err := h.svc.Disconnect(r.Context(), user.MerchantID); err != nil {
		models.SendErrorJSON(w, "uber", "disconnect", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "uber", "disconnect", map[string]string{"status": "disconnected"})
}
