package employees

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListEmployees(ctx context.Context, filters EmployeeListFilters) ([]Employee, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListEmployees(ctx, user.MerchantID, filters)
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
	if strings.TrimSpace(req.Position) == "" {
		return nil, models.ErrPlanningEmployeePositionRequired
	}
	if strings.TrimSpace(req.ContractTypeCode) == "" {
		return nil, models.ErrPlanningEmployeeContractTypeInvalid
	}
	if strings.TrimSpace(req.TimeTrackingModeCode) == "" {
		return nil, models.ErrPlanningEmployeeTimeTrackingModeInvalid
	}
	if req.UserID != nil && strings.TrimSpace(*req.UserID) == "" {
		return nil, models.ErrPlanningEmployeeUserLinkInvalid
	}
	return s.repo.CreateEmployee(ctx, user.MerchantID, req)
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
	if req.Position != nil {
		if strings.TrimSpace(*req.Position) == "" {
			return nil, models.ErrPlanningEmployeePositionRequired
		}
		current.Position = strings.TrimSpace(*req.Position)
	}
	if req.ContractTypeCode != nil {
		if strings.TrimSpace(*req.ContractTypeCode) == "" {
			return nil, models.ErrPlanningEmployeeContractTypeInvalid
		}
		current.ContractTypeCode = strings.TrimSpace(*req.ContractTypeCode)
	}
	if req.TimeTrackingModeCode != nil {
		if strings.TrimSpace(*req.TimeTrackingModeCode) == "" {
			return nil, models.ErrPlanningEmployeeTimeTrackingModeInvalid
		}
		current.TimeTrackingModeCode = strings.TrimSpace(*req.TimeTrackingModeCode)
	}
	if req.UserID != nil {
		if strings.TrimSpace(*req.UserID) == "" {
			return nil, models.ErrPlanningEmployeeUserLinkInvalid
		}
		current.UserID = req.UserID
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

func RequireAtLeastOneEmployeeField(req EmployeeUpdateRequest) error {
	if req.UserID == nil && req.FirstName == nil && req.LastName == nil && req.Position == nil && req.JobTitle == nil && req.Email == nil && req.Phone == nil && req.Role == nil && req.ContractTypeCode == nil && req.ContractStartDate == nil && req.ContractEndDate == nil && req.ProbationEndDate == nil && req.LastMedicalCheckupDate == nil && req.ContractHours == nil && req.MaxWeeklyHours == nil && req.RequiredRestDays == nil && req.SundayPremium == nil && req.NightPremium == nil && req.TimeTrackingModeCode == nil && req.HourlyRate == nil && req.GrossMonthlySalary == nil && req.EmployerChargesPct == nil && req.TransportCost == nil && req.BirthDate == nil && req.Gender == nil && req.Nationality == nil && req.Address == nil && req.HrComment == nil && req.Active == nil {
		return fmt.Errorf("at least one field must be provided")
	}
	return nil
}
