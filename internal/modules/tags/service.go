package tags

import (
	"context"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type Service struct {
	repo    *Repository
	authSvc auth.AuthService
}

func NewService(repo *Repository, authSvc auth.AuthService) *Service {
	return &Service{repo: repo, authSvc: authSvc}
}

// ListTags returns all tags for the authenticated merchant.
func (s *Service) ListTags(ctx context.Context, token string) ([]models.TagEntry, error) {
	user, err := s.authSvc.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListTags(ctx, user.MerchantID)
}

// CreateTag creates a new tag for the authenticated merchant.
func (s *Service) CreateTag(ctx context.Context, token string, req *CreateTagRequest) (*models.TagEntry, error) {
	// Validate input
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Auth
	user, err := s.authSvc.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	// Generate ID
	tagID, err := helpers.GeneratePrefixedID("tag")
	if err != nil {
		return nil, err
	}

	// Create tag
	return s.repo.CreateTag(ctx, user.MerchantID, tagID, req.Name)
}

// DeleteTag deletes a tag owned by the authenticated merchant.
func (s *Service) DeleteTag(ctx context.Context, token string, tagID string) error {
	// Auth
	user, err := s.authSvc.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	// Delete tag (checks ownership inside)
	return s.repo.DeleteTag(ctx, user.MerchantID, tagID)
}
