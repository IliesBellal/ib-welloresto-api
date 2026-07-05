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

// validTableShapes reflète les trois formes proposées par l'éditeur BO.
var validTableShapes = map[string]bool{"circle": true, "square": true, "rectangle": true}

// validateTableGeometry borne les propriétés d'une table au référentiel du plan
// (canvas virtuel 1000×1000, dimensions 40-300 de l'éditeur). Champs nil = non
// modifiés (payload partiel du PATCH), donc non validés.
func validateTableGeometry(x, y, width, height, angle *float64, seats *int, shape *string) error {
	inRange := func(v *float64, min, max float64) bool {
		return v == nil || (*v >= min && *v <= max)
	}

	if !inRange(x, 0, 1000) || !inRange(y, 0, 1000) {
		return models.ErrInvalidTableGeometry
	}
	if !inRange(width, 40, 300) || !inRange(height, 40, 300) {
		return models.ErrInvalidTableGeometry
	}
	if !inRange(angle, 0, 359) {
		return models.ErrInvalidTableGeometry
	}
	if seats != nil && *seats < 1 {
		return models.ErrInvalidTableGeometry
	}
	if shape != nil && !validTableShapes[*shape] {
		return models.ErrInvalidTableGeometry
	}

	return nil
}

func (s *LocationsService) CreateTable(ctx context.Context, token, floorID string, req CreateTableRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateTableGeometry(&req.X, &req.Y, &req.Width, &req.Height, &req.Angle, &req.Seats, &req.Shape); err != nil {
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

	if err := validateTableGeometry(req.X, req.Y, req.Width, req.Height, req.TableAngle(), req.Seats, req.Shape); err != nil {
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

func (s *LocationsService) UpdateFloor(ctx context.Context, token, floorID string, req FloorUpdateRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.locationsRepo.UpdateFloor(ctx, user.MerchantID, floorID, req.Name)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *LocationsService) DeleteFloor(ctx context.Context, token, floorID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.locationsRepo.DeleteFloor(ctx, user.MerchantID, floorID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}
