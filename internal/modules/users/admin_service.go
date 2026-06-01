package users

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
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
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	if err := s.userRepo.UpdatePassword(ctx, userID, currentUser.MerchantID, hash); err != nil {
		return err
	}
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
	if result.Unlinked && s.audit != nil && oldState != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "unlink_merchant_user", "merchant_user", userID, oldState, map[string]any{"enabled": false, "employee_links_cleared": result.EmployeeLinksCleared})
	}
	return result, nil
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
