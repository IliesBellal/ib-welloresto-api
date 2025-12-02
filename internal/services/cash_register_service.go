package services

import (
	"context"
	"errors"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type CashRegisterService struct {
	cashRegisterRepo *repositories.CashRegisterRepository
	userRepo         *repositories.UserRepository // used to resolve token -> merchant id
}

func NewCashRegisterService(cashRegisterRepo *repositories.CashRegisterRepository, userRepo *repositories.UserRepository) *CashRegisterService {
	return &CashRegisterService{
		cashRegisterRepo: cashRegisterRepo,
		userRepo:         userRepo,
	}
}

func (s *CashRegisterService) OpenCashRegister(ctx context.Context, token string, req *models.OpenCashRegisterRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	// merchantID via user
	// userID vient de req (comme en PHP)
	req.CashRegister.UserID = user.UserID

	return s.cashRegisterRepo.OpenCashRegister(ctx, req, user.MerchantID)
}

func (s *CashRegisterService) CloseCashRegister(ctx context.Context, token string, cashRegisterID string, req *models.CloseCashRegisterRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	// userID fourni dans la requête → OK (comme PHP)
	return s.cashRegisterRepo.CloseCashRegister(ctx, cashRegisterID, user.MerchantID, req)
}

func (s *CashRegisterService) GetCashRegisterSummary(ctx context.Context, token string, cashRegisterID string) (*models.CashRegisterSummaryResponse, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return nil, errors.New("invalid token")
	}

	return s.cashRegisterRepo.GetCashRegisterSummary(ctx, cashRegisterID, user.MerchantID)
}

func (s *CashRegisterService) AddCustomItem(ctx context.Context, id string, req *models.AddCustomItemRequest) (map[string]interface{}, error) {
	itemID, err := s.cashRegisterRepo.AddCustomItem(ctx, id, req.Label, req.Value)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register " + id + " closed."}, nil
		}
		return nil, err
	}

	return map[string]interface{}{
		"status": "1",
		"data1":  itemID,
	}, nil
}

func (s *CashRegisterService) DeleteCustomItem(ctx context.Context, id string, itemID string) (map[string]interface{}, error) {
	err := s.cashRegisterRepo.DeleteCustomItem(ctx, id, itemID)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register closed."}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"status": "1"}, nil
}

func (s *CashRegisterService) EncloseCashRegister(ctx context.Context, id, userID, comment string) (map[string]interface{}, error) {
	err := s.cashRegisterRepo.EncloseCashRegister(ctx, userID, id, comment)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register closed."}, nil
		}
		return nil, err
	}

	return map[string]interface{}{"status": "1"}, nil
}
