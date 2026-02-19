package stripe

import (
	"encoding/json"
	"io"
	"net/http"
)

type Handler struct {
	service *StripeWebhookService
}

func NewHandler(s *StripeWebhookService) *Handler {
	return &Handler{service: s}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	var event StripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}

	if err := h.service.ProcessEvent(r.Context(), event); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(http.StatusOK)
}
