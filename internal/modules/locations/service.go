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

func (s *LocationsService) CreateTable(ctx context.Context, token, floorID string, req CreateTableRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	locationID, err := s.locationsRepo.CreateTable(ctx, user.MerchantID, floorID, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":      "1",
		"location_id": locationID,
	}, nil
}

func (s *LocationsService) UpdateTable(ctx context.Context, token, locationID string, req UpdateTableRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.locationsRepo.UpdateTable(ctx, user.MerchantID, locationID, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *LocationsService) DeleteTable(ctx context.Context, token, locationID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.locationsRepo.DeleteTable(ctx, user.MerchantID, locationID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *LocationsService) CreateFloor(ctx context.Context, token string, req FloorCreateRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	floorID, err := s.locationsRepo.CreateFloor(ctx, user.MerchantID, req.Name)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":   "1",
		"floor_id": floorID,
	}, nil
}
