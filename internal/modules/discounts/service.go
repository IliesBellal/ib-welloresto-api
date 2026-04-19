package discounts

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

// GetActiveDiscounts retrieves active discounts for the authenticated merchant
func (s *Service) GetActiveDiscounts(ctx context.Context, token string) ([]Discount, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetActiveDiscounts(ctx, user.MerchantID)
}

// GetAllDiscounts retrieves all non-deleted discounts for the authenticated merchant
func (s *Service) GetAllDiscounts(ctx context.Context, token string) ([]Discount, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetAllDiscounts(ctx, user.MerchantID)
}

// GetDiscountByID retrieves a discount by ID
func (s *Service) GetDiscountByID(ctx context.Context, token string, discountID string) (*Discount, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetDiscountByID(ctx, user.MerchantID, discountID)
}

// CreateDiscount creates a new discount
func (s *Service) CreateDiscount(ctx context.Context, token string, req *CreateDiscountRequest) (*Discount, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Validate input
	if err := req.Validate(); err != nil {
		return nil, err
	}

	req.DiscountID = helpers.GeneratePrefixedID("discount")

	return s.repo.CreateDiscount(ctx, user.MerchantID, req)
}

// UpdateDiscount updates an existing discount
func (s *Service) UpdateDiscount(ctx context.Context, token string, discountID string, req *UpdateDiscountRequest) (*Discount, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.UpdateDiscount(ctx, user.MerchantID, discountID, req)
}

// DeleteDiscount deletes a discount (soft delete)
func (s *Service) DeleteDiscount(ctx context.Context, token string, discountID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.repo.DeleteDiscount(ctx, user.MerchantID, discountID)
}
