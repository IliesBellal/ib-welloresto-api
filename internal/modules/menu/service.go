package menu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/deliveroo"
	"welloresto-api/internal/modules/ubereats"
)

const (
	defaultExternalStatusSyncTimeout  = 8 * time.Second
	defaultExternalStatusSyncParallel = 64
)

type MenuService struct {
	legacy    *MenuRepository
	deliveroo *deliveroo.DeliverooService
	uber      *ubereats.UberEatsService

	statusSyncTimeout time.Duration
	statusSyncSem     chan struct{}
}

func NewMenuService(legacy *MenuRepository, deliverooSvc *deliveroo.DeliverooService, uberSvc *ubereats.UberEatsService) *MenuService {
	return &MenuService{
		legacy:            legacy,
		deliveroo:         deliverooSvc,
		uber:              uberSvc,
		statusSyncTimeout: defaultExternalStatusSyncTimeout,
		statusSyncSem:     make(chan struct{}, defaultExternalStatusSyncParallel),
	}
}

func (s *MenuService) UpdateProduct(ctx context.Context, token, productID string, updates ProductUpdatePayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	// On passe le MerchantID pour s'assurer qu'on ne modifie pas le produit d'un autre
	return s.legacy.UpdateProduct(ctx, user.MerchantID, productID, updates)
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

	return s.legacy.UpdateProductImage(ctx, user.MerchantID, productID, imageURL)
}

func (s *MenuService) UpdateProductAttributes(ctx context.Context, token, productID string, attributeIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UpdateProductAttributes(ctx, user.MerchantID, productID, attributeIDs)
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
	return s.legacy.CreateComponentCategory(ctx, req)
}

func (s *MenuService) CreateProductCategory(ctx context.Context, token string, req *CreateProductCategoryPayload) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	req.MerchantID = user.MerchantID
	return s.legacy.CreateProductCategory(ctx, req)
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

	return s.legacy.CreateAttribute(ctx, user.MerchantID, payload)
}

func (s *MenuService) UpdateAttribute(ctx context.Context, token, attributeID string, payload *UpdateAttributePayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UpdateAttribute(ctx, user.MerchantID, attributeID, payload)
}

func (s *MenuService) DeleteAttribute(ctx context.Context, token, attributeID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.DeleteAttribute(ctx, user.MerchantID, attributeID)
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
	case "0", "false", "out_of_stock", "unavailable":
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

	return s.legacy.SetProductCategoryAvailability(ctx, user.MerchantID, categoryID, status)
}

func (s *MenuService) SetProductAvailability(ctx context.Context, token, productID, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, err
	}

	return s.legacy.SetProductAvailability(ctx, user.MerchantID, productID, status)
}

func (s *MenuService) UpdateProductCategory(ctx context.Context, token, categoryID string, payload UpsertComponentCategoryPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UpdateProductCategory(ctx, user.MerchantID, categoryID, payload.Name)
}

func (s *MenuService) BulkAssignProductsToCategory(ctx context.Context, token, categoryID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.BulkAssignProductsToCategory(ctx, user.MerchantID, categoryID, productIDs)
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

	return s.legacy.DeleteProductCategory(ctx, user.MerchantID, categoryID)
}

func (s *MenuService) DeleteComponent(ctx context.Context, token, componentID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.DeleteComponent(ctx, user.MerchantID, componentID)
}

func (s *MenuService) DeleteComponentCategory(ctx context.Context, token, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.DeleteComponentCategory(ctx, user.MerchantID, categoryID)
}

func (s *MenuService) DeleteProduct(ctx context.Context, token, productID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.DeleteProduct(ctx, user.MerchantID, productID)
}

func (s *MenuService) UpdateDisplayOrder(ctx context.Context, token string, payload DisplayOrderPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UpdateDisplayOrder(ctx, user.MerchantID, payload)
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

	return helpers.Int64ToStringPtr(productID), nil
}

// SyncProductAllergens replaces all allergen associations for the given product.
// Only the merchant that owns the product may call this.
func (s *MenuService) SyncProductAllergens(ctx context.Context, token, productID string, allergenIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.SyncProductAllergens(ctx, user.MerchantID, productID, allergenIDs)
}

// BulkAssignTag adds a tag to many products without removing their other tags.
func (s *MenuService) BulkAssignTag(ctx context.Context, token, tagID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.BulkAssignTag(ctx, user.MerchantID, tagID, productIDs)
}

// BulkAssignProductsToTag replaces all product-tag links for a given tag.
// Removes all existing links from this tag to any product, then adds new links to the provided product IDs.
func (s *MenuService) BulkAssignProductsToTag(ctx context.Context, token, tagID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.BulkAssignProductsToTag(ctx, user.MerchantID, tagID, productIDs)
}

// BulkAssignAllergen adds an allergen to many products without removing their other allergens.
func (s *MenuService) BulkAssignAllergen(ctx context.Context, token, allergenID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.BulkAssignAllergen(ctx, user.MerchantID, allergenID, productIDs)
}

// SyncProductTags replaces all tag associations for the given product.
// Only the merchant that owns the product (and the tags) may call this.
func (s *MenuService) SyncProductTags(ctx context.Context, token, productID string, tagIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.SyncProductTags(ctx, user.MerchantID, productID, tagIDs)
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

	return s.legacy.BulkUpdateProductPrices(ctx, user.MerchantID, products)
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

	return s.legacy.CreateMarketingCategory(ctx, user.MerchantID, req.Name)
}

func (s *MenuService) UpdateMarketingCategory(ctx context.Context, token, categoryID string, req UpdateMarketingCategoryPayload) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UpdateMarketingCategory(ctx, user.MerchantID, categoryID, req)
}

func (s *MenuService) DeleteMarketingCategory(ctx context.Context, token, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.DeleteMarketingCategory(ctx, user.MerchantID, categoryID)
}

func (s *MenuService) UpdateMarketingCategoriesDisplayOrder(ctx context.Context, token string, categoryIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UpdateMarketingCategoriesDisplayOrder(ctx, user.MerchantID, categoryIDs)
}

func (s *MenuService) AssignProductMarketingCategory(ctx context.Context, token, productID, categoryID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.AssignProductMarketingCategory(ctx, user.MerchantID, productID, categoryID)
}

func (s *MenuService) UnassignProductMarketingCategory(ctx context.Context, token, productID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.UnassignProductMarketingCategory(ctx, user.MerchantID, productID)
}

func (s *MenuService) BulkAssignProductsToMarketingCategory(ctx context.Context, token, categoryID string, productIDs []string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.BulkAssignProductsToMarketingCategory(ctx, user.MerchantID, categoryID, productIDs)
}
