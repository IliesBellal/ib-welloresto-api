package deliveroo

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
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
	brandID, err := h.service.ValidateAndSyncBrandID(ctx, merchantID, "")
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
	menuID := "3" // Identifiant unique pour ce menu

	err := h.service.RunMenuScenario(r.Context(), merchantID, menuID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "Menu uploaded successfully"})
}

func (h *DeliverooHandler) RunUnavailabilitiesScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	merchantID := "2"
	menuID := "3" // Identifiant unique pour ce menu

	err := h.service.RunUnavailabilitiesScenario(r.Context(), merchantID, menuID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "Disabled successfully"})
}

func (h *DeliverooHandler) HandleScenario9(w http.ResponseWriter, r *http.Request) {
	// Extraction des paramètres (ex: /test/scenario9?merchant_id=123&menu_id=menu-abc)
	merchantID := "2"
	menuID := "3"

	if merchantID == "" || menuID == "" {
		http.Error(w, "missing merchant_id or menu_id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err := h.service.RunScenario9(ctx, merchantID, menuID)
	if err != nil {
		log.Printf("Scenario 9 failed: %v", err)
		http.Error(w, "Scenario 9 failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Scenario 9 completed successfully! Check your Deliveroo Sandbox portal."))
}

func (h *DeliverooHandler) HandleScenario10(w http.ResponseWriter, r *http.Request) {
	merchantID := "2"
	menuID := "3"

	if merchantID == "" || menuID == "" {
		http.Error(w, "missing merchant_id or menu_id", http.StatusBadRequest)
		return
	}

	if err := h.service.RunScenario10(r.Context(), merchantID, menuID); err != nil {
		http.Error(w, "Scenario 10 failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Scenario 10: Unavailabilities reset successfully!"))
}

func (h *DeliverooHandler) HandleScenario11(w http.ResponseWriter, r *http.Request) {
	merchantID := "2"
	menuID := "3"

	if merchantID == "" || menuID == "" {
		http.Error(w, "missing merchant_id or menu_id", http.StatusBadRequest)
		return
	}

	if err := h.service.RunScenario11(r.Context(), merchantID, menuID); err != nil {
		http.Error(w, "Scenario 11 failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Scenario 11: Initial states set. Wait for Deliveroo to simulate the morning reset!"))
}

func (h *DeliverooHandler) HandleScenario12(w http.ResponseWriter, r *http.Request) {
	merchantID := "2"
	menuID := "3"

	if merchantID == "" || menuID == "" {
		http.Error(w, "missing merchant_id or menu_id", http.StatusBadRequest)
		return
	}

	if err := h.service.RunScenario12(r.Context(), merchantID, menuID); err != nil {
		http.Error(w, "Scenario 12 failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Scenario 12: whole_milk updated. Deliveroo will now skip the morning reset."))
}

func (h *DeliverooHandler) HandleTriggerScenario13(w http.ResponseWriter, r *http.Request) {
	// On récupère les IDs via la query string
	merchantID := "2"
	menuID := "3"

	if merchantID == "" || menuID == "" {
		http.Error(w, "merchant_id and menu_id are required", http.StatusBadRequest)
		return
	}

	// On lance le processus d'upload du gros menu
	payload, err := h.service.RunScenario13(r.Context(), merchantID, menuID)
	if err != nil {
		http.Error(w, "Failed to trigger Scenario 13: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Scenario 13 triggered: Large menu uploaded. Now wait for the webhook to finalize the test!"))
	json.NewEncoder(w).Encode(payload)
}

func (h *DeliverooHandler) HandleScenario14(w http.ResponseWriter, r *http.Request) {
	merchantID := "2"
	menuID := "3"

	uploadURL, err := h.service.RunScenario14(r.Context(), merchantID, menuID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// On renvoie l'URL au client (ou on la loggue) pour vérifier que ça marche
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message":    "Scenario 14: URL generated successfully",
		"upload_url": uploadURL,
	})
}

func (h *DeliverooHandler) HandleScenario15(w http.ResponseWriter, r *http.Request) {
	merchantID := "2"
	menuID := "3"

	jobID, err := h.service.RunScenario15(r.Context(), merchantID, menuID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TRÈS IMPORTANT : Note bien ce jobID, on en aura besoin pour le Scénario 16
	json.NewEncoder(w).Encode(map[string]string{
		"status": "Job Created",
		"job_id": jobID,
	})
}

func (h *DeliverooHandler) HandleScenario16(w http.ResponseWriter, r *http.Request) {
	// Si tu utilises Chi ou un autre routeur :
	jobID := chi.URLParam(r, "job_id")
	merchantID := "2" // Ton merchant de test

	status, err := h.service.RunScenario16(r.Context(), merchantID, jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Route: r.Get("/deliveroo/17", deliverooHandler.HandleScenario17)

func (h *DeliverooHandler) HandleScenario17(w http.ResponseWriter, r *http.Request) {
	merchantID := "2"
	menuID := "3"

	downloadURL, err := h.service.RunScenario17(r.Context(), merchantID, menuID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":       "Success",
		"download_url": downloadURL,
	})
}
