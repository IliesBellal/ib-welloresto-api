package services

import (
	"context"
	"errors"
	"welloresto-api/internal/repositories"
)

type CashDrawerService struct {
	cashDrawerRepo *repositories.CashDrawerRepository
	userRepo       *repositories.UserRepository // used to resolve token -> merchant id
}

func NewCashDrawerService(cashDrawerRepo *repositories.CashDrawerRepository, userRepo *repositories.UserRepository) *CashDrawerService {
	return &CashDrawerService{
		cashDrawerRepo: cashDrawerRepo,
		userRepo:       userRepo,
	}
}

func (s *CashDrawerService) OpenDrawer(ctx context.Context, token string, deviceID string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return errors.New("invalid token")
	}

	return s.cashDrawerRepo.OpenCashDrawer(ctx, user.UserID, deviceID)
}
