package shifttemplates

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type PositionReader interface {
	GetEmployeePositionByID(ctx context.Context, merchantID, positionID string) (*employeespkg.EmployeePosition, error)
}

type Service struct {
	repo           *Repository
	positionReader PositionReader
}

func NewService(repo *Repository, positionReader PositionReader) *Service {
	return &Service{repo: repo, positionReader: positionReader}
}

func (s *Service) ListShiftTemplates(ctx context.Context) ([]ShiftTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListShiftTemplates(ctx, user.MerchantID)
}

func (s *Service) CreateShiftTemplate(ctx context.Context, req ShiftTemplateCreateRequest) (*ShiftTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		return nil, models.ErrPlanningShiftTemplateLabelRequired
	}
	if req.BreakMinutes == nil || *req.BreakMinutes < 0 {
		return nil, models.ErrInvalidData
	}
	startTime, endTime, err := normalizeShiftTemplateTimeRange(req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	color := sharedpkg.NormalizePlanningHexColor(req.Color)
	if !sharedpkg.IsValidPlanningHexColor(color) {
		return nil, models.ErrInvalidData
	}
	positionID := sharedpkg.TrimOptionalString(req.PositionID)
	if err := s.validateShiftTemplatePosition(ctx, user.MerchantID, positionID); err != nil {
		return nil, err
	}
	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	} else {
		nextSortOrder, sortErr := s.repo.NextShiftTemplateSortOrder(ctx, user.MerchantID)
		if sortErr != nil {
			return nil, sortErr
		}
		sortOrder = nextSortOrder
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	return s.repo.CreateShiftTemplate(ctx, user.MerchantID, ShiftTemplate{
		Label:        label,
		StartTime:    startTime,
		EndTime:      endTime,
		BreakMinutes: *req.BreakMinutes,
		PositionID:   positionID,
		Color:        color,
		SortOrder:    sortOrder,
		Active:       active,
	})
}

func (s *Service) UpdateShiftTemplate(ctx context.Context, templateID string, req ShiftTemplateUpdateRequest) (*ShiftTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(templateID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if req.Label == nil && req.StartTime == nil && req.EndTime == nil && req.BreakMinutes == nil && !req.PositionID.Present && req.Color == nil && req.SortOrder == nil && req.Active == nil {
		return nil, models.ErrValidationError
	}
	current, err := s.repo.GetShiftTemplateByID(ctx, user.MerchantID, strings.TrimSpace(templateID))
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningShiftTemplateNotFound
	} else if err != nil {
		return nil, err
	}
	if req.Label != nil {
		label := strings.TrimSpace(*req.Label)
		if label == "" {
			return nil, models.ErrPlanningShiftTemplateLabelRequired
		}
		current.Label = label
	}
	if req.StartTime != nil {
		current.StartTime = strings.TrimSpace(*req.StartTime)
	}
	if req.EndTime != nil {
		current.EndTime = strings.TrimSpace(*req.EndTime)
	}
	if req.BreakMinutes != nil {
		if *req.BreakMinutes < 0 {
			return nil, models.ErrInvalidData
		}
		current.BreakMinutes = *req.BreakMinutes
	}
	if req.PositionID.Present {
		current.PositionID = sharedpkg.TrimOptionalString(req.PositionID.Value)
	}
	if req.Color != nil {
		color := sharedpkg.NormalizePlanningHexColor(*req.Color)
		if !sharedpkg.IsValidPlanningHexColor(color) {
			return nil, models.ErrInvalidData
		}
		current.Color = color
	}
	if req.SortOrder != nil {
		current.SortOrder = *req.SortOrder
	}
	if req.Active != nil {
		current.Active = *req.Active
	}
	if err := s.validateShiftTemplatePosition(ctx, user.MerchantID, current.PositionID); err != nil {
		return nil, err
	}
	startTime, endTime, err := normalizeShiftTemplateTimeRange(current.StartTime, current.EndTime)
	if err != nil {
		return nil, err
	}
	current.StartTime = startTime
	current.EndTime = endTime
	return s.repo.UpdateShiftTemplate(ctx, user.MerchantID, templateID, *current)
}

func (s *Service) DeleteShiftTemplate(ctx context.Context, templateID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(templateID) == "" {
		return models.ErrMissingResourceID
	}
	current, err := s.repo.GetShiftTemplateByID(ctx, user.MerchantID, strings.TrimSpace(templateID))
	if err == sql.ErrNoRows || current == nil {
		return models.ErrPlanningShiftTemplateNotFound
	} else if err != nil {
		return err
	}
	current.Active = false
	_, err = s.repo.UpdateShiftTemplate(ctx, user.MerchantID, templateID, *current)
	if err == sql.ErrNoRows {
		return models.ErrPlanningShiftTemplateNotFound
	}
	return err
}

func normalizeShiftTemplateTimeRange(startRaw, endRaw string) (string, string, error) {
	startTime, err := sharedpkg.ParsePlanningTime(startRaw)
	if err != nil {
		return "", "", err
	}
	endTime, err := sharedpkg.ParsePlanningTime(endRaw)
	if err != nil {
		return "", "", err
	}
	if !endTime.After(startTime) {
		return "", "", models.ErrPlanningShiftTemplateInvalidRange
	}
	return startTime.Format("15:04"), endTime.Format("15:04"), nil
}

func (s *Service) validateShiftTemplatePosition(ctx context.Context, merchantID string, positionID *string) error {
	if positionID == nil {
		return nil
	}
	if s.positionReader == nil {
		return models.ErrPlanningPositionNotFound
	}
	position, err := s.positionReader.GetEmployeePositionByID(ctx, merchantID, *positionID)
	if err == sql.ErrNoRows || position == nil || !position.Active {
		return models.ErrPlanningPositionNotFound
	}
	return err
}
