package brevo_events

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type Handler struct {
	service      *Service
	webhookToken string
}

func NewHandler(s *Service, webhookToken string) *Handler {
	return &Handler{service: s, webhookToken: strings.TrimSpace(webhookToken)}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookToken != "" {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token == "" {
			token = strings.TrimSpace(r.Header.Get("X-Webhook-Token"))
		}
		if token != h.webhookToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	var single BrevoEventPayload
	if err := json.Unmarshal(body, &single); err == nil {
		if err := h.service.ProcessEvent(r.Context(), single); err != nil {
			http.Error(w, "processing error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	var batch []BrevoEventPayload
	if err := json.Unmarshal(body, &batch); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	for _, payload := range batch {
		if err := h.service.ProcessEvent(r.Context(), payload); err != nil {
			http.Error(w, "processing error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}
