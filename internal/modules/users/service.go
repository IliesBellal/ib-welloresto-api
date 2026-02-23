package users

import (
	"context"
	"errors"
	"welloresto-api/internal/models"

	"golang.org/x/crypto/bcrypt"
)

type UsersService struct {
	userRepo *UsersRepository
}

func NewUsersService(u *UsersRepository) *UsersService {
	return &UsersService{
		userRepo: u,
	}
}

func (s *UsersService) GetUserLocation(ctx context.Context, token, targetUserID string) (*models.OrderUser, error) {
	// 1. Validate token
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return nil, errors.New("invalid token")
	}

	// 2. Retrieve location
	return s.userRepo.GetUserLocation(ctx, user.MerchantID, targetUserID)
}

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", models.ErrInvalidInput
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func validateNewPassword(password string) error {
	if len(password) < 8 {
		return models.ErrInvalidInputPasswordTooShort
	}
	return nil
}

func (s *UsersService) UpdatePassword(ctx context.Context, token string, oldPass string, newPass string) error {

	// 1. Load user
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	// 2. Validate new password
	if err := validateNewPassword(newPass); err != nil {
		return err
	}

	// 3. Hash password
	hash, err := HashPassword(newPass)
	if err != nil {
		return err
	}

	// 2. Compare old password
	// Nécessite que tous les passwords soient des hash
	// désactivé pour le moment
	/*
		if !CheckPasswordHash(oldPass, user.Password) {
			return fmt.Errorf("invalid_old_password")
		}
	*/

	// 4. Save
	return s.userRepo.UpdatePassword(ctx, user.UserID, user.MerchantID, hash)
}

func (s *UsersService) UpdateUserSettings(ctx context.Context, userID, token string, req *models.UserSettingsRequest) error {

	// TODO
	// Optional validation
	// Valider qu'il s'agit du même user (user_id = token -> User.UserID)
	// Sinon, vérifier que token -> User.UserID est Admin de l'établissement

	return s.userRepo.UpdateUserSettings(ctx, userID, req)
}
