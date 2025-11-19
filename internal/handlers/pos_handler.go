package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/middleware"

	"welloresto-api/internal/services"
)

type POSHandler struct {
	service *services.POSService
}

func NewPOSHandler(s *services.POSService) *POSHandler {
	return &POSHandler{service: s}
}

func (h *POSHandler) GetPOSStatus(w http.ResponseWriter, r *http.Request) {
	token := middleware.GetToken(r)

	resp, err := h.service.GetPOSStatus(r.Context(), token)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   10,
			"data": map[string]string{"error": err.Error()},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":   10,
		"data": map[string]interface{}{"pos_status": resp},
	})
}

func (h *POSHandler) UpdatePOSStatus(w http.ResponseWriter, r *http.Request) {
	token := middleware.GetToken(r)

	var body struct {
		Status bool `json:"status"`
	}

	json.NewDecoder(r.Body).Decode(&body)

	resp, err := h.service.UpdatePOSStatus(r.Context(), token, body.Status)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   10,
			"data": map[string]string{"error": err.Error()},
		})
		return
	}

	// Return same as GET
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":   10,
		"data": map[string]interface{}{"pos_status": resp},
	})
}
