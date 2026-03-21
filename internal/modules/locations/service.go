package locations

import (
	"context"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type LocationsService struct {
	locationsRepo *LocationsRepository
}

func NewLocationsService(locationsRepo *LocationsRepository) *LocationsService {
	return &LocationsService{
		locationsRepo: locationsRepo,
	}
}

func (s *LocationsService) GetLocations(ctx context.Context, token string) (*models.LocationResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.locationsRepo.GetLocations(ctx, user.MerchantID)
}

func (s *LocationsService) UpdateLocationCoordinates(ctx context.Context, token, locationID string, x float64, y float64) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.locationsRepo.UpdateLocationCoordinates(ctx, user.MerchantID, locationID, x, y)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}
