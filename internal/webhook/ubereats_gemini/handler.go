package ubereats_gemini

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
)

// Handler gère les requêtes HTTP pour les webhooks Uber
type Handler struct {
	svc Service
}

// NewHandler crée un handler HTTP
func NewHandler(svc Service) *Handler {
	return &Handler{
		svc: svc,
	}
}

// ServeHTTP (ou une méthode HandleWebhook) reçoit la requête
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// 1. Décoder le JSON
	var payload WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	// 2. IMPORTANT : Répondre immédiatement à Uber (200 OK)
	// Uber attend une réponse rapide. Si on traite la commande maintenant, on risque le timeout.
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))

	// 3. Traitement Asynchrone
	// On lance une Goroutine pour traiter la logique métier sans bloquer la réponse HTTP.
	go func() {
		// On crée un contexte background car le contexte de la requête 'r.Context()'
		// sera annulé dès que la fonction HandleWebhook se termine.
		ctx := context.Background()

		// Appel de la logique métier
		err := h.svc.ProcessEvent(ctx, payload)
		if err != nil {
			// Ici, on loggerait l'erreur car on ne peut plus la renvoyer au client HTTP
			log.Printf("Erreur lors du traitement du webhook Uber [EventID: %s]: %v", payload.EventID, err)
		}
	}()
}
