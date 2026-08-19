package locations

import (
	"context"
	"regexp"
	"strings"
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

func (s *LocationsService) GetLocations(ctx context.Context, token string, window *BookingWindow) (*models.LocationResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.locationsRepo.GetLocations(ctx, user.MerchantID, window)
}

// validTableShapes reflète les trois formes proposées par l'éditeur BO.
var validTableShapes = map[string]bool{"circle": true, "square": true, "rectangle": true, "oval": true}

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

// validObstacleTypes reflète les quatre types d'obstacles proposés par l'éditeur BO.
var validObstacleTypes = map[ObstacleType]bool{
	ObstacleTypeWall: true, ObstacleTypeBar: true, ObstacleTypeStairs: true, ObstacleTypeDoor: true,
}

// validateObstacleGeometry borne les propriétés d'un obstacle au référentiel du
// plan (canvas virtuel 1000×1000). Champs nil = non modifiés (payload partiel
// du PATCH), donc non validés. direction n'est autorisé que pour type=door.
func validateObstacleGeometry(obsType *ObstacleType, x, y, width, height, angle, direction *float64) error {
	inRange := func(v *float64, min, max float64) bool {
		return v == nil || (*v >= min && *v <= max)
	}

	if obsType != nil && !validObstacleTypes[*obsType] {
		return models.ErrInvalidObstacleGeometry
	}
	if !inRange(x, 0, 1000) || !inRange(y, 0, 1000) {
		return models.ErrInvalidObstacleGeometry
	}
	if !inRange(width, 10, 500) || !inRange(height, 10, 500) {
		return models.ErrInvalidObstacleGeometry
	}
	if !inRange(angle, 0, 359) {
		return models.ErrInvalidObstacleGeometry
	}
	if direction != nil {
		isDoor := obsType != nil && *obsType == ObstacleTypeDoor
		if !isDoor || !inRange(direction, 0, 359) {
			return models.ErrInvalidObstacleGeometry
		}
	}

	return nil
}

func (s *LocationsService) CreateObstacle(ctx context.Context, token string, req CreateObstacleRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateObstacleGeometry(&req.Type, &req.X, &req.Y, &req.Width, &req.Height, &req.Angle, req.Direction); err != nil {
		return nil, err
	}

	exists, err := s.locationsRepo.FloorExists(ctx, user.MerchantID, req.FloorID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, models.ErrFloorNotFound
	}

	obstacleID, err := s.locationsRepo.CreateObstacle(ctx, user.MerchantID, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
		"id":     obstacleID,
	}, nil
}

func (s *LocationsService) UpdateObstacle(ctx context.Context, token, obstacleID string, req UpdateObstacleRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateObstacleGeometry(req.Type, req.X, req.Y, req.Width, req.Height, req.Angle, req.Direction); err != nil {
		return nil, err
	}

	err = s.locationsRepo.UpdateObstacle(ctx, user.MerchantID, obstacleID, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *LocationsService) DeleteObstacle(ctx context.Context, token, obstacleID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.locationsRepo.DeleteObstacle(ctx, user.MerchantID, obstacleID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

// hexColorRegex valide un code couleur hexadécimal #RRGGBB ou #RRGGBBAA
// (format déjà utilisé par les zones existantes, cf. floor_areas.stroke_color/color VARCHAR(32)).
var hexColorRegex = regexp.MustCompile(`^#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// validateAreaGeometry borne les propriétés d'une zone-conteneur (floor_area) :
// nom non vide (max 50 caractères), couleurs au format hex, polygone d'au
// moins 3 points, angle 0-359. Champs nil = non modifiés (payload partiel du
// PATCH), donc non validés.
func validateAreaGeometry(name, strokeColor, color *string, points *[]AreaPoint, angle *float64) error {
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" || len(trimmed) > 50 {
			return models.ErrInvalidAreaGeometry
		}
	}
	if strokeColor != nil && !hexColorRegex.MatchString(*strokeColor) {
		return models.ErrInvalidAreaGeometry
	}
	if color != nil && !hexColorRegex.MatchString(*color) {
		return models.ErrInvalidAreaGeometry
	}
	if points != nil && len(*points) < 3 {
		return models.ErrInvalidAreaGeometry
	}
	if angle != nil && (*angle < 0 || *angle > 359) {
		return models.ErrInvalidAreaGeometry
	}

	return nil
}

func (s *LocationsService) CreateArea(ctx context.Context, token, floorID string, req CreateAreaRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.FloorID = floorID

	if err := validateAreaGeometry(&req.Name, &req.StrokeColor, &req.Color, &req.Points, &req.Angle); err != nil {
		return nil, err
	}

	exists, err := s.locationsRepo.FloorExists(ctx, user.MerchantID, req.FloorID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, models.ErrFloorNotFound
	}

	areaID, err := s.locationsRepo.CreateArea(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
		"id":     areaID,
	}, nil
}

func (s *LocationsService) UpdateArea(ctx context.Context, token, areaID string, req UpdateAreaRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateAreaGeometry(req.Name, req.StrokeColor, req.Color, req.Points, req.Angle); err != nil {
		return nil, err
	}

	err = s.locationsRepo.UpdateArea(ctx, user.MerchantID, areaID, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}

func (s *LocationsService) DeleteArea(ctx context.Context, token, areaID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.locationsRepo.DeleteArea(ctx, user.MerchantID, areaID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
	}, nil
}
