package swaps

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
	GetEmployeeByUserID(ctx context.Context, merchantID, userID string) (*employeespkg.Employee, error)
}

type ShiftReader interface {
	GetPlanningShiftByID(ctx context.Context, merchantID, shiftID string) (*schedulepkg.PlanningShift, error)
}

type ConflictChecker interface {
	EnsureShiftHasNoConflicts(ctx context.Context, merchantID string, excludeShiftID *string, employeeID string, shiftDate time.Time, startTime string, endTime string) error
}

type SettingsReader interface {
	GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error)
}

type Service struct {
	repo            *Repository
	employeeRepo    EmployeeReader
	shiftRepo       ShiftReader
	conflictChecker ConflictChecker
	settingsRepo    SettingsReader
}

func NewService(repo *Repository, employeeRepo EmployeeReader, shiftRepo ShiftReader, conflictChecker ConflictChecker, settingsRepo SettingsReader) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo, shiftRepo: shiftRepo, conflictChecker: conflictChecker, settingsRepo: settingsRepo}
}

func (s *Service) ListPlanningShiftSwapRequests(ctx context.Context, filters PlanningShiftSwapRequestListFilters) ([]PlanningShiftSwapRequest, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	if strings.TrimSpace(filters.RequesterEmployeeID) != "" {
		if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, strings.TrimSpace(filters.RequesterEmployeeID)); err != nil {
			return nil, models.PaginationMetadata{}, models.ErrPlanningEmployeeNotFound
		}
	}
	if strings.TrimSpace(filters.TargetEmployeeID) != "" {
		if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, strings.TrimSpace(filters.TargetEmployeeID)); err != nil {
			return nil, models.PaginationMetadata{}, models.ErrPlanningEmployeeNotFound
		}
	}
	if strings.TrimSpace(filters.Status) != "" && !sharedpkg.IsValidPlanningShiftSwapStatus(strings.TrimSpace(filters.Status)) {
		return nil, models.PaginationMetadata{}, models.ErrPlanningShiftSwapStatusInvalid
	}
	pagination := sharedpkg.NormalizePlanningPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.Page
	filters.PageSize = pagination.PageSize
	items, totalItems, err := s.repo.ListPlanningShiftSwapRequests(ctx, user.MerchantID, filters)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	return items, sharedpkg.BuildPaginationMetadata(totalItems, pagination), nil
}

func (s *Service) GetPlanningShiftSwapRequest(ctx context.Context, requestID string) (*PlanningShiftSwapRequest, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, models.ErrMissingResourceID
	}
	request, err := s.repo.GetPlanningShiftSwapRequestByID(ctx, user.MerchantID, requestID)
	if err == sql.ErrNoRows || request == nil {
		return nil, models.ErrPlanningShiftSwapRequestNotFound
	}
	return request, err
}

func (s *Service) CreatePlanningShiftSwapRequest(ctx context.Context, req PlanningShiftSwapRequestCreateRequest) (*PlanningShiftSwapRequest, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(req.RequesterEmployeeID) == "" || strings.TrimSpace(req.RequesterShiftID) == "" || strings.TrimSpace(req.TargetEmployeeID) == "" || strings.TrimSpace(req.TargetShiftID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if strings.TrimSpace(req.RequesterEmployeeID) == strings.TrimSpace(req.TargetEmployeeID) || strings.TrimSpace(req.RequesterShiftID) == strings.TrimSpace(req.TargetShiftID) {
		return nil, models.ErrPlanningShiftSwapInvalid
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, strings.TrimSpace(req.RequesterEmployeeID)); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, strings.TrimSpace(req.TargetEmployeeID)); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	requesterShift, err := s.shiftRepo.GetPlanningShiftByID(ctx, user.MerchantID, strings.TrimSpace(req.RequesterShiftID))
	if err == sql.ErrNoRows || requesterShift == nil {
		return nil, models.ErrPlanningShiftNotFound
	} else if err != nil {
		return nil, err
	}
	targetShift, err := s.shiftRepo.GetPlanningShiftByID(ctx, user.MerchantID, strings.TrimSpace(req.TargetShiftID))
	if err == sql.ErrNoRows || targetShift == nil {
		return nil, models.ErrPlanningShiftNotFound
	} else if err != nil {
		return nil, err
	}
	if requesterShift.EmployeeID == nil || strings.TrimSpace(*requesterShift.EmployeeID) != strings.TrimSpace(req.RequesterEmployeeID) {
		return nil, models.ErrPlanningShiftSwapInvalid
	}
	if targetShift.EmployeeID == nil || strings.TrimSpace(*targetShift.EmployeeID) != strings.TrimSpace(req.TargetEmployeeID) {
		return nil, models.ErrPlanningShiftSwapInvalid
	}
	requestedByUserID := user.UserID
	request := PlanningShiftSwapRequest{
		RequesterEmployeeID: strings.TrimSpace(req.RequesterEmployeeID),
		RequesterShiftID:    strings.TrimSpace(req.RequesterShiftID),
		TargetEmployeeID:    strings.TrimSpace(req.TargetEmployeeID),
		TargetShiftID:       strings.TrimSpace(req.TargetShiftID),
		Status:              "pending",
		Reason:              sharedpkg.TrimOptionalString(req.Reason),
		RequestedByUserID:   &requestedByUserID,
	}
	return s.repo.CreatePlanningShiftSwapRequest(ctx, user.MerchantID, request)
}

func (s *Service) UpdatePlanningShiftSwapRequest(ctx context.Context, requestID string, req PlanningShiftSwapRequestUpdateRequest) (*PlanningShiftSwapRequest, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, models.ErrMissingResourceID
	}
	current, err := s.repo.GetPlanningShiftSwapRequestByID(ctx, user.MerchantID, requestID)
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningShiftSwapRequestNotFound
	} else if err != nil {
		return nil, err
	}
	if req.Reason != nil {
		current.Reason = sharedpkg.TrimOptionalString(req.Reason)
	}
	if req.ManagerNote != nil {
		current.ManagerNote = sharedpkg.TrimOptionalString(req.ManagerNote)
	}
	if req.Status == nil {
		return s.repo.UpdatePlanningShiftSwapRequest(ctx, user.MerchantID, requestID, *current)
	}
	status := strings.ToLower(strings.TrimSpace(*req.Status))
	if !sharedpkg.IsValidPlanningShiftSwapStatus(status) {
		return nil, models.ErrPlanningShiftSwapStatusInvalid
	}
	approvalMode, err := s.getShiftSwapApprovalMode(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if approvalMode == settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired && (status == "approved" || status == "rejected") {
		if err := s.ensureTargetEmployeeApproval(ctx, user.MerchantID, current.TargetEmployeeID, user.UserID); err != nil {
			return nil, err
		}
	}
	if current.Status == "approved" && status != "approved" {
		return nil, models.ErrPlanningShiftSwapStatusInvalid
	}
	if status == "approved" && current.Status != "approved" {
		requesterShift, requesterErr := s.shiftRepo.GetPlanningShiftByID(ctx, user.MerchantID, current.RequesterShiftID)
		if requesterErr == sql.ErrNoRows || requesterShift == nil {
			return nil, models.ErrPlanningShiftNotFound
		} else if requesterErr != nil {
			return nil, requesterErr
		}
		targetShift, targetErr := s.shiftRepo.GetPlanningShiftByID(ctx, user.MerchantID, current.TargetShiftID)
		if targetErr == sql.ErrNoRows || targetShift == nil {
			return nil, models.ErrPlanningShiftNotFound
		} else if targetErr != nil {
			return nil, targetErr
		}
		if requesterShift.EmployeeID == nil || strings.TrimSpace(*requesterShift.EmployeeID) != current.RequesterEmployeeID {
			return nil, models.ErrPlanningShiftSwapInvalid
		}
		if targetShift.EmployeeID == nil || strings.TrimSpace(*targetShift.EmployeeID) != current.TargetEmployeeID {
			return nil, models.ErrPlanningShiftSwapInvalid
		}
		if conflictErr := s.conflictChecker.EnsureShiftHasNoConflicts(ctx, user.MerchantID, &current.TargetShiftID, current.RequesterEmployeeID, targetShift.ShiftDate, targetShift.StartTime, targetShift.EndTime); conflictErr != nil {
			return nil, models.ErrPlanningShiftSwapConflict
		}
		if conflictErr := s.conflictChecker.EnsureShiftHasNoConflicts(ctx, user.MerchantID, &current.RequesterShiftID, current.TargetEmployeeID, requesterShift.ShiftDate, requesterShift.StartTime, requesterShift.EndTime); conflictErr != nil {
			return nil, models.ErrPlanningShiftSwapConflict
		}
		processedByUserID := user.UserID
		processedAt := time.Now().UTC()
		current.Status = "approved"
		current.ProcessedByUserID = &processedByUserID
		current.ProcessedAt = &processedAt
		return s.repo.ApprovePlanningShiftSwapRequest(ctx, user.MerchantID, requestID, *current)
	}
	current.Status = status
	if status == "pending" {
		current.ProcessedAt = nil
		current.ProcessedByUserID = nil
	} else {
		processedByUserID := user.UserID
		processedAt := time.Now().UTC()
		current.ProcessedByUserID = &processedByUserID
		current.ProcessedAt = &processedAt
	}
	return s.repo.UpdatePlanningShiftSwapRequest(ctx, user.MerchantID, requestID, *current)
}

func (s *Service) getShiftSwapApprovalMode(ctx context.Context, merchantID string) (string, error) {
	settings, err := s.settingsRepo.GetOrCreateSettings(ctx, merchantID)
	if err != nil {
		return "", err
	}
	mode := settingspkg.NormalizeShiftSwapApprovalMode(settings.ShiftSwapApprovalMode)
	if mode == "" {
		mode = settingspkg.ShiftSwapApprovalModeManagerRequired
	}
	if !settingspkg.IsValidShiftSwapApprovalMode(mode) {
		return "", models.ErrPlanningShiftSwapApprovalModeInvalid
	}
	return mode, nil
}

func (s *Service) ensureTargetEmployeeApproval(ctx context.Context, merchantID, targetEmployeeID, currentUserID string) error {
	if strings.TrimSpace(currentUserID) == "" {
		return models.ErrPlanningShiftSwapApprovalForbidden
	}
	employee, err := s.employeeRepo.GetEmployeeByUserID(ctx, merchantID, currentUserID)
	if err == sql.ErrNoRows || employee == nil {
		return models.ErrPlanningShiftSwapApprovalForbidden
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(employee.ID) != strings.TrimSpace(targetEmployeeID) {
		return models.ErrPlanningShiftSwapApprovalForbidden
	}
	return nil
}

func (s *Service) DeletePlanningShiftSwapRequest(ctx context.Context, requestID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(requestID) == "" {
		return models.ErrMissingResourceID
	}
	if err := s.repo.SoftDeletePlanningShiftSwapRequest(ctx, user.MerchantID, requestID); err == sql.ErrNoRows {
		return models.ErrPlanningShiftSwapRequestNotFound
	} else {
		return err
	}
}
