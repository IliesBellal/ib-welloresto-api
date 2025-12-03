package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type LocationsService struct {
	locationsRepo *repositories.LocationsRepository
	userRepo      *repositories.UsersRepository // used to resolve token -> merchant id
}

func NewLocationsService(locationsRepo *repositories.LocationsRepository, userRepo *repositories.UsersRepository) *LocationsService {
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

func (s *LocationsService) UpdateLocationCoordinates(ctx context.Context, token, locationID string, x float64, y float64) (map[string]interface{}, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	err = s.locationsRepo.UpdateLocationCoordinates(ctx, user.MerchantID, locationID, x, y)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}
