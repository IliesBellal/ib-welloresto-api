package leave

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
	GetEmployeeIDByMemberID(ctx context.Context, merchantID, memberID string) (string, error)
}

type Service struct {
	repo         *Repository
	employeeRepo EmployeeReader
}

func NewService(repo *Repository, employeeRepo EmployeeReader) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo}
}

func (s *Service) ResolveCurrentEmployeeID(ctx context.Context) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", models.ErrUnauthorized
	}
	employeeID, err := sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, "me", user.MerchantRightsID)
	if err != nil {
		return "", err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return "", models.ErrPlanningEmployeeNotFound
	}
	return employeeID, nil
}

func (s *Service) ListCurrentUserLeaveRequests(ctx context.Context, status string) ([]PlanningLeaveRequestSelfView, error) {
	items, _, err := s.ListCurrentUserLeaveRequestsPaginated(ctx, PlanningLeaveRequestListFilters{Status: status})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) ListCurrentUserLeaveRequestsPaginated(ctx context.Context, filters PlanningLeaveRequestListFilters) ([]PlanningLeaveRequestSelfView, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	employeeID, err := s.ResolveCurrentEmployeeID(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	if strings.TrimSpace(filters.Status) != "" && !sharedpkg.IsValidPlanningLeaveStatus(strings.TrimSpace(filters.Status)) {
		return nil, models.PaginationMetadata{}, models.ErrPlanningLeaveStatusInvalid
	}
	if filters.FromDate != nil && filters.ToDate != nil && filters.ToDate.Before(*filters.FromDate) {
		return nil, models.PaginationMetadata{}, models.ErrPlanningLeaveInvalidRange
	}
	pagination := sharedpkg.NormalizePlanningPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.Page
	filters.PageSize = pagination.PageSize
	filters.EmployeeID = employeeID
	items, totalItems, err := s.repo.ListPlanningLeaveRequests(ctx, user.MerchantID, PlanningLeaveRequestListFilters{
		EmployeeID: filters.EmployeeID,
		Status:     strings.TrimSpace(filters.Status),
		Page:       filters.Page,
		PageSize:   filters.PageSize,
		FromDate:   filters.FromDate,
		ToDate:     filters.ToDate,
	})
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}

	result := make([]PlanningLeaveRequestSelfView, len(items))
	for i, item := range items {
		result[i] = mapPlanningLeaveRequestToSelfView(item)
	}
	return result, sharedpkg.BuildPaginationMetadata(totalItems, pagination), nil
}

func (s *Service) CreateCurrentUserLeaveRequest(ctx context.Context, req PlanningLeaveRequestSelfCreateRequest) (*PlanningLeaveRequestSelfView, error) {
	employeeID, err := s.ResolveCurrentEmployeeID(ctx)
	if err != nil {
		return nil, err
	}

	created, err := s.CreatePlanningLeaveRequest(ctx, PlanningLeaveRequestCreateRequest{
		EmployeeID: employeeID,
		LeaveType:  req.LeaveType,
		StartDate:  req.StartDate,
		EndDate:    req.EndDate,
		Reason:     req.Reason,
	})
	if err != nil {
		return nil, err
	}

	view := mapPlanningLeaveRequestToSelfView(*created)
	return &view, nil
}

func mapPlanningLeaveRequestToSelfView(item PlanningLeaveRequest) PlanningLeaveRequestSelfView {
	return PlanningLeaveRequestSelfView{
		ID:          item.ID,
		EmployeeID:  item.EmployeeID,
		LeaveType:   item.LeaveType,
		StartDate:   item.StartDate,
		EndDate:     item.EndDate,
		Status:      item.Status,
		Reason:      item.Reason,
		ManagerNote: item.ManagerNote,
		ProcessedAt: item.ProcessedAt,
		CreatedAt:   item.CreatedAt,
	}
}

func (s *Service) ListPlanningLeaveRequests(ctx context.Context, filters PlanningLeaveRequestListFilters) ([]PlanningLeaveRequest, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	if strings.TrimSpace(filters.EmployeeID) != "" {
		filters.EmployeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, filters.EmployeeID, user.MerchantRightsID)
		if err != nil {
			return nil, models.PaginationMetadata{}, err
		}
		if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, strings.TrimSpace(filters.EmployeeID)); err != nil {
			return nil, models.PaginationMetadata{}, models.ErrPlanningEmployeeNotFound
		}
	}
	if strings.TrimSpace(filters.Status) != "" && !sharedpkg.IsValidPlanningLeaveStatus(strings.TrimSpace(filters.Status)) {
		return nil, models.PaginationMetadata{}, models.ErrPlanningLeaveStatusInvalid
	}
	if filters.FromDate != nil && filters.ToDate != nil && filters.ToDate.Before(*filters.FromDate) {
		return nil, models.PaginationMetadata{}, models.ErrPlanningLeaveInvalidRange
	}
	pagination := sharedpkg.NormalizePlanningPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.Page
	filters.PageSize = pagination.PageSize
	items, totalItems, err := s.repo.ListPlanningLeaveRequests(ctx, user.MerchantID, filters)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	return items, sharedpkg.BuildPaginationMetadata(totalItems, pagination), nil
}

func (s *Service) GetPlanningLeaveRequest(ctx context.Context, requestID string) (*PlanningLeaveRequest, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, models.ErrMissingResourceID
	}
	request, err := s.repo.GetPlanningLeaveRequestByID(ctx, user.MerchantID, requestID)
	if err == sql.ErrNoRows || request == nil {
		return nil, models.ErrPlanningLeaveRequestNotFound
	}
	return request, err
}

func (s *Service) CreatePlanningLeaveRequest(ctx context.Context, req PlanningLeaveRequestCreateRequest) (*PlanningLeaveRequest, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(req.EmployeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	resolvedEmployeeID, err := sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, req.EmployeeID, user.MerchantRightsID)
	if err != nil {
		return nil, err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, resolvedEmployeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	leaveType := strings.ToLower(strings.TrimSpace(req.LeaveType))
	if !sharedpkg.IsValidPlanningLeaveType(leaveType) {
		return nil, models.ErrPlanningLeaveTypeInvalid
	}
	startDate, endDate, err := sharedpkg.ParsePlanningDateRange(req.StartDate, req.EndDate)
	if err != nil {
		return nil, models.ErrPlanningLeaveInvalidRange
	}
	requestedByUserID := user.UserID
	request := PlanningLeaveRequest{
		EmployeeID:        resolvedEmployeeID,
		LeaveType:         leaveType,
		StartDate:         startDate,
		EndDate:           endDate,
		Status:            "pending",
		Reason:            sharedpkg.TrimOptionalString(req.Reason),
		RequestedByUserID: &requestedByUserID,
	}
	return s.repo.CreatePlanningLeaveRequest(ctx, user.MerchantID, request)
}

func (s *Service) UpdatePlanningLeaveRequest(ctx context.Context, requestID string, req PlanningLeaveRequestUpdateRequest) (*PlanningLeaveRequest, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, models.ErrMissingResourceID
	}
	current, err := s.repo.GetPlanningLeaveRequestByID(ctx, user.MerchantID, requestID)
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningLeaveRequestNotFound
	} else if err != nil {
		return nil, err
	}
	if req.LeaveType != nil {
		leaveType := strings.ToLower(strings.TrimSpace(*req.LeaveType))
		if !sharedpkg.IsValidPlanningLeaveType(leaveType) {
			return nil, models.ErrPlanningLeaveTypeInvalid
		}
		current.LeaveType = leaveType
	}
	if req.StartDate != nil {
		startDate, parseErr := sharedpkg.ParsePlanningDate(*req.StartDate)
		if parseErr != nil {
			return nil, models.ErrPlanningLeaveInvalidRange
		}
		current.StartDate = startDate
	}
	if req.EndDate != nil {
		endDate, parseErr := sharedpkg.ParsePlanningDate(*req.EndDate)
		if parseErr != nil {
			return nil, models.ErrPlanningLeaveInvalidRange
		}
		current.EndDate = endDate
	}
	if current.EndDate.Before(current.StartDate) {
		return nil, models.ErrPlanningLeaveInvalidRange
	}
	if req.Reason != nil {
		current.Reason = sharedpkg.TrimOptionalString(req.Reason)
	}
	if req.ManagerNote != nil {
		current.ManagerNote = sharedpkg.TrimOptionalString(req.ManagerNote)
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if !sharedpkg.IsValidPlanningLeaveStatus(status) {
			return nil, models.ErrPlanningLeaveStatusInvalid
		}
		current.Status = status
		if status == "approved" {
			count, countErr := s.repo.CountEmployeeAssignedShiftsInRange(ctx, user.MerchantID, current.EmployeeID, current.StartDate, current.EndDate)
			if countErr != nil {
				return nil, countErr
			}
			if count > 0 {
				return nil, models.ErrPlanningLeaveShiftConflict
			}
		}
		if status == "pending" {
			current.ProcessedAt = nil
			current.ProcessedByUserID = nil
		} else {
			processedByUserID := user.UserID
			processedAt := time.Now().UTC()
			current.ProcessedByUserID = &processedByUserID
			current.ProcessedAt = &processedAt
		}
	}
	return s.repo.UpdatePlanningLeaveRequest(ctx, user.MerchantID, requestID, *current)
}

func (s *Service) DeletePlanningLeaveRequest(ctx context.Context, requestID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(requestID) == "" {
		return models.ErrMissingResourceID
	}
	if err := s.repo.SoftDeletePlanningLeaveRequest(ctx, user.MerchantID, requestID); err == sql.ErrNoRows {
		return models.ErrPlanningLeaveRequestNotFound
	} else {
		return err
	}
}

func (s *Service) ListPlanningLeaveRequestConflictingShifts(ctx context.Context, requestID string) ([]PlanningLeaveConflictingShift, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, models.ErrMissingResourceID
	}

	leaveRequest, err := s.repo.GetPlanningLeaveRequestByID(ctx, user.MerchantID, requestID)
	if err == sql.ErrNoRows || leaveRequest == nil {
		return nil, models.ErrPlanningLeaveRequestNotFound
	}
	if err != nil {
		return nil, err
	}

	items, err := s.repo.ListEmployeeAssignedShiftsInRange(ctx, user.MerchantID, leaveRequest.EmployeeID, leaveRequest.StartDate, leaveRequest.EndDate)
	if err != nil {
		return nil, err
	}

	return items, nil
}
