package services

import (
	"context"
	"errors"
	"strconv"
	"welloresto-api/internal/repositories"
)

type AppVersionService struct {
	repo     *repositories.AppVersionRepository
	userRepo *repositories.UserRepository
}

func NewAppVersionService(r *repositories.AppVersionRepository, u *repositories.UserRepository) *AppVersionService {
	return &AppVersionService{repo: r, userRepo: u}
}

func (s *AppVersionService) CheckAppVersion(ctx context.Context, token, versionCodeString, app string) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	versionCode, err := strconv.Atoi(versionCodeString)
	if err != nil {
		return nil, errors.New("invalid version number")
	}

	// Business logic
	result, err := s.repo.CheckAppVersion(ctx, versionCode, app, user.MerchantID)
	if err != nil {
		return nil, err
	}

	return result, nil
}
