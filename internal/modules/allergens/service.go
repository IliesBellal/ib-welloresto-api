package allergens

import (
	"context"
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

// ListAllergens validates the token then returns all system allergens.
func (s *Service) ListAllergens(ctx context.Context, token string) ([]models.AllergenEntry, error) {
	user, err := s.authSvc.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListAllergens(ctx)
}
