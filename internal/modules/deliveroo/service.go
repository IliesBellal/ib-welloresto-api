package deliveroo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
