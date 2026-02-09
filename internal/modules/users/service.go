package users

import (
	"context"
	"errors"
	"fmt"
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
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *UsersService) UpdatePassword(ctx context.Context, token string, oldPass string, newPass string) error {

	// 1. Load user
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("invalid token")
	}

	// 2. Compare old password
	if !CheckPasswordHash(oldPass, user.Password) {
		return fmt.Errorf("invalid_old_password")
	}

	// 3. Hash new password
	hash, err := HashPassword(newPass)
	if err != nil {
		return fmt.Errorf("hash_error")
	}

	// 4. Save
	return s.userRepo.UpdatePassword(ctx, user.UserID, hash)
}

func (s *UsersService) UpdateUserSettings(ctx context.Context, userID, token string, req *models.UserSettingsRequest) error {

	// TODO
	// Optional validation
	// Valider qu'il s'agit du même user (user_id = token -> User.UserID)
	// Sinon, vérifier que token -> User.UserID est Admin de l'établissement

	return s.userRepo.UpdateUserSettings(ctx, userID, req)
}
