package helpers

import (
	"strings"

	"welloresto-api/internal/models"

	"golang.org/x/crypto/bcrypt"
)

const (
	// PasswordBcryptCost is the bcrypt cost for every user-facing password hash
	// (creation, self-service change, admin force-reset, forgot-password reset).
	//
	// Note: HashPassword in services_helpers.go uses bcrypt.DefaultCost (10) and
	// is kept as-is because it sits on the login legacy-upgrade path. New code
	// should use HashUserPassword so that all four password entry points agree.
	PasswordBcryptCost = 12

	// PasswordMinLength is the shared minimum length policy.
	PasswordMinLength = 8
)

// ValidatePassword enforces the shared password policy. Single source of truth
// for every module that accepts a new password.
func ValidatePassword(password string) error {
	if len(password) < PasswordMinLength {
		return models.ErrInvalidInputPasswordTooShort
	}
	return nil
}

// HashUserPassword hashes a user password at the project's standard cost.
func HashUserPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", models.ErrInvalidInput
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordBcryptCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}
