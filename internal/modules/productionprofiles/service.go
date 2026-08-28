package productionprofiles

import (
	"context"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// ListProfiles returns every production profile for the authenticated merchant.
func (s *Service) ListProfiles(ctx context.Context, token string) ([]ProductionProfileEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListProfiles(ctx, user.MerchantID)
}

// GetProfile returns a single profile, with its product association matrix,
// owned by the authenticated merchant.
func (s *Service) GetProfile(ctx context.Context, token, profileID string) (*ProductionProfileDetail, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.GetProfile(ctx, user.MerchantID, profileID)
}

// CreateProfile validates, generates an ID, and persists a new profile.
func (s *Service) CreateProfile(ctx context.Context, token string, req *CreateProductionProfileRequest) (*ProductionProfileEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	profileID := helpers.GeneratePrefixedID(helpers.ProductionProfileIDPrefix)
	return s.repo.CreateProfile(ctx, user.MerchantID, profileID, req)
}

// UpdateProfile validates and renames a merchant-owned profile.
func (s *Service) UpdateProfile(ctx context.Context, token, profileID string, req *UpdateProductionProfileRequest) (*ProductionProfileEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	return s.repo.UpdateProfile(ctx, user.MerchantID, profileID, req)
}

// DeleteProfile removes a profile owned by the authenticated merchant, along
// with its product associations.
func (s *Service) DeleteProfile(ctx context.Context, token, profileID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	return s.repo.DeleteProfile(ctx, user.MerchantID, profileID)
}

// ReplaceProducts validates and fully replaces the product association
// matrix of a merchant-owned profile.
func (s *Service) ReplaceProducts(ctx context.Context, token, profileID string, items ReplaceProductsRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := items.Validate(); err != nil {
		return err
	}

	return s.repo.ReplaceProducts(ctx, user.MerchantID, profileID, items)
}
