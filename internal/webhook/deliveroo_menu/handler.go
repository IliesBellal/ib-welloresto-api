package deliveroo_menu

import (
	"encoding/json"
	"io"
	"net/http"
	"welloresto-api/internal/logger"
)

type MenuWebhookHandler struct {
	service *MenuWebhookService
}

func NewMenuWebhookHandler(service *MenuWebhookService) *MenuWebhookHandler {
	return &MenuWebhookHandler{service: service}
}

func (h *MenuWebhookHandler) HandleMenuWebhook(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// 1. Lecture du body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error("WEBHOOK MENU DELIVEROO - Error reading body: " + err.Error())
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	log.Info("WEBHOOK BODY RECEIVED: " + string(bodyBytes))

	// 2. Décodage dans la structure spécifique au Menu
	var payload MenuWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		log.Error("WEBHOOK MENU DELIVEROO - Invalid JSON: " + err.Error())
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 3. Routage selon l'événement (même si pour l'instant il n'y en a qu'un)
	if payload.Event == "menu.upload_result" {
		err = h.service.ProcessMenuUploadResult(r.Context(), payload)
	} else {
		// On loggue si on reçoit un event non géré sur cette URL
		log.Warn("WEBHOOK MENU DELIVEROO - Received unhandled event: " + payload.Event)
	}

	if err != nil {
		log.Error("WEBHOOK MENU DELIVEROO - Processing error: " + err.Error())
		// Si l'erreur vient de notre logique métier, on renvoie une 500 pour que Deliveroo réessaie
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 4. REPONSE STRICTE : 200 OK
	// Pour les webhooks, il faut répondre vite.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}
