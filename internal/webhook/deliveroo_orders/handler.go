package deliveroo_orders

import (
	"encoding/json"
	"io"
	"net/http"
	"welloresto-api/internal/logger"
)

type DeliverooHandler struct {
	service *DeliverooService
}

func NewDeliverooHandler(service *DeliverooService) *DeliverooHandler {
	return &DeliverooHandler{service: service}
}

func (h *DeliverooHandler) HandleOrdersWebhook(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())

	// Lecture du body une seule fois
	bodyBytes, _ := io.ReadAll(r.Body)
	// On restaure le body pour le décodeur si besoin, ou on décode direct les bytes
	var payload DeliverooWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var err error
	switch payload.Event {
	case "order.new", "order.new_order":
		err = h.service.ProcessNewOrder(r.Context(), payload)
	case "order.status_update":
		err = h.service.ProcessStatusUpdate(r.Context(), payload)
	default:
		// Fallback robuste
		if payload.Event == "" && payload.Body.Order.Status != "" {
			err = h.service.ProcessStatusUpdate(r.Context(), payload)
		}
		// Note : Si c'est un event "cancel_order" (legacy), on peut l'ignorer et renvoyer 200
	}

	if err != nil {
		log.Error("WEBHOOK DELIVEROO - " + err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// REPONSE STRICTE : 200 OK avec JSON vide
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}
