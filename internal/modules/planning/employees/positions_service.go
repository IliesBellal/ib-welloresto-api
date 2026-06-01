package employees

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

func (s *Service) ListEmployeePositions(ctx context.Context, filters EmployeePositionListFilters) ([]EmployeePosition, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListEmployeePositions(ctx, user.MerchantID, filters)
}

func (s *Service) GetEmployeePosition(ctx context.Context, positionID string) (*EmployeePosition, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(positionID) == "" {
		return nil, models.ErrMissingResourceID
	}
	position, err := s.repo.GetEmployeePositionByID(ctx, user.MerchantID, strings.TrimSpace(positionID))
	if err == sql.ErrNoRows || position == nil {
		return nil, models.ErrPlanningPositionNotFound
	}
	return position, err
}

func (s *Service) CreateEmployeePosition(ctx context.Context, req EmployeePositionCreateRequest) (*EmployeePosition, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		return nil, models.ErrPlanningPositionLabelRequired
	}
	color := sharedpkg.NormalizePlanningHexColor(req.Color)
	if !sharedpkg.IsValidPlanningHexColor(color) {
		return nil, models.ErrInvalidData
	}
	if existing, existingErr := s.repo.GetEmployeePositionByLabel(ctx, user.MerchantID, label, ""); existingErr == nil && existing != nil {
		return nil, models.ErrPlanningPositionAlreadyExists
	} else if existingErr != nil && existingErr != sql.ErrNoRows {
		return nil, existingErr
	}
	req.Label = label
	req.Color = color
	if req.SortOrder == nil {
		nextSortOrder, sortErr := s.repo.NextEmployeePositionSortOrder(ctx, user.MerchantID)
		if sortErr != nil {
			return nil, sortErr
		}
		req.SortOrder = &nextSortOrder
	}
	return s.repo.CreateEmployeePosition(ctx, user.MerchantID, req)
}

func (s *Service) UpdateEmployeePosition(ctx context.Context, positionID string, req EmployeePositionUpdateRequest) (*EmployeePosition, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(positionID) == "" {
		return nil, models.ErrMissingResourceID
	}
	current, err := s.repo.GetEmployeePositionByID(ctx, user.MerchantID, strings.TrimSpace(positionID))
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningPositionNotFound
	} else if err != nil {
		return nil, err
	}
	if req.Label != nil {
		label := strings.TrimSpace(*req.Label)
		if label == "" {
			return nil, models.ErrPlanningPositionLabelRequired
		}
		if existing, existingErr := s.repo.GetEmployeePositionByLabel(ctx, user.MerchantID, label, positionID); existingErr == nil && existing != nil {
			return nil, models.ErrPlanningPositionAlreadyExists
		} else if existingErr != nil && existingErr != sql.ErrNoRows {
			return nil, existingErr
		}
		current.Label = label
	}
	if req.SortOrder != nil {
		current.SortOrder = *req.SortOrder
	}
	if req.Color != nil {
		color := sharedpkg.NormalizePlanningHexColor(*req.Color)
		if !sharedpkg.IsValidPlanningHexColor(color) {
			return nil, models.ErrInvalidData
		}
		current.Color = color
	}
	if req.Active != nil {
		current.Active = *req.Active
	}
	return s.repo.UpdateEmployeePosition(ctx, user.MerchantID, positionID, *current)
}
func (s *Service) DeleteEmployeePosition(ctx context.Context, positionID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(positionID) == "" {
		return models.ErrMissingResourceID
	}
	count, countErr := s.repo.CountEmployeesByPositionID(ctx, user.MerchantID, strings.TrimSpace(positionID))
	if countErr != nil {
		return countErr
	}
	if count > 0 {
		return models.ErrPlanningPositionInUse
	}
	if err := s.repo.SoftDeleteEmployeePosition(ctx, user.MerchantID, strings.TrimSpace(positionID)); err == sql.ErrNoRows {
		return models.ErrPlanningPositionNotFound
	} else {
		return err
	}
}
