package services

import (
	"context"
	"errors"
	"log"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type MenuService struct {
	userRepo *repositories.UserRepository // uses your existing interface
	legacy   *repositories.LegacyMenuRepository
	opt      *repositories.OptimizedMenuRepository
	// choose repo via config; for now use both
	useOptimized bool
}

func NewMenuService(userRepo *repositories.UserRepository, legacy *repositories.LegacyMenuRepository, opt *repositories.OptimizedMenuRepository, useOptimized bool) *MenuService {
	return &MenuService{
		userRepo:     userRepo,
		legacy:       legacy,
		opt:          opt,
		useOptimized: useOptimized,
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

	// Branch: check last_menu_update early (both repos implement check)
	if s.useOptimized {
		log.Printf("MenuRepository: using OPTIMIZED mode")
		return s.opt.GetMenu(ctx, user.MerchantID, lastMenu)
	}
	log.Printf("MenuRepository: using LEGACY mode")
	return s.legacy.GetMenu(ctx, user.MerchantID, lastMenu)
}
