package deliveroo

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type DeliverooHandler struct {
	service *DeliverooService
}

func NewDeliverooHandler(service *DeliverooService) *DeliverooHandler {
	return &DeliverooHandler{service: service}
}

// GetConnectInfo : GET /integrations/deliveroo/connect — identifiant à saisir
// dans le Partner Hub et sites déjà connus (docs/marketplace-onboarding.md).
func (h *DeliverooHandler) GetConnectInfo(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	info, err := h.service.GetConnectInfo(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "integrations", "get_deliveroo_connect", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "integrations", "get_deliveroo_connect", info)
}

// Connect : POST /integrations/deliveroo/connect body {"location_id": "..."} —
// vérifie chez Deliveroo que le site est relié à Wello Resto et l'active.
func (h *DeliverooHandler) Connect(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var body struct {
		LocationID string `json:"location_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		models.SendErrorJSON(w, "integrations", "connect_deliveroo", models.ErrInvalidRequestBody)
		return
	}
	res, err := h.service.Connect(r.Context(), user.MerchantID, body.LocationID)
	if err != nil {
		status, code := 0, ""
		switch {
		case errors.Is(err, ErrLocationNotAllowed):
			status, code = http.StatusBadRequest, "location_not_allowed"
		case errors.Is(err, ErrLocationLinkedElsewhere):
			status, code = http.StatusConflict, "linked_elsewhere"
		case errors.Is(err, ErrLocationNotLinked):
			status, code = http.StatusUnprocessableEntity, "not_linked"
		}
		if status == 0 {
			models.SendErrorJSON(w, "integrations", "connect_deliveroo", err)
			return
		}
		models.SendJSON(w, status, "integrations", "connect_deliveroo", map[string]string{"status": "0", "error": code})
		return
	}
	models.SendJSON(w, http.StatusOK, "integrations", "connect_deliveroo", res)
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

// HandleTriggerScenario13 publie le vrai menu du marchand de test en V1 ; le
// webhook menu.upload_result aligne ensuite les disponibilités (voir
// docs/deliveroo-menu-sync.md, « Scénarios de certification »).
func (h *DeliverooHandler) HandleTriggerScenario13(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.RunScenario13(r.Context(), testMerchantID)
	writeScenarioResult(w, "Scenario 13: menu sent (V1). Wait for the menu.upload_result webhook: item availabilities are then pushed automatically.", result, err)
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

// HandleScenario15 publie le vrai menu du marchand de test en V3 (URL
// présignée, dépôt S3, job) ; le job_id renvoyé se suit avec /test/deliveroo/16/{job_id}.
func (h *DeliverooHandler) HandleScenario15(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.RunScenario15(r.Context(), testMerchantID)
	writeScenarioResult(w, "Scenario 15: menu uploaded to S3 and publish job created (V3). Wait for the menu.upload_result webhook.", result, err)
}

// testMerchantID : marchand de test du sandbox Deliveroo (routes /test).
const testMerchantID = "2"

func writeScenarioResult(w http.ResponseWriter, message string, result *PublishResult, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusInternalServerError
		if IsPublishTooSoon(err) {
			status = http.StatusTooManyRequests
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"message": message, "result": result})
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

// ListMenuSyncs : GET /integrations/deliveroo/syncs?location_id=&limit=
// Dernières publications du menu de l'établissement connecté.
func (h *DeliverooHandler) ListMenuSyncs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	syncs, err := h.service.ListMenuSyncs(r.Context(), user.MerchantID, r.URL.Query().Get("location_id"), limit)
	if err != nil {
		models.SendErrorJSON(w, "integrations", "list_deliveroo_syncs", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "integrations", "list_deliveroo_syncs", map[string]any{"syncs": syncs})
}
