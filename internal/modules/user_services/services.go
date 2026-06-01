package user_services

import (
	"context"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type ServicesService struct {
	servicesRepo *ServicesRepository
}

func NewServicesService(d *ServicesRepository) *ServicesService {
	return &ServicesService{servicesRepo: d}
}

func (s *ServicesService) GetCurrentService(ctx context.Context, token string, deviceID string) (*models.CurrentServiceResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.servicesRepo.GetCurrentService(ctx, user.UserID, user.MerchantID, deviceID)
}
