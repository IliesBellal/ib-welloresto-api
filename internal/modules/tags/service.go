package tags

import (
	"context"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// ListTags returns all tags for the authenticated merchant.
func (s *Service) ListTags(ctx context.Context, token string) ([]models.TagEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.ListTags(ctx, user.MerchantID)
}

// CreateTag creates a new tag for the authenticated merchant.
func (s *Service) CreateTag(ctx context.Context, token string, req *CreateTagRequest) (*models.TagEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Validate input
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Generate ID
	tagID := helpers.GeneratePrefixedID("tag")
	req.ID = &tagID
	if req.Color == nil {
		defaultColor := "#FFFFFF"
		req.Color = &defaultColor
	}

	// Create tag
	return s.repo.CreateTag(ctx, user.MerchantID, req)
}

// DeleteTag deletes a tag owned by the authenticated merchant.
func (s *Service) DeleteTag(ctx context.Context, token string, tagID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	// Delete tag (checks ownership inside)
	return s.repo.DeleteTag(ctx, user.MerchantID, tagID)
}

// UpdateTagsDisplayOrder updates the display order of tags for the authenticated merchant.
func (s *Service) UpdateTagsDisplayOrder(ctx context.Context, token string, req *UpdateTagsDisplayOrderRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	// Validate input
	if err := req.Validate(); err != nil {
		return err
	}

	// Update display order
	return s.repo.UpdateTagsDisplayOrder(ctx, user.MerchantID, req.Tags)
}
