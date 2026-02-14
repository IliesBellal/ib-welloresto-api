package deliveroo

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/logger"
)

type DeliverooHandler struct {
	service *DeliverooService
}

func NewDeliverooHandler(service *DeliverooService) *DeliverooHandler {
	return &DeliverooHandler{service: service}
}

func (h *DeliverooHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	var payload DeliverooWebhookPayload

	// 1. Decode
à	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 2. Dispatch selon l'event_name
	// Deliveroo envoie souvent "order.create" ou "order.status_update"
	// Si l'event name n'est pas dans le body top-level, il faut vérifier les headers
	// ou la structure (Deliveroo met event_name dans le JSON racine).

	var err error
	switch payload.Event {
	case "order.new_order":
	case "order.new": // Ou la string exacte envoyée par Deliveroo
		err = h.service.ProcessNewOrder(r.Context(), payload)
	case "order.status_update":
		err = h.service.ProcessStatusUpdate(r.Context(), payload)
	default:
		// Si le PHP gérait d'autres cas, les ajouter ici
		// Sinon on ignore ou on traite comme update si le payload match
		if payload.Event == "" && payload.Body.Order.Status != "" {
			// Fallback si event_name vide mais status présent
			err = h.service.ProcessStatusUpdate(r.Context(), payload)
		}
	}

	if err != nil {
		// Log l'erreur
		log.Error("WEBHOOK DELIVEROO - 500 - " + err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
