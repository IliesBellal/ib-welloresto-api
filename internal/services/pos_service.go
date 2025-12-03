package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"

	"welloresto-api/internal/repositories"
)

type POSService struct {
	userRepo *repositories.UsersRepository
	posRepo  *repositories.POSRepository
}

func NewPOSService(u *repositories.UsersRepository, p *repositories.POSRepository) *POSService {
	return &POSService{userRepo: u, posRepo: p}
}

func (s *POSService) GetPOSStatus(ctx context.Context, token string) (*models.POSStatus, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return nil, errors.New("invalid_token")
	}

	return s.posRepo.GetPOSStatus(ctx, user.MerchantID)
}

func (s *POSService) UpdatePOSStatus(ctx context.Context, token string, status bool) (*models.POSStatus, error) {
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

func (s *POSService) GetDeletionReasons(ctx context.Context, object string) ([]models.DeletionReason, error) {
	return s.posRepo.GetDeletionReasons(ctx, object)
}

func (s *POSService) ToggleScanNOrder(ctx context.Context, token, status string) (int64, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return 0, errors.New("invalid_token")
	}

	return s.posRepo.ToggleScanNOrder(ctx, user.MerchantID, status)
}

func (s *POSService) ToggleProductionPaidOnly(ctx context.Context, token, status string) (int64, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return 0, errors.New("invalid_token")
	}

	return s.posRepo.ToggleProductionPaidOnly(ctx, user.MerchantID, status)
}

func (s *POSService) ToggleSafetyStock(ctx context.Context, token, status string) (int64, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return 0, errors.New("invalid_token")
	}

	return s.posRepo.ToggleSafetyStock(ctx, user.MerchantID, status)
}

func (s *POSService) GetDeliveryMen(ctx context.Context, token string) ([]models.DeliveryMan, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return nil, errors.New("invalid_token")
	}

	return s.posRepo.GetDeliveryMen(ctx, user.MerchantID)
}
