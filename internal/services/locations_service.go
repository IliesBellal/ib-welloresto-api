package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type LocationsService struct {
	locationsRepo *repositories.LocationsRepository
	userRepo      *repositories.UserRepository // used to resolve token -> merchant id
}

func NewLocationsService(locationsRepo *repositories.LocationsRepository, userRepo *repositories.UserRepository) *LocationsService {
	return &LocationsService{
		locationsRepo: locationsRepo,
		userRepo:      userRepo,
	}
}

func (s *LocationsService) GetLocations(ctx context.Context, token string) ([]models.Location, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.locationsRepo.GetLocations(ctx, user.MerchantID)
}
