package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"welloresto-api/internal/modules/webhook/ubereats/models"
	"welloresto-api/internal/modules/webhook/ubereats/service"
)

type Handler struct {
	service *service.Service
}

func NewHandler(s *service.Service) *Handler {
	return &Handler{service: s}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	h.service.VerifySignature(r.Context(), r.Header, body)

	var event models.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if err := h.service.ProcessEvent(r.Context(), event); err != nil {
		log.Println("[UBER EATS] processing error:", err)
		http.Error(w, "processing error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
