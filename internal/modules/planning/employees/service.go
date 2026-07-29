package employees

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListEmployees(ctx context.Context, filters EmployeeListFilters) ([]Employee, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	pagination := sharedpkg.NormalizePlanningPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.Page
	filters.PageSize = pagination.PageSize
	items, totalItems, err := s.repo.ListEmployees(ctx, user.MerchantID, filters)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	return items, sharedpkg.BuildPaginationMetadata(totalItems, pagination), nil
}

func (s *Service) GetEmployee(ctx context.Context, employeeID string) (*Employee, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	employee, err := s.repo.GetEmployeeByID(ctx, user.MerchantID, employeeID)
	if err == sql.ErrNoRows || employee == nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	return employee, err
}

func (s *Service) CreateEmployee(ctx context.Context, req EmployeeCreateRequest) (*Employee, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(req.FirstName) == "" {
		return nil, models.ErrPlanningEmployeeNameRequired
	}
	if strings.TrimSpace(req.LastName) == "" {
		return nil, models.ErrPlanningEmployeeLastNameRequired
	}
	if strings.TrimSpace(req.PositionID) == "" {
		return nil, models.ErrPlanningEmployeePositionRequired
	}
	if strings.TrimSpace(req.ContractTypeCode) == "" {
		return nil, models.ErrPlanningEmployeeContractTypeInvalid
	}
	if req.UserID != nil && strings.TrimSpace(*req.UserID) == "" {
		return nil, models.ErrPlanningEmployeeUserLinkInvalid
	}
	if req.UserID != nil {
		normalizedUserID := strings.TrimSpace(*req.UserID)
		if err := s.validateEmployeeUserLink(ctx, user.MerchantID, normalizedUserID, ""); err != nil {
			return nil, err
		}
		req.UserID = &normalizedUserID
	}
	position, err := s.repo.GetEmployeePositionByID(ctx, user.MerchantID, strings.TrimSpace(req.PositionID))
	if err == sql.ErrNoRows || position == nil || !position.Active {
		return nil, models.ErrPlanningPositionNotFound
	} else if err != nil {
		return nil, err
	}
	req.PositionID = position.ID
	req.PositionNote = normalizeOptionalString(req.PositionNote)
	return s.repo.CreateEmployee(ctx, user.MerchantID, req)
}

func (s *Service) UpdateEmployeesDisplayOrder(ctx context.Context, req EmployeeDisplayOrderUpdateRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if len(req.EmployeeIDs) == 0 {
		return models.ErrInvalidInput
	}
	return s.repo.UpdateEmployeesDisplayOrder(ctx, user.MerchantID, req.EmployeeIDs)
}

func (s *Service) UpdateEmployee(ctx context.Context, employeeID string, req EmployeeUpdateRequest) (*Employee, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	current, err := s.repo.GetEmployeeByID(ctx, user.MerchantID, employeeID)
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	if req.FirstName != nil {
		if strings.TrimSpace(*req.FirstName) == "" {
			return nil, models.ErrPlanningEmployeeNameRequired
		}
		current.FirstName = strings.TrimSpace(*req.FirstName)
	}
	if req.LastName != nil {
		if strings.TrimSpace(*req.LastName) == "" {
			return nil, models.ErrPlanningEmployeeLastNameRequired
		}
		current.LastName = strings.TrimSpace(*req.LastName)
	}
	if req.PositionID != nil {
		if strings.TrimSpace(*req.PositionID) == "" {
			return nil, models.ErrPlanningEmployeePositionRequired
		}
		position, positionErr := s.repo.GetEmployeePositionByID(ctx, user.MerchantID, strings.TrimSpace(*req.PositionID))
		if positionErr == sql.ErrNoRows || position == nil || !position.Active {
			return nil, models.ErrPlanningPositionNotFound
		} else if positionErr != nil {
			return nil, positionErr
		}
		current.PositionID = position.ID
		current.Position = position.Label
	}
	if req.PositionNote != nil {
		current.PositionNote = normalizeOptionalString(req.PositionNote)
	}
	if req.ContractTypeCode != nil {
		if strings.TrimSpace(*req.ContractTypeCode) == "" {
			return nil, models.ErrPlanningEmployeeContractTypeInvalid
		}
		current.ContractTypeCode = strings.TrimSpace(*req.ContractTypeCode)
	}
	if req.UserID != nil {
		if strings.TrimSpace(*req.UserID) == "" {
			return nil, models.ErrPlanningEmployeeUserLinkInvalid
		}
		normalizedUserID := strings.TrimSpace(*req.UserID)
		if err := s.validateEmployeeUserLink(ctx, user.MerchantID, normalizedUserID, employeeID); err != nil {
			return nil, err
		}
		current.UserID = &normalizedUserID
	}
	if req.JobTitle != nil {
		current.JobTitle = req.JobTitle
	}
	if req.Email != nil {
		current.Email = req.Email
	}
	if req.Phone != nil {
		current.Phone = req.Phone
	}
	if req.Role != nil {
		current.Role = strings.ToLower(strings.TrimSpace(*req.Role))
	}
	if req.ContractStartDate != nil {
		current.ContractStartDate = req.ContractStartDate
	}
	if req.ContractEndDate != nil {
		current.ContractEndDate = req.ContractEndDate
	}
	if req.ProbationEndDate != nil {
		current.ProbationEndDate = req.ProbationEndDate
	}
	if req.LastMedicalCheckupDate != nil {
		current.LastMedicalCheckupDate = req.LastMedicalCheckupDate
	}
	if req.ContractHours != nil {
		current.ContractHours = *req.ContractHours
	}
	if req.MaxWeeklyHours != nil {
		current.MaxWeeklyHours = *req.MaxWeeklyHours
	}
	if req.RequiredRestDays != nil {
		current.RequiredRestDays = *req.RequiredRestDays
	}
	if req.SundayPremium != nil {
		current.SundayPremium = *req.SundayPremium
	}
	if req.NightPremium != nil {
		current.NightPremium = *req.NightPremium
	}
	if req.HourlyRate != nil {
		current.HourlyRate = *req.HourlyRate
	}
	if req.GrossMonthlySalary != nil {
		current.GrossMonthlySalary = *req.GrossMonthlySalary
	}
	if req.EmployerChargesPct != nil {
		current.EmployerChargesPct = *req.EmployerChargesPct
	}
	if req.TransportCost != nil {
		current.TransportCost = *req.TransportCost
	}
	if req.BirthDate != nil {
		current.BirthDate = req.BirthDate
	}
	if req.Gender != nil {
		current.Gender = req.Gender
	}
	if req.Nationality != nil {
		current.Nationality = req.Nationality
	}
	if req.Address != nil {
		current.Address = req.Address
	}
	if req.HrComment != nil {
		current.HrComment = req.HrComment
	}
	if req.Active != nil {
		current.Active = *req.Active
	}
	return s.repo.UpdateEmployee(ctx, user.MerchantID, employeeID, *current)
}

func (s *Service) DeleteEmployee(ctx context.Context, employeeID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return models.ErrMissingResourceID
	}
	if err := s.repo.SoftDeleteEmployee(ctx, user.MerchantID, employeeID); err == sql.ErrNoRows {
		return models.ErrPlanningEmployeeNotFound
	} else {
		return err
	}
}

func (s *Service) LinkEmployeeUser(ctx context.Context, employeeID string, req EmployeeUserLinkRequest) (*Employee, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	normalizedUserID := strings.TrimSpace(req.UserID)
	if normalizedUserID == "" {
		return nil, models.ErrPlanningEmployeeUserLinkInvalid
	}
	if _, err := s.repo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err == sql.ErrNoRows {
		return nil, models.ErrPlanningEmployeeNotFound
	} else if err != nil {
		return nil, err
	}
	if err := s.validateEmployeeUserLink(ctx, user.MerchantID, normalizedUserID, employeeID); err != nil {
		return nil, err
	}
	return s.repo.UpdateEmployeeUserLink(ctx, user.MerchantID, employeeID, &normalizedUserID)
}

func (s *Service) UnlinkEmployeeUser(ctx context.Context, employeeID string) (*Employee, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if _, err := s.repo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err == sql.ErrNoRows {
		return nil, models.ErrPlanningEmployeeNotFound
	} else if err != nil {
		return nil, err
	}
	return s.repo.UpdateEmployeeUserLink(ctx, user.MerchantID, employeeID, nil)
}

func RequireAtLeastOneEmployeeField(req EmployeeUpdateRequest) error {
	if req.UserID == nil && req.FirstName == nil && req.LastName == nil && req.PositionID == nil && req.PositionNote == nil && req.JobTitle == nil && req.Email == nil && req.Phone == nil && req.Role == nil && req.ContractTypeCode == nil && req.ContractStartDate == nil && req.ContractEndDate == nil && req.ProbationEndDate == nil && req.LastMedicalCheckupDate == nil && req.ContractHours == nil && req.MaxWeeklyHours == nil && req.RequiredRestDays == nil && req.SundayPremium == nil && req.NightPremium == nil && req.HourlyRate == nil && req.GrossMonthlySalary == nil && req.EmployerChargesPct == nil && req.TransportCost == nil && req.BirthDate == nil && req.Gender == nil && req.Nationality == nil && req.Address == nil && req.HrComment == nil && req.Active == nil {
		return fmt.Errorf("at least one field must be provided")
	}
	return nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *Service) validateEmployeeUserLink(ctx context.Context, merchantID, userID, excludedEmployeeID string) error {
	linked, err := s.repo.IsMerchantUserLinked(ctx, merchantID, userID)
	if err != nil {
		return err
	}
	if !linked {
		return models.ErrPlanningEmployeeUserNotLinkedToMerchant
	}
	existingEmployee, err := s.repo.GetActiveEmployeeByUserID(ctx, merchantID, userID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if existingEmployee != nil && existingEmployee.ID != excludedEmployeeID {
		return models.ErrPlanningEmployeeUserAlreadyAssigned
	}
	return nil
}
