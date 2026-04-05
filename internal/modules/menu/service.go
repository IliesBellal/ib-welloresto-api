package menu

import (
	"context"
	"fmt"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/deliveroo"
	"welloresto-api/internal/modules/ubereats"
)

type MenuService struct {
	legacy    *MenuRepository
	deliveroo *deliveroo.DeliverooService
	uber      *ubereats.UberEatsService
}

func NewMenuService(legacy *MenuRepository, deliverooSvc *deliveroo.DeliverooService, uberSvc *ubereats.UberEatsService) *MenuService {
	return &MenuService{
		legacy:    legacy,
		deliveroo: deliverooSvc,
		uber:      uberSvc,
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

	return s.legacy.GetProduct(ctx, req.MerchantID, productID)
}

func (s *MenuService) CreateComponent(ctx context.Context, token string, req *CreateComponentPayload) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", err
	}

	req.MerchantID = user.MerchantID
	return s.legacy.CreateComponent(ctx, req)
}

func (s *MenuService) CreateComponentCategory(ctx context.Context, token string, req *CreateComponentCategoryPayload) (string, error) {
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

	return s.legacy.SetProductStatus(ctx, user.MerchantID, pid, status)
}

func (s *MenuService) SetProductCategoryAvailability(ctx context.Context, token, categoryID, status string) (int64, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return 0, err
	}

	return s.legacy.SetProductCategoryAvailability(ctx, user.MerchantID, categoryID, status)
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

func (s *MenuService) DeleteProduct(ctx context.Context, token, productID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.legacy.DeleteProduct(ctx, user.MerchantID, productID)
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

	internalMenu, err := s.legacy.GetMenu(ctx, user.MerchantID, nil)
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

	internalMenu, err := s.legacy.GetMenu(ctx, user.MerchantID, nil)
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
