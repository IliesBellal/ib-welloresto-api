package users

import (
	"context"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// CreateUser validates the request, opens a transaction, and persists the new user.
// If merchant_id is provided in the request, automatically links the user to that merchant.
// Returns the generated user_id on success.
func (s *UsersService) CreateUser(ctx context.Context, req CreateUserRequest) (string, error) {
	// --- Validation ---
	if strings.TrimSpace(req.FirstName) == "" ||
		strings.TrimSpace(req.LastName) == "" ||
		// strings.TrimSpace(req.UserName) == "" ||
		strings.TrimSpace(req.Email) == "" {
		return "", models.ErrInvalidInput
	}

	if err := validateNewPassword(req.Password); err != nil {
		return "", err
	}

	// --- Reject duplicate email before doing any work (uq_users_email_lower,
	// migration 124) — the INSERT itself still falls back to the same check
	// for the concurrent case (create_repository.go, CreateUser).
	exists, err := s.userRepo.EmailExists(ctx, req.Email)
	if err != nil {
		return "", err
	}
	if exists {
		return "", models.ErrEmailAlreadyUsed
	}

	// --- Hash password ---
	hashed, err := HashPassword(req.Password)
	if err != nil {
		return "", err
	}

	// --- Generate IDs & tokens ---
	userID := helpers.GeneratePrefixedID(helpers.UserIDPrefix)

	userToken, err := helpers.GenerateToken(30) // 30 bytes -> 60 hex chars, fits users.token (VARCHAR(64))
	if err != nil {
		return "", err
	}

	// name column = first_name + " " + last_name (legacy field)
	fullName := strings.TrimSpace(req.FirstName) + " " + strings.TrimSpace(req.LastName)

	merchantID := ""
	if currentUser, err := middleware.UserFromContext(ctx); err == nil {
		merchantID = strings.TrimSpace(currentUser.MerchantID)
	} else if req.MerchantID != nil {
		merchantID = strings.TrimSpace(*req.MerchantID)
	}
	rights := defaultMerchantUserRights(req.Admin)
	if req.Rights != nil {
		rights = req.Rights.Normalize(defaultMerchantUserRights(req.Admin))
	}
	if req.RoleID != nil && strings.TrimSpace(*req.RoleID) != "" {
		if merchantID == "" {
			return "", models.ErrInvalidInput
		}
		belongs, err := s.userRepo.RoleBelongsToMerchant(ctx, merchantID, strings.TrimSpace(*req.RoleID))
		if err != nil {
			return "", err
		}
		if !belongs {
			return "", models.ErrRoleNotFound
		}
		rights.RoleID = req.RoleID
	}

	err = dbutils.RunInTx(ctx, s.userRepo.database, func(txCtx context.Context) error {
		// A staff member added by an admin never sees a consent screen —
		// terms/marketing are a public-signup concept (LOT A Semaine 3,
		// Chantier 14), not applicable here.
		if createErr := s.userRepo.CreateUser(txCtx, userID, fullName, req.FirstName, req.LastName, req.Email, req.Tel, hashed, userToken, false, false); createErr != nil {
			return createErr
		}

		if merchantID == "" {
			return nil
		}

		rightsToken, tokenErr := helpers.GenerateToken(30) // 30 bytes -> 60 hex chars, fits users_rights.token (VARCHAR(255))
		if tokenErr != nil {
			return tokenErr
		}

		_, insertErr := s.userRepo.UpsertMerchantUserRights(txCtx, userID, merchantID, rightsToken, rights)
		return insertErr
	})
	if err != nil {
		return "", err
	}

	if merchantID != "" && s.onboarding != nil {
		s.onboarding.RecomputeOnboardingBestEffort(ctx, merchantID)
	}

	return userID, nil
}
