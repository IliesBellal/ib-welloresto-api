package locations

import (
	"context"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type LocationsService struct {
	locationsRepo *LocationsRepository
	userRepo      auth.AuthService
}

func NewLocationsService(locationsRepo *LocationsRepository, userRepo auth.AuthService) *LocationsService {
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
		return nil, models.ErrUnauthorized
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
		return nil, models.ErrUnauthorized
	}

	err = s.locationsRepo.UpdateLocationCoordinates(ctx, user.MerchantID, locationID, x, y)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}
