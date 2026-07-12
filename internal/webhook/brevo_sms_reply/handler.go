package brevo_sms_reply

import (
	"encoding/json"
	"io"
	"net/http"
)

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

// HandleWebhook reçoit un SMS entrant Brevo (réponse client). Aucune
// vérification de secret — même niveau que le webhook Stripe existant du repo
// (décision de cadrage). Répond 200 immédiatement après traitement synchrone.
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	var reply BrevoSMSReply
	if err := json.Unmarshal(body, &reply); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	h.service.ProcessReply(r.Context(), reply.Phone(), reply.Body())

	w.WriteHeader(http.StatusOK)
}
