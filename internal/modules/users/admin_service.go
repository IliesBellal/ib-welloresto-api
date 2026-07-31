package users

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	planningemployees "welloresto-api/internal/modules/planning/employees"
	"welloresto-api/internal/utils/dbutils"
)

func (s *UsersService) ListMerchantUsers(ctx context.Context, filters MerchantUserListFilters) ([]MerchantUserListItem, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	pagination := normalizeUsersPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.CurrentPage
	filters.PageSize = pagination.Limit
	items, totalItems, err := s.userRepo.ListMerchantUsers(ctx, user.MerchantID, filters)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	return items, buildUsersPaginationMetadata(totalItems, pagination), nil
}

func (s *UsersService) GetMerchantUser(ctx context.Context, userID string) (*MerchantUserDetail, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(userID) == "" {
		return nil, models.ErrMissingResourceID
	}
	item, err := s.userRepo.GetMerchantUserByID(ctx, currentUser.MerchantID, userID)
	if err == sql.ErrNoRows {
		return nil, models.ErrMerchantUserNotFound
	}
	return item, mapMerchantUserNotFound(err)
}

func (s *UsersService) SearchLinkableUsers(ctx context.Context, filters LinkableUserSearchFilters) ([]LinkableUser, models.PaginationMetadata, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	pagination := normalizeUsersPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.CurrentPage
	filters.PageSize = pagination.Limit
	items, totalItems, err := s.userRepo.SearchLinkableUsers(ctx, currentUser.MerchantID, filters)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	return items, buildUsersPaginationMetadata(totalItems, pagination), nil
}

func (s *UsersService) GetMerchantUserRights(ctx context.Context, userID string) (*MerchantUserRights, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(userID) == "" {
		return nil, models.ErrMissingResourceID
	}
	rights, err := s.userRepo.GetMerchantUserRights(ctx, currentUser.MerchantID, userID)
	if err == sql.ErrNoRows {
		return nil, models.ErrMerchantUserNotFound
	}
	return rights, mapMerchantUserNotFound(err)
}

func (s *UsersService) UpdateMerchantUserRights(ctx context.Context, userID string, req MerchantUserRightsUpsertRequest) (*MerchantUserRights, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(userID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if err := s.userRepo.UpdateMerchantUserRights(ctx, currentUser.MerchantID, userID, req.Normalize(defaultMerchantUserRights(req.Admin))); err != nil {
		return nil, mapMerchantUserNotFound(err)
	}

	// Le token ne change pas ici : on invalide le cache de session pour que la
	// prochaine requête relise les droits/login_enabled fraîchement écrits,
	// au lieu d'attendre l'expiration du TTL (models.UserCacheTTL, 60 min).
	// Best-effort : une erreur de lookup ne doit jamais faire échouer la mise à jour.
	if token, tokenErr := s.userRepo.GetUsersRightsToken(ctx, currentUser.MerchantID, userID); tokenErr == nil && token != "" {
		s.redis.Delete(ctx, models.UserCachePrefix+token)
	}

	return s.userRepo.GetMerchantUserRights(ctx, currentUser.MerchantID, userID)
}

func (s *UsersService) LinkMerchantUser(ctx context.Context, userID string, req MerchantUserLinkRequest) (*MerchantUserRights, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, models.ErrMissingResourceID
	}
	exists, err := s.userRepo.UserExists(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, models.ErrUserNotFound
	}
	linked, err := s.userRepo.MerchantUserLinkExists(ctx, currentUser.MerchantID, userID)
	if err != nil {
		return nil, err
	}
	if linked {
		return nil, models.ErrMerchantUserAlreadyLinked
	}
	rights := defaultMerchantUserRights(req.Admin)
	if req.Rights != nil {
		rights = req.Rights.Normalize(rights)
	}

	var merchantRights *MerchantUserRights
	err = dbutils.RunInTx(ctx, s.userRepo.database, func(txCtx context.Context) error {
		token, tokenErr := helpers.GenerateToken(30)
		if tokenErr != nil {
			return tokenErr
		}
		if _, upsertErr := s.userRepo.UpsertMerchantUserRights(txCtx, userID, currentUser.MerchantID, token, rights); upsertErr != nil {
			return upsertErr
		}
		merchantRights, err = s.userRepo.GetMerchantUserRights(txCtx, currentUser.MerchantID, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return merchantRights, nil
}

func (s *UsersService) ForceResetPassword(ctx context.Context, userID string, req ForceResetPasswordRequest) error {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return models.ErrMissingResourceID
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		return models.ErrInvalidInput
	}
	if err := validateNewPassword(req.NewPassword); err != nil {
		return err
	}
	if _, err := s.userRepo.GetMerchantUserRights(ctx, currentUser.MerchantID, userID); err == sql.ErrNoRows {
		return models.ErrMerchantUserNotFound
	} else if err != nil {
		return err
	}
	oldToken, err := s.userRepo.GetUsersRightsToken(ctx, currentUser.MerchantID, userID)
	if err != nil {
		return err
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	if _, err := s.userRepo.UpdatePassword(ctx, userID, currentUser.MerchantID, hash); err != nil {
		return err
	}
	s.redis.Delete(ctx, models.UserCachePrefix+oldToken)
	if s.audit != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "force_reset_password", "merchant_user", userID, map[string]any{"reset": true}, map[string]any{"reset": true})
	}
	return nil
}

func (s *UsersService) UnlinkMerchantUser(ctx context.Context, userID string) (*MerchantUserUnlinkResult, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, models.ErrMissingResourceID
	}
	oldState, err := s.userRepo.GetMerchantUserRights(ctx, currentUser.MerchantID, userID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	// Récupéré avant la transaction : DisableMerchantUserLink met enabled=FALSE,
	// et GetUsersRightsToken filtre sur enabled=TRUE — après coup il ne
	// trouverait plus rien. Best-effort, ignoré si absent.
	token, _ := s.userRepo.GetUsersRightsToken(ctx, currentUser.MerchantID, userID)

	result := &MerchantUserUnlinkResult{}
	err = dbutils.RunInTx(ctx, s.userRepo.database, func(txCtx context.Context) error {
		cleared, clearErr := s.userRepo.ClearMerchantEmployeeLinks(txCtx, currentUser.MerchantID, userID)
		if clearErr != nil {
			return clearErr
		}
		result.EmployeeLinksCleared = cleared
		unlinked, unlinkErr := s.userRepo.DisableMerchantUserLink(txCtx, currentUser.MerchantID, userID)
		if unlinkErr != nil {
			return unlinkErr
		}
		result.Unlinked = unlinked
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Invalide la session mise en cache pour que l'accès révoqué (côté SQL,
	// voir GetUserByToken) prenne effet immédiatement au lieu d'attendre le TTL.
	if result.Unlinked && token != "" {
		s.redis.Delete(ctx, models.UserCachePrefix+token)
	}

	if result.Unlinked && s.audit != nil && oldState != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "unlink_merchant_user", "merchant_user", userID, oldState, map[string]any{"enabled": false, "employee_links_cleared": result.EmployeeLinksCleared})
	}
	return result, nil
}

func (s *UsersService) GetMerchantUserMember(ctx context.Context, userID string) (*MerchantUserMember, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, models.ErrMissingResourceID
	}
	if _, err := s.userRepo.GetMerchantUserByID(ctx, currentUser.MerchantID, userID); err == sql.ErrNoRows {
		return nil, models.ErrMerchantUserNotFound
	} else if err != nil {
		return nil, err
	}
	employee, err := s.memberEmployee.GetActiveEmployeeByUserID(ctx, currentUser.MerchantID, userID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapMemberFromEmployee(employee), nil
}

func (s *UsersService) PatchMerchantUserMember(ctx context.Context, userID string, req MerchantUserMemberPatchRequest) (*MerchantUserMember, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, models.ErrMissingResourceID
	}
	userDetail, err := s.userRepo.GetMerchantUserByID(ctx, currentUser.MerchantID, userID)
	if err == sql.ErrNoRows {
		return nil, models.ErrMerchantUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := requireAtLeastOneMemberField(req); err != nil {
		return nil, err
	}

	var output *MerchantUserMember
	err = dbutils.RunInTx(ctx, s.userRepo.database, func(txCtx context.Context) error {
		existing, existingErr := s.memberEmployee.GetActiveEmployeeByUserID(txCtx, currentUser.MerchantID, userID)
		if existingErr != nil && existingErr != sql.ErrNoRows {
			return existingErr
		}

		updateReq := memberPatchToEmployeeUpdate(req)

		if existingErr == sql.ErrNoRows || existing == nil {
			if req.PositionID == nil || strings.TrimSpace(*req.PositionID) == "" {
				return models.ErrPlanningEmployeePositionRequired
			}
			if req.ContractTypeCode == nil || strings.TrimSpace(*req.ContractTypeCode) == "" {
				return models.ErrPlanningEmployeeContractTypeRequired
			}

			created, createErr := s.memberEmployee.CreateEmployee(txCtx, planningemployees.EmployeeCreateRequest{
				FirstName:        strings.TrimSpace(userDetail.FirstName),
				LastName:         strings.TrimSpace(userDetail.LastName),
				PositionID:       strings.TrimSpace(*req.PositionID),
				ContractTypeCode: strings.TrimSpace(*req.ContractTypeCode),
			})
			if createErr != nil {
				return createErr
			}
			linked, linkErr := s.memberEmployee.LinkEmployeeUser(txCtx, created.ID, planningemployees.EmployeeUserLinkRequest{UserID: userID})
			if linkErr != nil {
				return linkErr
			}
			updated, updateErr := s.memberEmployee.UpdateEmployee(txCtx, linked.ID, updateReq)
			if updateErr != nil {
				return updateErr
			}
			output = mapMemberFromEmployee(updated)
			return nil
		}

		updated, updateErr := s.memberEmployee.UpdateEmployee(txCtx, existing.ID, updateReq)
		if updateErr != nil {
			return updateErr
		}
		output = mapMemberFromEmployee(updated)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return output, nil
}

func requireAtLeastOneMemberField(req MerchantUserMemberPatchRequest) error {
	if req.PositionID == nil && req.Role == nil && req.ContractTypeCode == nil && !req.ContractStartDate.Present && !req.ContractEndDate.Present && !req.ProbationEndDate.Present && !req.LastMedicalCheckupDate.Present && req.ContractHours == nil && req.MaxWeeklyHours == nil && req.RequiredRestDays == nil && req.SundayPremium == nil && req.NightPremium == nil && req.EmployerChargesPct == nil && req.HourlyRate == nil && req.GrossMonthlySalary == nil && req.TransportCost == nil && req.HrComment == nil {
		return fmt.Errorf("at least one field must be provided")
	}
	return nil
}

func memberPatchToEmployeeUpdate(req MerchantUserMemberPatchRequest) planningemployees.EmployeeUpdateRequest {
	return planningemployees.EmployeeUpdateRequest{
		PositionID:             req.PositionID,
		Role:                   req.Role,
		ContractTypeCode:       req.ContractTypeCode,
		ContractStartDate:      dateOnlyPatchFieldToTimePtr(req.ContractStartDate),
		ContractEndDate:        dateOnlyPatchFieldToTimePtr(req.ContractEndDate),
		ProbationEndDate:       dateOnlyPatchFieldToTimePtr(req.ProbationEndDate),
		LastMedicalCheckupDate: dateOnlyPatchFieldToTimePtr(req.LastMedicalCheckupDate),
		ContractHours:          req.ContractHours,
		MaxWeeklyHours:         req.MaxWeeklyHours,
		RequiredRestDays:       req.RequiredRestDays,
		SundayPremium:          req.SundayPremium,
		NightPremium:           req.NightPremium,
		EmployerChargesPct:     req.EmployerChargesPct,
		HourlyRate:             req.HourlyRate,
		GrossMonthlySalary:     req.GrossMonthlySalary,
		TransportCost:          req.TransportCost,
		HrComment:              req.HrComment,
	}
}

func mapMemberFromEmployee(employee *planningemployees.Employee) *MerchantUserMember {
	if employee == nil {
		return nil
	}
	return &MerchantUserMember{
		PositionID:             employee.PositionID,
		Role:                   employee.Role,
		ContractTypeCode:       employee.ContractTypeCode,
		ContractStartDate:      timePtrToDateOnlyPtr(employee.ContractStartDate),
		ContractEndDate:        timePtrToDateOnlyPtr(employee.ContractEndDate),
		ProbationEndDate:       timePtrToDateOnlyPtr(employee.ProbationEndDate),
		LastMedicalCheckupDate: timePtrToDateOnlyPtr(employee.LastMedicalCheckupDate),
		ContractHours:          employee.ContractHours,
		MaxWeeklyHours:         employee.MaxWeeklyHours,
		RequiredRestDays:       employee.RequiredRestDays,
		SundayPremium:          employee.SundayPremium,
		NightPremium:           employee.NightPremium,
		EmployerChargesPct:     employee.EmployerChargesPct,
		HourlyRate:             employee.HourlyRate,
		GrossMonthlySalary:     employee.GrossMonthlySalary,
		TransportCost:          employee.TransportCost,
		HrComment:              employee.HrComment,
	}
}

func dateOnlyPatchFieldToTimePtr(field models.NullableDateOnlyPatchField) *time.Time {
	if !field.Present || field.Value == nil {
		return nil
	}
	value := field.Value.Time()
	return &value
}

func timePtrToDateOnlyPtr(value *time.Time) *models.DateOnly {
	if value == nil {
		return nil
	}
	dateOnly := models.NewDateOnly(*value)
	return &dateOnly
}

func normalizeUsersPagination(page, pageSize int) models.PaginationMetadata {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return models.PaginationMetadata{CurrentPage: page, Limit: pageSize}
}

func buildUsersPaginationMetadata(totalItems int, pagination models.PaginationMetadata) models.PaginationMetadata {
	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + pagination.Limit - 1) / pagination.Limit
	}
	pagination.TotalItems = totalItems
	pagination.TotalPages = totalPages
	return pagination
}

func mapMerchantUserNotFound(err error) error {
	if err == nil {
		return nil
	}
	if err == sql.ErrNoRows {
		return models.ErrMerchantUserNotFound
	}
	return err
}
