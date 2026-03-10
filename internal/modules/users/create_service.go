package users

import (
	"context"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// CreateUser validates the request, opens a transaction, and persists the new user.
// If merchant_id is provided in the request, automatically links the user to that merchant.
// Returns the generated user_id on success.
func (s *UsersService) CreateUser(ctx context.Context, req CreateUserRequest) (string, error) {
	// --- Validation ---
	if strings.TrimSpace(req.FirstName) == "" ||
		strings.TrimSpace(req.LastName) == "" ||
		strings.TrimSpace(req.UserName) == "" ||
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
	userID, err := helpers.GeneratePrefixedID("user")
	if err != nil {
		return "", err
	}

	userToken, err := helpers.GenerateToken(15) // 30-char token for the users.token column (VARCHAR(30))
	if err != nil {
		return "", err
	}

	// name column = first_name + " " + last_name (legacy field)
	fullName := strings.TrimSpace(req.FirstName) + " " + strings.TrimSpace(req.LastName)

	// --- Transaction ---
	tx, err := s.userRepo.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck — superseded by explicit Commit below

	if err := s.userRepo.CreateUser(ctx, tx, userID, fullName, req.FirstName, req.LastName, req.UserName, req.Email, req.Tel, hashed, userToken); err != nil {
		return "", err
	}

	// Link to merchant if provided
	if req.MerchantID != nil {
		rightsToken, err := helpers.GenerateToken(16) // 32-char token → VARCHAR(255)
		if err != nil {
			return "", err
		}
		if _, err := s.userRepo.InsertUserRights(ctx, tx, userID, *req.MerchantID, req.Admin, rightsToken); err != nil {
			return "", err
		}
	}

	return userID, tx.Commit()
}
