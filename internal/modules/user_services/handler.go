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
		models.SendJSON(w, http.StatusUnauthorized, "services", "get_current", map[string]string{"error": "missing_token"})
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendJSON(w, http.StatusBadRequest, "services", "get_current", map[string]string{"error": "missing_parameter"})
		return
	}

	resp, err := h.servicesService.GetCurrentService(r.Context(), token, deviceID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "services", "get_current", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "services", "get_current", resp)
}
