package services

import (
	"context"
	"errors"
	"strconv"
	"time"
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

func (s *POSService) CheckTR(ctx context.Context, token, code string) (*models.TRCheckResponse, error) {

	// 1) Authentication
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	// --- Parse code ---
	if len(code) != 20 {
		return &models.TRCheckResponse{
			Status:  "invalid_format",
			Message: "TR code must be 20 digits long",
			Code:    code,
		}, nil
	}

	// extract parts
	id := code[:11]

	valueInt, err := strconv.Atoi(code[11:16])
	if err != nil {
		return nil, errors.New("invalid TR value")
	}
	value := float64(valueInt) / 100.0

	vintage, err := strconv.Atoi(code[16:])
	if err != nil {
		return nil, errors.New("invalid TR vintage")
	}

	if value == 0 {
		return &models.TRCheckResponse{
			Status:  "no_value",
			Message: "Value cannot be 0",
			Code:    code,
			ID:      id,
			Value:   value,
			Vintage: vintage,
		}, nil
	}

	// --- Check if TR already used ---
	used, err := s.posRepo.IsTicketUsed(ctx, code)
	if err != nil {
		return nil, err
	}
	if used {
		return &models.TRCheckResponse{
			Status:  "used",
			Message: "TR already used",
			Code:    code,
			ID:      id,
			Value:   value,
			Vintage: vintage,
		}, nil
	}

	// --- Expiration logic ---
	now := time.Now().UTC()

	expiry := time.Date(vintage+1, 1, 31, 0, 0, 0, 0, time.UTC)
	validFrom := time.Date(vintage-1, 12, 1, 0, 0, 0, 0, time.UTC)

	if now.After(expiry) || now.Before(validFrom) {
		return &models.TRCheckResponse{
			Status:  "expired",
			Message: "TR is expired",
			Code:    code,
			ID:      id,
			Value:   value,
			Vintage: vintage,
		}, nil
	}

	// --- VALID ---
	return &models.TRCheckResponse{
		Status:  "valid",
		Message: "TR can be used",
		Code:    code,
		ID:      id,
		Value:   value,
		Vintage: vintage,
	}, nil
}
