package user_services

import (
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type ServicesHandler struct {
	servicesService *ServicesService
}

func NewServicesHandler(s *ServicesService) *ServicesHandler {
	return &ServicesHandler{servicesService: s}
}

func (h *ServicesHandler) GetCurrentService(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		http.Error(w, "missing device_id", http.StatusBadRequest)
		return
	}

	resp, err := h.servicesService.GetCurrentService(r.Context(), token, deviceID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "services", "get_current_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "services", "get_current", resp)
}
