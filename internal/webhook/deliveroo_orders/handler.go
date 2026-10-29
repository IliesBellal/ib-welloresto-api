package deliveroo_orders

import (
	"encoding/json"
	"io"
	"net/http"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/webhook/deliveroosig"
)

type DeliverooHandler struct {
	service       *DeliverooService
	webhookSecret string
}

func NewDeliverooHandler(service *DeliverooService, webhookSecret string) *DeliverooHandler {
	return &DeliverooHandler{service: service, webhookSecret: webhookSecret}
}

func (h *DeliverooHandler) HandleOrdersWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// 1. Lecture du body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	deliveroosig.Observe(ctx, "orders", r.Header, bodyBytes, h.webhookSecret)

	// 2. Décodage du JSON
	var payload DeliverooWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		log.Error("WEBHOOK DELIVEROO - Invalid JSON: " + err.Error())
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Fallback de sécurité si l'Event est vide mais qu'on a un statut
	if payload.Event == "" && payload.Body.Order.Status != "" {
		payload.Event = "order.status_update"
	}

	// 3. Envoi au Service pour traitement global (Idempotence + Routage)
	if err := h.service.ProcessEvent(ctx, payload); err != nil {
		log.Error("WEBHOOK DELIVEROO PROCESS ERROR - " + err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. RÉPONSE STRICTE : 200 OK avec JSON vide
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}
