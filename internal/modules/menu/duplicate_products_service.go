package menu

import (
	"context"
	"welloresto-api/internal/middleware"
)

// BulkDuplicateProducts copie intégralement des produits dans une catégorie
// caisse (cf. MenuRepository.DuplicateProducts). Les copies n'ont pas encore
// d'image : voir SetProductImages.
func (s *MenuService) BulkDuplicateProducts(ctx context.Context, token, categoryID, status string, productIDs []string) ([]DuplicatedProduct, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	copies, err := s.legacy.DuplicateProducts(ctx, user.MerchantID, categoryID, status, productIDs)
	if err != nil {
		return nil, err
	}

	if len(copies) > 0 {
		s.onMenuChanged(ctx, user.MerchantID)
	}
	return copies, nil
}

// SetProductImages attache leur image à plusieurs produits en une fois.
func (s *MenuService) SetProductImages(ctx context.Context, token string, images map[string]string) error {
	if len(images) == 0 {
		return nil
	}
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if err := s.legacy.SetProductImages(ctx, user.MerchantID, images); err != nil {
		return err
	}
	s.onMenuChanged(ctx, user.MerchantID)
	return nil
}
