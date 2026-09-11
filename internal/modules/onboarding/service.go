package onboarding

import (
	"context"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetOnboarding returns merchantID's onboarding tasks, scoped to the
// caller's own merchant — every authenticated request in this API is scoped
// to the merchant carried by its token (CLAUDE.md), and this route is no
// exception: {id} in the URL must match the caller's own merchant.
func (s *Service) GetOnboarding(ctx context.Context, merchantID string) ([]Task, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return nil, models.ErrMissingResourceID
	}
	if currentUser.MerchantID != merchantID {
		return nil, models.ErrForbidden
	}
	return s.repo.ListByMerchant(ctx, merchantID)
}
