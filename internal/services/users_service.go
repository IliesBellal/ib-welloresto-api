package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type UsersService struct {
	userRepo *repositories.UsersRepository
}

func NewUsersService(u *repositories.UsersRepository) *UsersService {
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
