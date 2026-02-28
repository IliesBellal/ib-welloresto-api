package deliveroo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"welloresto-api/internal/config"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type DeliverooService struct {
	repo   *DeliverooRepository
	db     *sql.DB
	client *DeliverooClient
	cfg    config.DeliverooConfig
}

func NewDeliverooService(db *sql.DB, cfg config.DeliverooConfig) *DeliverooService {
	dc := NewDeliverooClient(nil, cfg)
	repo := NewDeliverooRepo(db)
	return &DeliverooService{repo: repo, db: db, client: dc, cfg: cfg}
}

func (s *DeliverooService) SetCollected(ctx context.Context, brandOrderID string) error {
	err := s.client.SetCollected(ctx, brandOrderID)
	if err != nil {
		return err
	}

	return nil
}

func (s *DeliverooService) AcceptOrder(ctx context.Context, merchantID string, orderID string) error {

	// 1️⃣ Load brand_order_id in DB
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("deliveroo: brand_order_id missing")
		}
		return err
	}

	// 3️⃣ Call Deliveroo API
	err = s.client.AcceptOrder(ctx, brandOrderID)
	if err != nil {
		return err
	}

	// 5️⃣ Non-2xx → return error with payload
	return nil
}

func (s *DeliverooService) StartDeliverooDelivery(ctx context.Context, brandOrderID string) error {

	_, err := s.repo.MarkDeliverooDeliveryStarted(ctx, brandOrderID)
	if err != nil {
		return err
	}

	payload := map[string]string{
		"stage":       "collected",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	}

	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		"https://api.developers.deliveroo.com/order/v1/orders/"+brandOrderID+"/prep_stage",
		strings.NewReader(string(jsonBody)),
	)
	if err != nil {
		return err
	}

	token, err := s.repo.GetBearerToken(ctx)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	_, err = http.DefaultClient.Do(req)
	return err
}

func (s *DeliverooService) StartDelivery(ctx context.Context, brandOrderID string) (map[string]any, error) {

	// Retrieve internal IDs
	_, err := s.repo.MarkDeliverooDeliveryStarted(ctx, brandOrderID)
	if err != nil {
		return nil, err
	}

	err = s.client.SetCollected(ctx, brandOrderID)
	if err != nil {
		return nil, err
	}

	// Success
	return map[string]any{
		"status": "1",
	}, nil
}

func (s *DeliverooService) DenyOrder(ctx context.Context, orderID string, in models.DenyOrderRequest) error {
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.client.DenyOrder(ctx, brandOrderID, in)
	if err != nil {
		return err
	}

	return nil
}

func (s *DeliverooService) FinishOrderIfDoesNotExist(ctx context.Context, brandOrderID string) {
	// Will be implemented later
}

func (s *DeliverooService) ReadyForCollection(orderID string) error {
	ctx := context.Background()

	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.client.SetReadyForCollection(ctx, brandOrderID)
	if err != nil {
		return err
	}

	return nil
}

func (s *DeliverooService) CancelOrder(ctx context.Context, userID, orderID string, in models.DenyOrderRequest) error {

	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.client.DenyOrder(ctx, brandOrderID, in)
	if err != nil {
		return err
	}

	return nil
}

// Dans deliveroo_service.go

// SetSyncStatus informe Deliveroo de l'issue du traitement du webhook
func (s *DeliverooService) SetSyncStatus(ctx context.Context, brandOrderID string, status string, reason string) error {
	notes := ""
	if status == "failed" {
		notes = "Failed to process webhook internally"
	}

	err := s.client.SendSyncStatus(ctx, brandOrderID, status, reason, notes)
	if err != nil {
		// On logue l'erreur métier ici
		logger.FromContext(ctx).Error("Failed to send sync_status to Deliveroo",
			zap.String("brand_order_id", brandOrderID),
			zap.Error(err))
		return err
	}

	return nil
}

func (s *DeliverooService) ConfirmOrder(ctx context.Context, id string) error {
	return s.client.ConfirmOrder(ctx, id)
}

// GetMenu récupère le menu du restaurant depuis l'API Deliveroo
func (s *DeliverooService) GetMenu(ctx context.Context, merchantID string) (map[string]interface{}, error) {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return s.client.GetMenu(ctx, brandID)
}

// SyncMenu pousse un menu interne vers l'API Deliveroo
func (s *DeliverooService) SyncMenu(ctx context.Context, merchantID string, menu interface{}) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}
	return s.client.SyncMenu(ctx, brandID, menu)
}

// ValidateAndSyncBrandID exécute le scénario de validation du Brand ID
// Il récupère le site_id en base, appelle Deliveroo, et met à jour le brand_id.
func (s *DeliverooService) ValidateAndSyncBrandID(ctx context.Context, merchantID string) (string, error) {
	siteID, err := s.repo.GetSiteIDByMerchant(ctx, merchantID)
	if err != nil {
		return "", fmt.Errorf("service: failed to get site_id: %w", err)
	}

	brandID, err := s.client.GetBrandIDBySiteID(ctx, siteID)
	if err != nil {
		return "", err
	}

	// Mise à jour en base avec le premier ID extrait du tableau
	err = s.repo.UpdateMerchantBrandID(ctx, merchantID, brandID)
	if err != nil {
		return "", fmt.Errorf("service: failed to store brand_id: %w", err)
	}

	return brandID, nil
}

func (s *DeliverooService) RunMenuScenario(ctx context.Context, merchantID string, menuID string) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}
	siteID, _ := s.repo.GetSiteIDByMerchant(ctx, merchantID)

	payload := map[string]any{
		"name":     "Scenario 4 - Menu with Bundles",
		"site_ids": []string{siteID},
		// Définition globale pour le validateur
		"dietary_types": []map[string]any{
			{"id": "VEGETARIAN", "name": map[string]string{"en": "Vegetarian"}},
			{"id": "VEGAN", "name": map[string]string{"en": "Vegan"}},
		},
		"menu": map[string]any{
			"mealtimes": []map[string]any{
				{
					"id":           "morning-time",
					"name":         map[string]string{"en": "Morning"},
					"description":  map[string]string{"en": "Morning Selection"},
					"image":        map[string]any{"url": "https://picsum.photos/seed/morning/800/600"},
					"category_ids": []string{"cat-1", "cat-bundle"}, // Inclus la catégorie bundle
					"schedule":     s.generateSchedule("00:00:00", "11:00:00"),
				},
				{
					"id":           "rest-of-day",
					"name":         map[string]string{"en": "Main Menu"},
					"description":  map[string]string{"en": "Full day selection"},
					"image":        map[string]any{"url": "https://picsum.photos/seed/late/800/600"},
					"category_ids": []string{"cat-1", "cat-2", "cat-bundle"},
					"schedule":     s.generateSchedule("11:00:01", "23:59:59"),
				},
			},
			"categories": []map[string]any{
				{"id": "cat-1", "name": map[string]string{"en": "Mains"}, "item_ids": []string{"item-1", "item-2"}},
				{"id": "cat-2", "name": map[string]string{"en": "Sides"}, "item_ids": []string{"item-3"}},
				{"id": "cat-bundle", "name": map[string]string{"en": "Deals"}, "item_ids": []string{"bundle-1", "bundle-2"}},
			},
			"items": []map[string]any{
				// --- ITEM 1 : Vendu seul ET dans le bundle (avec extra cost) ---
				{
					"id":               "item-1",
					"name":             map[string]string{"en": "Super Burger"},
					"operational_name": "op-burger",
					"plu":              "PLU1",
					"type":             "ITEM",
					"image": map[string]any{
						"url": "https://storage.welloresto.fr/merchants/2_brasserie_du_midi/products/default.jpg",
					},
					"price_info": map[string]any{
						"price": 1000,
						"overrides": []map[string]any{
							{"id": "bundle-1", "price": 200, "type": "ITEM"}, // OVERRIDE > 0 : OK
							{"id": "bundle-2", "price": 0, "type": "ITEM"},
						},
					},
					"tax_rate":    "20",
					"description": map[string]string{"en": "Classic burger"},
					"diets":       []string{"VEGETARIAN"},
					// IMPORTANT : Cet item est "ITEM", donc il ne doit PAS avoir mod-bundle-main dans ses modifier_ids
					"modifier_ids": []string{"mod-extra-outside"},
				},
				// --- ITEM 2 : Vendu seul ET dans le bundle ---
				{
					"id":               "item-2",
					"name":             map[string]string{"en": "Vegan Salad"},
					"operational_name": "op-salad",
					"plu":              "PLU2",
					"type":             "ITEM",
					"price_info": map[string]any{
						"price": 800,
						"overrides": []map[string]any{
							{"id": "bundle-1", "price": 0, "type": "ITEM"},
							{"id": "bundle-2", "price": 0, "type": "ITEM"},
						},
					},
					"tax_rate":    "20",
					"description": map[string]string{"en": "Fresh salad for everyone"},
					"diets":       []string{"VEGAN"},
				},
				// --- ITEM 3 : Boisson vendue seule ---
				{
					"id":               "item-3",
					"name":             map[string]string{"en": "Soda"},
					"operational_name": "op-soda",
					"plu":              "PLU3",
					"type":             "ITEM",
					"price_info": map[string]any{
						"price": 300,
						"overrides": []map[string]any{
							{"id": "bundle-1", "price": 0, "type": "ITEM"},
							{"id": "bundle-2", "price": 0, "type": "ITEM"},
						},
					},
					"tax_rate":    "20",
					"description": map[string]string{"en": "Icy cold soda"},
				},
				// --- BUNDLE 1 : Solo ---
				{
					"id":               "bundle-1",
					"name":             map[string]string{"en": "Solo Bundle"},
					"operational_name": "op-bundle-solo",
					"plu":              "B-SOLO",
					"type":             "BUNDLE", // CHANGÉ : ITEM -> BUNDLE
					"description":      map[string]string{"en": "1 Main + 1 Drink"},
					"price_info":       map[string]any{"price": 1000},
					"modifier_ids":     []string{"mod-bundle-main", "mod-bundle-drink"},
					"tax_rate":         "20",
				},
				{
					"id":               "bundle-2",
					"name":             map[string]string{"en": "Duo Party Pack"},
					"operational_name": "op-bundle-duo",
					"plu":              "B-DUO",
					"type":             "BUNDLE", // CHANGÉ : ITEM -> BUNDLE
					"description":      map[string]string{"en": "Feast for two"},
					"price_info":       map[string]any{"price": 1800},
					"party_size":       2, // PARTY SIZE : OK
					"modifier_ids":     []string{"mod-bundle-main", "mod-bundle-drink"},
					"tax_rate":         "20",
				},
				// Modificateurs de choix classiques
				{"id": "mod-choice-cheese", "name": map[string]string{"en": "Extra Cheese"}, "type": "CHOICE", "price_info": map[string]any{"price": 100}, "tax_rate": "20", "operational_name": "cheese", "plu": "M-CH", "description": map[string]string{"en": "Cheddar"}},
			},
			"modifiers": []map[string]any{
				{
					"name":          map[string]string{"en": "Select Main"},
					"description":   map[string]string{"en": "Choice of burger or salad"},
					"min_selection": 1, "max_selection": 1,
					"id":       "mod-bundle-main",
					"type":     "bundle-item", // Utilisé UNIQUEMENT par bundle-1 et bundle-2
					"item_ids": []string{"item-1", "item-2"},
				},
				{
					"name":          map[string]string{"en": "Select Drink"},
					"description":   map[string]string{"en": "Choice of cold beverage"},
					"min_selection": 1, "max_selection": 1,
					"id":       "mod-bundle-drink",
					"type":     "bundle-item", // Utilisé UNIQUEMENT par bundle-1 et bundle-2
					"item_ids": []string{"item-3"},
				},
				{
					"name":          map[string]string{"en": "Add-ons"},
					"description":   map[string]string{"en": "Customization outside bundles"},
					"min_selection": 0, "max_selection": 1,
					"id":       "mod-extra-outside",
					"type":     "add-ingredient", // Utilisé UNIQUEMENT par item-1 (qui est type ITEM)
					"item_ids": []string{"mod-choice-cheese"},
				},
			},
		},
	}

	return s.client.UploadMenu(ctx, brandID, menuID, payload)
}

func (s *DeliverooService) RunUnavailabilitiesScenario(ctx context.Context, merchantID string, menuID string) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}
	siteID, _ := s.repo.GetSiteIDByMerchant(ctx, merchantID)

	// --- ÉTAPE 1 : Orange Juice & Granola indisponibles ---
	payload1 := map[string]any{
		"item_unavailabilities": []map[string]string{
			{"item_id": "orange_juice", "status": "unavailable"},
			{"item_id": "granola", "status": "unavailable"},
		},
	}

	if err := s.client.UpdateUnavailabilities(ctx, brandID, menuID, siteID, payload1); err != nil {
		return fmt.Errorf("step 1 failed: %w", err)
	}

	// Respect du rate limit (100ms)
	time.Sleep(1000 * time.Millisecond)

	// --- ÉTAPE 2 : Orange Juice redevient dispo, Whole Milk devient indispo ---
	// Note : Granola reste "unavailable" car on ne l' उल्लेख (mentionne) pas ici !
	payload2 := map[string]any{
		"item_unavailabilities": []map[string]string{
			{"item_id": "orange_juice", "status": "available"},
			{"item_id": "whole_milk", "status": "unavailable"},
		},
	}

	return s.client.UpdateUnavailabilities(ctx, brandID, menuID, siteID, payload2)
}

// Helper pour éviter les erreurs de structure dans le planning
func (s *DeliverooService) generateFullWeekSchedule() []map[string]any {
	var schedule []map[string]any
	for i := 0; i <= 6; i++ {
		schedule = append(schedule, map[string]any{
			"day_of_week": i,
			"time_periods": []map[string]string{
				{"start": "00:00:00", "end": "23:59:00"},
			},
		})
	}
	return schedule
}

// generateSchedule est un helper pour créer un planning propre pour toute la semaine
func (s *DeliverooService) generateSchedule(start, end string) []map[string]any {
	var schedule []map[string]any
	for i := 0; i <= 6; i++ {
		schedule = append(schedule, map[string]any{
			"day_of_week": i,
			"time_periods": []map[string]string{
				{"start": start, "end": end},
			},
		})
	}
	return schedule
}

func (s *DeliverooService) RunScenario9(ctx context.Context, merchantID string, menuID string) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}
	siteID, _ := s.repo.GetSiteIDByMerchant(ctx, merchantID)

	// ÉTAPE 1 : Récupérer l'état actuel (contient déjà orange_juice et granola)
	current, err := s.client.GetUnavailabilities(ctx, brandID, menuID, siteID)
	if err != nil {
		return fmt.Errorf("failed to fetch current state: %w", err)
	}

	// ÉTAPE 2 : On prépare la mise à jour
	// On garde tout ce qu'on a reçu (immutabilité de l'état tablette)
	// et on ajoute notre "whole_milk"
	newUnavailabilities := UnavailabilitiesRequest{
		UnavailableIDs: append(current.UnavailableIDs, "whole_milk"),
		HiddenIDs:      current.HiddenIDs, // On conserve Granola qui était caché
	}

	// ÉTAPE 3 : On écrase tout avec le PUT
	log.Printf("Sending PUT update: %v", newUnavailabilities)
	return s.client.ReplaceUnavailabilities(ctx, brandID, menuID, siteID, newUnavailabilities)
}

func (s *DeliverooService) RunScenario10(ctx context.Context, merchantID string, menuID string) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}
	siteID, _ := s.repo.GetSiteIDByMerchant(ctx, merchantID)

	// On prépare des listes strictement vides
	resetPayload := UnavailabilitiesRequest{
		UnavailableIDs: []string{}, // Force le JSON []
		HiddenIDs:      []string{}, // Force le JSON []
	}

	log.Printf("Resetting all unavailabilities for site %s", siteID)

	// On réutilise la méthode Replace (PUT) créée pour le scénario 9
	return s.client.ReplaceUnavailabilities(ctx, brandID, menuID, siteID, resetPayload)
}

func (s *DeliverooService) RunScenario11(ctx context.Context, merchantID string, menuID string) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}
	siteID, _ := s.repo.GetSiteIDByMerchant(ctx, merchantID)

	// État initial demandé par le scénario
	payload := map[string]any{
		"item_unavailabilities": []map[string]any{
			{
				"item_id": "granola",
				"status":  "unavailable", // Sera réinitialisé par Deliveroo
			},
			{
				"item_id": "orange_juice",
				"status":  "hidden", // Restera caché
			},
		},
	}

	log.Printf("Setting initial stock state for Scenario 11 on site %s", siteID)
	return s.client.UpdateIndividualUnavailabilities(ctx, brandID, menuID, siteID, payload)
}

func (s *DeliverooService) UpdateIndividualUnavailabilities(ctx context.Context, brandID, menuID, siteID string, payload any) error {
	return s.client.UpdateIndividualUnavailabilities(ctx, brandID, menuID, siteID, payload)
}

func (s *DeliverooService) RunScenario12(ctx context.Context, merchantID string, menuID string) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}
	siteID, _ := s.repo.GetSiteIDByMerchant(ctx, merchantID)

	// Dans ce scénario, orange_juice est DÉJÀ indisponible (fait par le système).
	// On ajoute simplement whole_milk à la liste des indisponibles.
	payload := map[string]any{
		"item_unavailabilities": []map[string]any{
			{
				"item_id": "whole_milk",
				"status":  "unavailable",
			},
		},
	}

	log.Printf("Scenario 12: Marking whole_milk as unavailable after midnight for site %s", siteID)

	// On réutilise la méthode POST (UpdateIndividualUnavailabilities)
	return s.client.UpdateIndividualUnavailabilities(ctx, brandID, menuID, siteID, payload)
}

func (s *DeliverooService) RunScenario13(ctx context.Context, merchantID, menuID string) error {
	brandID, err := s.repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		return err
	}

	// 1. Génération du payload de 100 items
	menuPayload := s.generateScenario13Menu(menuID)

	// 2. Envoi du menu via PUT
	// On utilise l'URL officielle de l'API Menu
	url := fmt.Sprintf("https://api.developers.deliveroo.com/menu/v1/brands/%s/menus/%s", brandID, menuID)

	// On utilise ton helper doRequest via le client
	// Note: Assure-toi que ton client expose doRequest ou une méthode PutMenu
	resp, err := s.client.doRequest(ctx, "PUT", url, menuPayload)
	if err != nil {
		return fmt.Errorf("error uploading large menu: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// generateScenario13Menu crée la structure requise par Deliveroo pour ce test
func (s *DeliverooService) generateScenario13Menu(menuID string) map[string]any {
	var items []map[string]any
	var itemIDs []string

	for i := 1; i <= 100; i++ {
		id := fmt.Sprintf("item_%d", i)
		itemIDs = append(itemIDs, id)
		items = append(items, map[string]any{
			"id":   id,
			"name": map[string]string{"en": fmt.Sprintf("Test Item %d", i)},
			"price_info": map[string]any{
				"price": 1000, // 10.00€
			},
			"tax_rate": "20",
			"type":     "ITEM",
		})
	}

	return map[string]any{
		"name": "Scenario 13 Menu",
		"menu": map[string]any{
			"categories": []map[string]any{
				{
					"id":       "cat_scenario_13",
					"name":     map[string]string{"en": "Main Category"},
					"item_ids": itemIDs,
				},
			},
			"items": items,
		},
	}
}
