package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type ServicesService struct {
	userRepo     *repositories.UsersRepository
	servicesRepo *repositories.ServicesRepository
}

func NewServicesService(d *repositories.ServicesRepository, u *repositories.UsersRepository) *ServicesService {
	return &ServicesService{userRepo: u, servicesRepo: d}
}

func (s *ServicesService) GetCurrentService(ctx context.Context, token string, deviceID string) (*models.CurrentServiceResponse, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.servicesRepo.GetCurrentService(ctx, user.UserID, deviceID)
}
