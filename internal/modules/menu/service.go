package menu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"welloresto-api/internal/helpers"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/deliveroo"
	"welloresto-api/internal/modules/pos/accounting"
	"welloresto-api/internal/modules/ubereats"
)

const (
	defaultExternalStatusSyncTimeout  = 8 * time.Second
	defaultExternalStatusSyncParallel = 64
)

// merchantHeaderProvider est le sous-ensemble de accounting.AccountingRepository utilisé par ce
// service (en-tête établissement pour l'affiche PDF des allergènes). Même pattern que
// order_life_cycle.merchantHeaderProvider.
type merchantHeaderProvider interface {
	GetMerchantHeader(ctx context.Context, merchantID string) (*accounting.MerchantHeader, error)
}

// allergenCatalogProvider est le sous-ensemble de allergens.Repository utilisé par ce service (le
// référentiel complet des allergènes, pour les colonnes de l'affiche PDF).
type allergenCatalogProvider interface {
	ListAllergens(ctx context.Context) ([]models.AllergenEntry, error)
}

type MenuService struct {
	legacy           *MenuRepository
	deliveroo        *deliveroo.DeliverooService
	uber             *ubereats.UberEatsService
	redis            *redisclient.Client
	merchantHeader   merchantHeaderProvider
	allergensCatalog allergenCatalogProvider

	statusSyncTimeout time.Duration
	statusSyncSem     chan struct{}
}

func NewMenuService(legacy *MenuRepository, deliverooSvc *deliveroo.DeliverooService, uberSvc *ubereats.UberEatsService, redis *redisclient.Client, merchantHeader merchantHeaderProvider, allergensCatalog allergenCatalogProvider) *MenuService {
	return &MenuService{
		legacy:            legacy,
		deliveroo:         deliverooSvc,
		uber:              uberSvc,
		redis:             redis,
		merchantHeader:    merchantHeader,
		allergensCatalog:  allergensCatalog,
		statusSyncTimeout: defaultExternalStatusSyncTimeout,
		statusSyncSem:     make(chan struct{}, defaultExternalStatusSyncParallel),
	}
}

// invalidateMenuCache purge les caches Redis dérivés du catalogue produits
// (menus scannorder/kiosk, upsell scannorder) après une mutation réussie du
// menu. Best-effort et nil-safe (client Redis absent = no-op) : n'échoue
// jamais la mutation métier qui vient d'aboutir.
func (s *MenuService) invalidateMenuCache(ctx context.Context, merchantID string) {
	s.redis.InvalidateMerchantMenuCaches(ctx, merchantID)
}

func (s *MenuService) UpdateProduct(ctx context.Context, token, productID string, updates ProductUpdatePayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	// On passe le MerchantID pour s'assurer qu'on ne modifie pas le produit d'un autre
	if err := s.legacy.UpdateProduct(ctx, user.MerchantID, productID, updates); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) GetProductImageURL(ctx context.Context, token, productID string) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	return s.legacy.GetProductImageURL(ctx, user.MerchantID, productID)
}

func (s *MenuService) UpdateProductImage(ctx context.Context, token, productID, imageURL string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateProductImage(ctx, user.MerchantID, productID, imageURL); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) GetProductCategoryImageURL(ctx context.Context, token, categoryID string) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	return s.legacy.GetProductCategoryImageURL(ctx, user.MerchantID, categoryID)
}

func (s *MenuService) UpdateProductCategoryImageURL(ctx context.Context, token, categoryID, imageURL string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateProductCategoryImageURL(ctx, user.MerchantID, categoryID, imageURL); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) ClearProductCategoryImageURL(ctx context.Context, token, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.ClearProductCategoryImageURL(ctx, user.MerchantID, categoryID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) GetMarketingCategoryImageURL(ctx context.Context, token, categoryID string) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	return s.legacy.GetMarketingCategoryImageURL(ctx, user.MerchantID, categoryID)
}

func (s *MenuService) UpdateMarketingCategoryImageURL(ctx context.Context, token, categoryID, imageURL string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateMarketingCategoryImageURL(ctx, user.MerchantID, categoryID, imageURL); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) ClearMarketingCategoryImageURL(ctx context.Context, token, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.ClearMarketingCategoryImageURL(ctx, user.MerchantID, categoryID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) GetAttributeOptionImageURL(ctx context.Context, token, optionID string) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	return s.legacy.GetAttributeOptionImageURL(ctx, user.MerchantID, optionID)
}

func (s *MenuService) UpdateAttributeOptionImageURL(ctx context.Context, token, optionID, imageURL string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateAttributeOptionImageURL(ctx, user.MerchantID, optionID, imageURL); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) UpdateProductAttributes(ctx context.Context, token, productID string, attributeIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateProductAttributes(ctx, user.MerchantID, productID, attributeIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) GetMenu(ctx context.Context, token string, lastMenu *time.Time) (*models.MenuResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetMenu(ctx, user.MerchantID, lastMenu)
}

func (s *MenuService) GetAllProducts(ctx context.Context, token string) ([]models.ProductCategory, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetAllProducts(ctx, user.MerchantID)
}

// GetAllergensPosterPDF génère l'affiche PDF listant chaque produit et ses allergènes. Réutilise
// GetAllProducts (même requête que GET /menu/products, allergènes inclus) et le référentiel
// allergens — aucune nouvelle requête SQL.
func (s *MenuService) GetAllergensPosterPDF(ctx context.Context, token string) ([]byte, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	header, err := s.merchantHeader.GetMerchantHeader(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	catalog, err := s.allergensCatalog.ListAllergens(ctx)
	if err != nil {
		return nil, err
	}

	categories, err := s.legacy.GetAllProducts(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	return buildAllergensPosterPDF(header, catalog, categories)
}

func (s *MenuService) GetAllComponents(ctx context.Context, token string) ([]models.ComponentCategory, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetAllComponents(ctx, user.MerchantID)
}

func (s *MenuService) GetMenuFromMerchantId(ctx context.Context, merchant_id string, lastMenu *time.Time) (*models.MenuResponse, error) {
	return s.legacy.GetMenu(ctx, merchant_id, lastMenu)
}

// GetMenuFromMerchantIdWithMarketing returns the menu with marketing category overrides applied.
// Use this for external platform integrations (ScanNOrder, Uber Eats, Deliveroo) only.
// Standard GET /menu must always call GetMenuFromMerchantId instead.
func (s *MenuService) GetMenuFromMerchantIdWithMarketing(ctx context.Context, merchantID string) (*models.MenuResponse, error) {
	return s.legacy.GetMenuWithMarketingCategories(ctx, merchantID)
}

func (s *MenuService) GetProductFromMerchantId(ctx context.Context, merchant_id, product_id string) (*models.ProductEntry, error) {
	return s.legacy.GetProduct(ctx, merchant_id, product_id)
}

func (s *MenuService) CreateProduct(ctx context.Context, token string, req *CreateProductPayload) (*models.ProductEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.MerchantID = user.MerchantID
	productID, err := s.legacy.CreateProduct(ctx, req)
	if err != nil {
		return nil, err
	}

	if req.MarketingCategoryID != nil && *req.MarketingCategoryID != "" {
		// Best-effort: ignore error so product creation is not rolled back if assignment fails
		_ = s.legacy.AssignProductMarketingCategory(ctx, req.MerchantID, productID, *req.MarketingCategoryID)
	}

	s.invalidateMenuCache(ctx, user.MerchantID)
	return s.legacy.GetProduct(ctx, req.MerchantID, productID)
}

func (s *MenuService) CreateComponent(ctx context.Context, token string, req *UpdateComponentPayload) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	req.MerchantID = user.MerchantID
	return s.legacy.CreateComponent(ctx, req)
}

func (s *MenuService) CreateComponentCategory(ctx context.Context, token string, req *UpsertComponentCategoryPayload) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	req.MerchantID = user.MerchantID
	categoryID, err := s.legacy.CreateComponentCategory(ctx, req)
	if err != nil {
		return "", err
	}
	// Les catégories d'ingrédients font partie du menu mis en cache : sans
	// invalidation la nouvelle catégorie n'apparaît qu'à l'expiration du cache.
	s.invalidateMenuCache(ctx, user.MerchantID)
	return categoryID, nil
}

func (s *MenuService) CreateProductCategory(ctx context.Context, token string, req *CreateProductCategoryPayload) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	req.MerchantID = user.MerchantID
	categoryID, err := s.legacy.CreateProductCategory(ctx, req)
	if err != nil {
		return "", err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return categoryID, nil
}

func (s *MenuService) GetProduct(ctx context.Context, token, product_id string) (*models.ProductEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetProduct(ctx, user.MerchantID, product_id)
}

func (s *MenuService) GetUnitsOfMeasures(ctx context.Context, token string) ([]Unit, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Appel du repo legacy ou actuel
	return s.legacy.GetUnitsOfMeasures(ctx, user.MerchantID)
}

func (s *MenuService) GetAttributes(ctx context.Context, token string) (interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetAttributes(ctx, user.MerchantID)
}

func (s *MenuService) GetAttribute(ctx context.Context, token, attributeID string) (interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetAttribute(ctx, user.MerchantID, attributeID)
}

func (s *MenuService) CreateAttribute(ctx context.Context, token string, payload *UpdateAttributePayload) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	attributeID, err := s.legacy.CreateAttribute(ctx, user.MerchantID, payload)
	if err != nil {
		return "", err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return attributeID, nil
}

func (s *MenuService) UpdateAttribute(ctx context.Context, token, attributeID string, payload *UpdateAttributePayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateAttribute(ctx, user.MerchantID, attributeID, payload); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) DeleteAttribute(ctx context.Context, token, attributeID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.DeleteAttribute(ctx, user.MerchantID, attributeID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) SetComponentStatus(ctx context.Context, token, cid, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, err
	}

	return s.legacy.SetComponentStatus(ctx, user.MerchantID, cid, status)
}

func (s *MenuService) SetProductStatus(ctx context.Context, token, pid, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, err
	}

	updated, err := s.legacy.SetProductStatus(ctx, user.MerchantID, pid, status)
	if err != nil {
		return 0, err
	}

	if updated > 0 {
		s.invalidateMenuCache(ctx, user.MerchantID)
		s.enqueueProductStatusSync(ctx, user.MerchantID, pid, status)
	}

	return updated, nil
}

func (s *MenuService) enqueueProductStatusSync(ctx context.Context, merchantID, productID, status string) {
	available, shouldSync := mapWelloStatusToAvailability(status)
	if !shouldSync {
		return
	}

	log := logger.FromContext(ctx)

	select {
	case s.statusSyncSem <- struct{}{}:
	default:
		log.Warn("[WARN] enqueueProductStatusSync dropped due to full async queue")
		return
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error(fmt.Sprintf("[ERROR] panic in product status external sync: %v", rec))
			}
			<-s.statusSyncSem
		}()

		taskCtx, cancel := context.WithTimeout(context.Background(), s.statusSyncTimeout)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			if s.uber == nil {
				log.Warn("[WARN] UberEats client not initialized, skipping status sync for UberEats")
				return
			}
			if err := s.uber.ToggleItemAvailability(taskCtx, merchantID, productID, available); err != nil {
				log.Warn("[WARN] Uber status sync failed: " + err.Error())
			}
		}()

		go func() {
			defer wg.Done()
			if s.deliveroo == nil {
				log.Warn("[WARN] Deliveroo client not initialized, skipping status sync for Deliveroo")
				return
			}
			if err := s.deliveroo.ToggleItemAvailability(taskCtx, merchantID, productID, available); err != nil {
				log.Warn("[WARN] Deliveroo status sync failed: " + err.Error())
			}
		}()

		wg.Wait()
	}()
}

func mapWelloStatusToAvailability(status string) (bool, bool) {
	v := strings.ToLower(strings.TrimSpace(status))

	switch v {
	case "1", "true", "available":
		return true, true
	// "not_available" est la valeur effectivement envoyée par le toggle POS
	// (menu_api.dart) — son absence ici sautait silencieusement la sync
	// Uber Eats/Deliveroo quand un produit était désactivé depuis le POS.
	case "0", "false", "out_of_stock", "unavailable", "not_available":
		return false, true
	default:
		return false, false
	}
}

func (s *MenuService) SetProductCategoryAvailability(ctx context.Context, token, categoryID, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, err
	}

	updated, err := s.legacy.SetProductCategoryAvailability(ctx, user.MerchantID, categoryID, status)
	if err != nil {
		return 0, err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return updated, nil
}

func (s *MenuService) SetProductAvailability(ctx context.Context, token, productID, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, err
	}

	updated, err := s.legacy.SetProductAvailability(ctx, user.MerchantID, productID, status)
	if err != nil {
		return 0, err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return updated, nil
}

func (s *MenuService) UpdateProductCategory(ctx context.Context, token, categoryID string, payload UpsertComponentCategoryPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateProductCategory(ctx, user.MerchantID, categoryID, payload.Name); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) BulkAssignProductsToCategory(ctx context.Context, token, categoryID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.BulkAssignProductsToCategory(ctx, user.MerchantID, categoryID, productIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) UpdateComponent(ctx context.Context, token, componentID string, updates *UpdateComponentPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UpdateComponent(ctx, user.MerchantID, componentID, updates)
}

func (s *MenuService) GetComponent(ctx context.Context, token, componentID string) (*models.ComponentBasic, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetComponent(ctx, user.MerchantID, componentID)
}

func (s *MenuService) DeleteProductCategory(ctx context.Context, token, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.DeleteProductCategory(ctx, user.MerchantID, categoryID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) DeleteComponent(ctx context.Context, token, componentID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.DeleteComponent(ctx, user.MerchantID, componentID)
}

// Modes de suppression d'une catégorie d'ingrédients non vide.
const (
	DeleteComponentCategoryModeReassign = "reassign"
	DeleteComponentCategoryModePurge    = "purge"
)

// ErrComponentCategoryNotEmpty est renvoyé quand la catégorie contient encore
// des ingrédients et qu'aucun mode n'a été choisi. Le refus est délibéré :
// l'ancien comportement désactivait la catégorie en laissant les ingrédients
// pointer dessus, ce qui les rendait invisibles dans toute l'application.
var ErrComponentCategoryNotEmpty = errors.New("component_category_not_empty")

// DeleteComponentCategory supprime une catégorie d'ingrédients.
// mode == "reassign" : les ingrédients sont déplacés vers reassignTo.
// mode == "purge"    : les ingrédients sont désactivés avec la catégorie.
// Une catégorie vide se supprime sans mode.
func (s *MenuService) DeleteComponentCategory(ctx context.Context, token, categoryID, mode, reassignTo string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	exists, err := s.legacy.ComponentCategoryExists(ctx, user.MerchantID, categoryID)
	if err != nil {
		return err
	}
	if !exists {
		return models.ErrNotFound
	}

	count, err := s.legacy.CountComponentsInCategory(ctx, user.MerchantID, categoryID)
	if err != nil {
		return err
	}

	// Une catégorie vide n'a rien à réaffecter : le mode est ignoré.
	target := ""
	if count > 0 {
		switch mode {
		case DeleteComponentCategoryModeReassign:
			reassignTo = strings.TrimSpace(reassignTo)
			if reassignTo == "" || reassignTo == categoryID {
				return fmt.Errorf("%w: reassign_to", models.ErrInvalidInput)
			}
			targetExists, err := s.legacy.ComponentCategoryExists(ctx, user.MerchantID, reassignTo)
			if err != nil {
				return err
			}
			if !targetExists {
				return fmt.Errorf("%w: reassign_to", models.ErrInvalidInput)
			}
			target = reassignTo
		case DeleteComponentCategoryModePurge:
			// target reste vide : les ingrédients sont désactivés
		default:
			return ErrComponentCategoryNotEmpty
		}
	}

	if err := s.legacy.DeleteComponentCategory(ctx, user.MerchantID, categoryID, target); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// UpdateComponentCategory renomme une catégorie d'ingrédients.
func (s *MenuService) UpdateComponentCategory(ctx context.Context, token, categoryID string, req UpdateComponentCategoryPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	exists, err := s.legacy.ComponentCategoryExists(ctx, user.MerchantID, categoryID)
	if err != nil {
		return err
	}
	if !exists {
		return models.ErrNotFound
	}

	if err := s.legacy.UpdateComponentCategory(ctx, user.MerchantID, categoryID, req); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// UpdateComponentCategoriesDisplayOrder réordonne les catégories d'ingrédients.
func (s *MenuService) UpdateComponentCategoriesDisplayOrder(ctx context.Context, token string, categoryIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateComponentCategoriesDisplayOrder(ctx, user.MerchantID, categoryIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) DeleteProduct(ctx context.Context, token, productID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.DeleteProduct(ctx, user.MerchantID, productID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) UpdateDisplayOrder(ctx context.Context, token string, payload DisplayOrderPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateDisplayOrder(ctx, user.MerchantID, payload); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// GetDeliverooMenu récupère le menu du restaurant depuis l'API Deliveroo
func (s *MenuService) GetDeliverooMenu(ctx context.Context, token string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.deliveroo.GetMenu(ctx, user.MerchantID)
}

// SyncDeliverooMenu synchronise le menu interne vers l'API Deliveroo
func (s *MenuService) SyncDeliverooMenu(ctx context.Context, token string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	internalMenu, err := s.legacy.GetMenuWithMarketingCategories(ctx, user.MerchantID)
	if err != nil {
		return err
	}

	deliverooMenu, err := ToDeliverooFormat(internalMenu)
	if err != nil {
		return fmt.Errorf("menu sync deliveroo: mapping failed: %w", err)
	}

	return s.deliveroo.SyncMenu(ctx, user.MerchantID, deliverooMenu)
}

// GetUberEatsMenu récupère le menu du restaurant depuis l'API Uber Eats
func (s *MenuService) GetUberEatsMenu(ctx context.Context, token string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.uber.GetMenu(ctx, user.MerchantID)
}

// SyncUberEatsMenu synchronise le menu interne vers l'API Uber Eats
func (s *MenuService) SyncUberEatsMenu(ctx context.Context, token string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	internalMenu, err := s.legacy.GetMenuWithMarketingCategories(ctx, user.MerchantID)
	if err != nil {
		return err
	}

	uberMenu, err := ToUberEatsFormat(internalMenu)
	if err != nil {
		return fmt.Errorf("menu sync ubereats: mapping failed: %w", err)
	}

	return s.uber.SyncMenu(ctx, user.MerchantID, uberMenu)
}

func (s *MenuService) CreateProductFromExternal(ctx context.Context, merchantID, title, description string, amount int) (*string, error) {
	// ⚠️ Plus besoin d'ouvrir une nouvelle transaction ici, on utilise `tx` fourni en paramètre !
	productID, err := s.legacy.CreateExternalProductTx(ctx, merchantID, title, description, amount)
	if err != nil {
		return nil, err
	}

	s.invalidateMenuCache(ctx, merchantID)
	return helpers.Int64ToStringPtr(productID), nil
}

// SyncProductAllergens replaces all allergen associations for the given product.
// Only the merchant that owns the product may call this.
func (s *MenuService) SyncProductAllergens(ctx context.Context, token, productID string, allergenIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.SyncProductAllergens(ctx, user.MerchantID, productID, allergenIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// BulkAssignTag adds a tag to many products without removing their other tags.
func (s *MenuService) BulkAssignTag(ctx context.Context, token, tagID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.BulkAssignTag(ctx, user.MerchantID, tagID, productIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// BulkAssignProductsToTag replaces all product-tag links for a given tag.
// Removes all existing links from this tag to any product, then adds new links to the provided product IDs.
func (s *MenuService) BulkAssignProductsToTag(ctx context.Context, token, tagID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.BulkAssignProductsToTag(ctx, user.MerchantID, tagID, productIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// BulkAssignAllergen adds an allergen to many products without removing their other allergens.
func (s *MenuService) BulkAssignAllergen(ctx context.Context, token, allergenID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.BulkAssignAllergen(ctx, user.MerchantID, allergenID, productIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// SyncProductTags replaces all tag associations for the given product.
// Only the merchant that owns the product (and the tags) may call this.
func (s *MenuService) SyncProductTags(ctx context.Context, token, productID string, tagIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.SyncProductTags(ctx, user.MerchantID, productID, tagIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

// ListTags returns all tags for the authenticated merchant.
func (s *MenuService) ListTags(ctx context.Context, token string) ([]models.TagEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.ListTags(ctx, user.MerchantID)
}

// BulkUpdateProductPrices updates prices for multiple products in a single operation
func (s *MenuService) BulkUpdateProductPrices(ctx context.Context, token string, products []BulkUpdateProductPrice) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.BulkUpdateProductPrices(ctx, user.MerchantID, products); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) GetMarketingCategories(ctx context.Context, token string) ([]MarketingCategoryEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetMarketingCategories(ctx, user.MerchantID)
}

func (s *MenuService) CreateMarketingCategory(ctx context.Context, token string, req *CreateMarketingCategoryPayload) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	categoryID, err := s.legacy.CreateMarketingCategory(ctx, user.MerchantID, req.Name)
	if err != nil {
		return "", err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return categoryID, nil
}

func (s *MenuService) UpdateMarketingCategory(ctx context.Context, token, categoryID string, req UpdateMarketingCategoryPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateMarketingCategory(ctx, user.MerchantID, categoryID, req); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) DeleteMarketingCategory(ctx context.Context, token, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.DeleteMarketingCategory(ctx, user.MerchantID, categoryID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) UpdateMarketingCategoriesDisplayOrder(ctx context.Context, token string, categoryIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UpdateMarketingCategoriesDisplayOrder(ctx, user.MerchantID, categoryIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) AssignProductMarketingCategory(ctx context.Context, token, productID, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.AssignProductMarketingCategory(ctx, user.MerchantID, productID, categoryID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) UnassignProductMarketingCategory(ctx context.Context, token, productID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.UnassignProductMarketingCategory(ctx, user.MerchantID, productID); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}

func (s *MenuService) BulkAssignProductsToMarketingCategory(ctx context.Context, token, categoryID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.BulkAssignProductsToMarketingCategory(ctx, user.MerchantID, categoryID, productIDs); err != nil {
		return err
	}
	s.invalidateMenuCache(ctx, user.MerchantID)
	return nil
}
