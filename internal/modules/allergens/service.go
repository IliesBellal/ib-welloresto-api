package allergens

import (
	"context"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// ListAllergens validates the token then returns all system allergens.
func (s *Service) ListAllergens(ctx context.Context, token string) ([]models.AllergenEntry, error) {
	_, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.ListAllergens(ctx)
}
