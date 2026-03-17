package cash_registers

import (
	"context"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type CashRegisterService struct {
	cashRegisterRepo *CashRegisterRepository
	userRepo         auth.AuthService
}

func NewCashRegisterService(cashRegisterRepo *CashRegisterRepository, userRepo auth.AuthService) *CashRegisterService {
	return &CashRegisterService{
		cashRegisterRepo: cashRegisterRepo,
		userRepo:         userRepo,
	}
}

func (s *CashRegisterService) OpenCashRegister(ctx context.Context, token string, req *models.OpenCashRegisterRequest) (*models.CashRegisterOpenResponse, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
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
		return nil, models.ErrUnauthorized
	}

	// userID fourni dans la requête → OK (comme PHP)
	return s.cashRegisterRepo.CloseCashRegister(ctx, cashRegisterID, user.MerchantID, req)
}

func (s *CashRegisterService) GetCashRegisterSummary(ctx context.Context, token string, cashRegisterID string) (*models.CashRegisterSummaryResponse, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.cashRegisterRepo.GetCashRegisterSummary(ctx, cashRegisterID, user.MerchantID)
}

func (s *CashRegisterService) GetCashRegisterTVADetails(ctx context.Context, token string, cashRegisterID string) (*models.CashRegisterDetails, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.cashRegisterRepo.GetCashRegisterTVADetails(ctx, user.MerchantID, cashRegisterID)
}

func (s *CashRegisterService) AddCustomItem(ctx context.Context, token string, id string, req *models.AddCustomItemRequest) (interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	itemID, err := s.cashRegisterRepo.AddCustomItem(ctx, id, req.Label, req.Value)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register " + id + " closed."}, nil
		}
		return nil, err
	}

	return models.HandlerDefaultResponseModelSet{
		Status: "success",
		Data1:  itemID,
	}, nil
}

func (s *CashRegisterService) DeleteCustomItem(ctx context.Context, token string, id string, itemID string) (map[string]interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	err = s.cashRegisterRepo.DeleteCustomItem(ctx, id, itemID)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register " + id + " closed."}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"status": "1"}, nil
}

func (s *CashRegisterService) EncloseCashRegister(ctx context.Context, id, token, comment string) (map[string]interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	err = s.cashRegisterRepo.EncloseCashRegister(ctx, user.UserID, id, comment)
	if err != nil {
		if err.Error() == "cash_register_closed" {
			return map[string]interface{}{"status": "-1", "error": "Cash register closed."}, nil
		}
		return nil, err
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *CashRegisterService) GetCashRegisterHistory(ctx context.Context, req models.OrderHistoryRequest) ([]models.CashRegister, error) {
	// Récupérer l'utilisateur depuis le contexte
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.cashRegisterRepo.GetCashRegisterHistory(ctx, user.MerchantID, user.UserID, req)
}

func (s *CashRegisterService) LinkDevice(ctx context.Context, req DeviceLinkRequest) error {
	// Récupérer l'utilisateur depuis le contexte
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	// Logique métier additionnelle si nécessaire
	err = s.cashRegisterRepo.UpsertDeviceLink(ctx, req.DeviceID, user.UserID, req.OnBehalfOf)
	if err != nil {
		return models.ErrInternalServerError
	}
	return nil
}
