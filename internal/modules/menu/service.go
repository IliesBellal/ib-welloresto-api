package menu

import (
	"context"
	"database/sql"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type MenuService struct {
	userRepo auth.AuthService // uses your existing interface
	legacy   *MenuRepository
}

func NewMenuService(legacy *MenuRepository, userRepo auth.AuthService) *MenuService {
	return &MenuService{
		userRepo: userRepo,
		legacy:   legacy,
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

func (s *MenuService) CreateProductFromExternal(ctx context.Context, merchantID, title, description string, amount int) (*string, error) {
	tx, err := s.legacy.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	productID, err := s.legacy.CreateExternalProductTx(ctx, tx, merchantID, title, description, amount)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	// Tu peux adapter le retour selon ton modèle API
	return helpers.Int64ToStringPtr(productID), nil
}
