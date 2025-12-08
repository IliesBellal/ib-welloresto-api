package user_services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type ServicesService struct {
	userRepo     auth.AuthService
	servicesRepo *ServicesRepository
}

func NewServicesService(d *ServicesRepository, u auth.AuthService) *ServicesService {
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
