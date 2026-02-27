package deliveroo

import (
	"encoding/json"
	"log"
	"net/http"
)

type DeliverooHandler struct {
	service *DeliverooService
}

func NewDeliverooHandler(service *DeliverooService) *DeliverooHandler {
	return &DeliverooHandler{service: service}
}

func (h *DeliverooHandler) SyncSiteBrandID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// On récupère le merchant_id (soit via query param, soit via le body)
	// Ici, prenons le query param pour la simplicité du test
	merchantID := "2"

	ctx := r.Context()

	// Appel au service que nous avons défini précédemment
	brandID, err := h.service.ValidateAndSyncBrandID(ctx, merchantID)
	if err != nil {
		// Log l'erreur détaillée en interne
		log.Printf("Error syncing brand ID for merchant %s: %v", merchantID, err)

		// Retourne une erreur propre au client
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Failed to retrieve Brand ID from Deliveroo",
			"details": err.Error(),
		})
		return
	}

	// Réponse de succès
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "success",
		"brand_id": brandID,
		"message":  "Brand ID successfully retrieved and synchronized",
	})
}

func (h *DeliverooHandler) UploadTestMenu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	merchantID := "2"
	menuID := "2" // Identifiant unique pour ce menu

	err := h.service.RunMenuScenario(r.Context(), merchantID, menuID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "Menu uploaded successfully"})
}
