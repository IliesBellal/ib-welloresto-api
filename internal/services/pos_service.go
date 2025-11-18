package services

import (
	"context"
	"errors"

	"welloresto-api/internal/repositories"
)

type POSService struct {
	userRepo *repositories.UserRepository
	posRepo  *repositories.POSRepository
}

func NewPOSService(u *repositories.UserRepository, p *repositories.POSRepository) *POSService {
	return &POSService{userRepo: u, posRepo: p}
}

func (s *POSService) GetPOSStatus(ctx context.Context, token string) (*repositories.POSStatus, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return nil, errors.New("invalid_token")
	}

	return s.posRepo.GetPOSStatus(ctx, user.MerchantID)
}

func (s *POSService) UpdatePOSStatus(ctx context.Context, token string, status bool) (*repositories.POSStatus, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return nil, errors.New("invalid_token")
	}

	if !user.AccessReception {
		return nil, errors.New("not_allowed")
	}

	err = s.posRepo.UpdatePOSStatus(ctx, user.UserID, status)
	if err != nil {
		return nil, err
	}

	return s.posRepo.GetPOSStatus(ctx, user.MerchantID)
}
