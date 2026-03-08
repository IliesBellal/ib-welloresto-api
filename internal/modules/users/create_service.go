package users

import (
	"context"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// CreateUser validates the request, opens a transaction, and persists the new user.
// Returns the generated user_id on success.
func (s *UsersService) CreateUser(ctx context.Context, req CreateUserRequest) (string, error) {
	// --- Validation ---
	if strings.TrimSpace(req.FirstName) == "" ||
		strings.TrimSpace(req.LastName) == "" ||
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

	req.UserID = userID

	userToken, err := helpers.GenerateToken(30) // 30-char token for the users.token column (VARCHAR(30))
	if err != nil {
		return "", err
	}

	// --- Transaction ---
	tx, err := s.userRepo.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck — superseded by explicit Commit below

	if err := s.userRepo.CreateUser(ctx, tx, req, hashed, userToken); err != nil {
		return "", err
	}

	return userID, tx.Commit()
}
