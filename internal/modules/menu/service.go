package menu

import (
	"context"
	"errors"
	"log"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type MenuService struct {
	userRepo auth.AuthService // uses your existing interface
	legacy   *MenuRepository
}

func NewMenuService(legacy *MenuRepository, userRepo auth.AuthService) *MenuService {
	return &MenuService{
		userRepo: userRepo,
		legacy:   legacy,
	}
}

func (s *MenuService) GetMenu(ctx context.Context, token string, lastMenu *time.Time) (*models.MenuResponse, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	log.Printf("MenuRepository: using LEGACY mode")
	return s.legacy.GetMenu(ctx, user.MerchantID, lastMenu)
}

func (s *MenuService) GetAttributes(ctx context.Context, token string) (interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	log.Printf("MenuRepository: using LEGACY mode")
	return s.legacy.GetAttributes(ctx, user.MerchantID)
}

func (s *MenuService) SetComponentAvailability(ctx context.Context, token, cid, status string) (int64, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return 0, err
	}
	if user == nil {
		return 0, errors.New("invalid token")
	}

	return s.legacy.SetComponentAvailability(ctx, user.MerchantID, cid, status)
}

func (s *MenuService) SetProductAvailability(ctx context.Context, token, pid, status string) (int64, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return 0, err
	}
	if user == nil {
		return 0, errors.New("invalid token")
	}

	return s.legacy.SetProductAvailability(ctx, user.MerchantID, pid, status)
}
