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

	// --- Hash password ---
	hashed, err := HashPassword(req.Password)
	if err != nil {
		return "", err
	}

	// --- Generate IDs & tokens ---
	userID := helpers.GeneratePrefixedID(helpers.UserIDPrefix)

	userToken, err := helpers.GenerateToken(30) // 30-char token for the users.token column (VARCHAR(30))
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

	err = dbutils.RunInTx(ctx, s.userRepo.database, func(txCtx context.Context) error {
		if createErr := s.userRepo.CreateUser(txCtx, userID, fullName, req.FirstName, req.LastName, req.UserName, req.Email, req.Tel, hashed, userToken); createErr != nil {
			return createErr
		}

		if merchantID == "" {
			return nil
		}

		rightsToken, tokenErr := helpers.GenerateToken(30) // 30-char token → VARCHAR(255)
		if tokenErr != nil {
			return tokenErr
		}

		_, insertErr := s.userRepo.UpsertMerchantUserRights(txCtx, userID, merchantID, rightsToken, rights)
		return insertErr
	})
	if err != nil {
		return "", err
	}

	return userID, nil
}
