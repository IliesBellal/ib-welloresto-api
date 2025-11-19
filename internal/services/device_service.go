package services

import (
	"context"
	"errors"
	"welloresto-api/internal/repositories"
)

type DeviceService struct {
	userRepo   *repositories.UserRepository
	deviceRepo *repositories.DeviceRepository
}

func NewDeviceService(u *repositories.UserRepository, d *repositories.DeviceRepository) *DeviceService {
	return &DeviceService{userRepo: u, deviceRepo: d}
}

func (s *DeviceService) SaveDeviceToken(ctx context.Context, token, deviceToken, deviceID, app string) (map[string]string, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	err = s.deviceRepo.SaveDevice(ctx, user.UserID, user.MerchantID, app, deviceID, deviceToken)
	if err != nil {
		return map[string]string{
			"status": "-3",
			"error":  err.Error(),
		}, nil
	}

	return map[string]string{"status": "1"}, nil
}
