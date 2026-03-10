package menu

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/deliveroo"
	"welloresto-api/internal/modules/ubereats"
)

type MenuService struct {
	userRepo  auth.AuthService // uses your existing interface
	legacy    *MenuRepository
	deliveroo *deliveroo.DeliverooService
	uber      *ubereats.UberEatsService
}

func NewMenuService(legacy *MenuRepository, userRepo auth.AuthService, deliverooSvc *deliveroo.DeliverooService, uberSvc *ubereats.UberEatsService) *MenuService {
	return &MenuService{
		userRepo:  userRepo,
		legacy:    legacy,
		deliveroo: deliverooSvc,
		uber:      uberSvc,
	}
}

func (s *MenuService) UpdateProduct(ctx context.Context, token, productID string, updates ProductUpdatePayload) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	// On passe le MerchantID pour s'assurer qu'on ne modifie pas le produit d'un autre
	return s.legacy.UpdateProduct(ctx, user.MerchantID, productID, updates)
}

func (s *MenuService) UpdateProductAttributes(ctx context.Context, token, productID string, attributeIDs []string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	return s.legacy.UpdateProductAttributes(ctx, user.MerchantID, productID, attributeIDs)
}

func (s *MenuService) GetMenu(ctx context.Context, token string, lastMenu *time.Time) (*models.MenuResponse, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.legacy.GetMenu(ctx, user.MerchantID, lastMenu)
}

func (s *MenuService) GetMenuFromMerchantId(ctx context.Context, merchant_id string, lastMenu *time.Time) (*models.MenuResponse, error) {
	return s.legacy.GetMenu(ctx, merchant_id, lastMenu)
}

func (s *MenuService) CreateProduct(ctx context.Context, token string, req *CreateProductPayload) (*ProductEntry, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	req.MerchantID = user.MerchantID
	productID, err := s.legacy.CreateProduct(ctx, req)
	if err != nil {
		return nil, err
	}

	return s.legacy.GetProduct(ctx, req.MerchantID, productID)
}

func (s *MenuService) GetProduct(ctx context.Context, token, product_id string) (*ProductEntry, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.legacy.GetProduct(ctx, user.MerchantID, product_id)
}

func (s *MenuService) GetUnitsOfMeasures(ctx context.Context, token string) (interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.legacy.GetUnitsOfMeasures(ctx, user.MerchantID)
}

func (s *MenuService) GetAttributes(ctx context.Context, token string) (interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.legacy.GetAttributes(ctx, user.MerchantID)
}

func (s *MenuService) SetComponentAvailability(ctx context.Context, token, cid, status string) (int64, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return 0, err
	}
	if user == nil {
		return 0, models.ErrUnauthorized
	}

	return s.legacy.SetComponentAvailability(ctx, user.MerchantID, cid, status)
}

func (s *MenuService) SetProductAvailability(ctx context.Context, token, pid, status string) (int64, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return 0, err
	}
	if user == nil {
		return 0, models.ErrUnauthorized
	}

	return s.legacy.SetProductAvailability(ctx, user.MerchantID, pid, status)
}

// GetDeliverooMenu récupère le menu du restaurant depuis l'API Deliveroo
func (s *MenuService) GetDeliverooMenu(ctx context.Context, token string) (map[string]interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}
	return s.deliveroo.GetMenu(ctx, user.MerchantID)
}

// SyncDeliverooMenu synchronise le menu interne vers l'API Deliveroo
func (s *MenuService) SyncDeliverooMenu(ctx context.Context, token string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
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
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}
	return s.uber.GetMenu(ctx, user.MerchantID)
}

// SyncUberEatsMenu synchronise le menu interne vers l'API Uber Eats
func (s *MenuService) SyncUberEatsMenu(ctx context.Context, token string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
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

func (s *MenuService) CreateProductFromExternal(ctx context.Context, tx *sql.Tx, merchantID, title, description string, amount int) (*string, error) {
	// ⚠️ Plus besoin d'ouvrir une nouvelle transaction ici, on utilise `tx` fourni en paramètre !
	productID, err := s.legacy.CreateExternalProductTx(ctx, tx, merchantID, title, description, amount)
	if err != nil {
		return nil, err
	}

	return helpers.Int64ToStringPtr(productID), nil
}

// SyncProductAllergens replaces all allergen associations for the given product.
// Only the merchant that owns the product may call this.
func (s *MenuService) SyncProductAllergens(ctx context.Context, token, productID string, allergenIDs []int) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}
	return s.legacy.SyncProductAllergens(ctx, user.MerchantID, productID, allergenIDs)
}

// BulkAssignTag adds a tag to many products without removing their other tags.
func (s *MenuService) BulkAssignTag(ctx context.Context, token, tagID string, productIDs []string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}
	return s.legacy.BulkAssignTag(ctx, user.MerchantID, tagID, productIDs)
}

// BulkAssignAllergen adds an allergen to many products without removing their other allergens.
func (s *MenuService) BulkAssignAllergen(ctx context.Context, token, allergenID string, productIDs []string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}
	return s.legacy.BulkAssignAllergen(ctx, user.MerchantID, allergenID, productIDs)
}

// SyncProductTags replaces all tag associations for the given product.
// Only the merchant that owns the product (and the tags) may call this.
func (s *MenuService) SyncProductTags(ctx context.Context, token, productID string, tagIDs []int) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}
	return s.legacy.SyncProductTags(ctx, user.MerchantID, productID, tagIDs)
}

// ListTags returns all tags for the authenticated merchant.
func (s *MenuService) ListTags(ctx context.Context, token string) ([]models.TagEntry, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}
	return s.legacy.ListTags(ctx, user.MerchantID)
}
