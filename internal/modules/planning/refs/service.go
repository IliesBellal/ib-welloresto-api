package refs

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

func (s *Service) ListContractTypes(ctx context.Context) ([]SystemRef, error) {
	if _, err := middleware.UserFromContext(ctx); err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListContractTypes(ctx)
}

func (s *Service) ListAttendanceSources(ctx context.Context) ([]SystemRef, error) {
	if _, err := middleware.UserFromContext(ctx); err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListAttendanceSources(ctx)
}

func (s *Service) ListPlanningEventTypes(ctx context.Context) ([]SystemRef, error) {
	if _, err := middleware.UserFromContext(ctx); err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListPlanningEventTypes(ctx)
}
