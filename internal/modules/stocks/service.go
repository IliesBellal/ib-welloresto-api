package stocks

import (
	"context"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type StocksService struct {
	usersRepo  auth.AuthService
	stocksRepo *StocksRepository
}

func NewStockService(repo *StocksRepository, u auth.AuthService) *StocksService {
	return &StocksService{
		stocksRepo: repo,
		usersRepo:  u,
	}
}

func (s *StocksService) GetBarcodeInfo(ctx context.Context, token, code string) (*models.BarcodeInfoResponse, error) {
	user, err := s.usersRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	info, uoms, err := s.stocksRepo.GetBarcodeInfo(ctx, user.MerchantID, code)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return &models.BarcodeInfoResponse{Status: "0"}, nil
	}

	return &models.BarcodeInfoResponse{
		Status:       "1",
		Component:    info,
		AvailableUOM: uoms,
		Code:         code,
	}, nil
}

func (s *StocksService) DeleteBarcode(ctx context.Context, token, code string) error {
	user, err := s.usersRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	return s.stocksRepo.DeleteBarcode(ctx, user.MerchantID, code)
}

func (s *StocksService) CreateBarcode(ctx context.Context, token, code, componentID string) error {
	user, err := s.usersRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	return s.stocksRepo.CreateBarcode(ctx, user.MerchantID, code, componentID)
}

// AddStockBarcode orchestration
func (s *StocksService) AddStockBarcode(ctx context.Context, token, barcode string, specs models.BarcodeSpecs) error {
	user, err := s.usersRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	return s.stocksRepo.AddStockBarcode(ctx, user.MerchantID, user.UserID, barcode, specs)
}

func (s *StocksService) SetStockLoss(ctx context.Context, token string, req models.StockLossRequest) error {
	user, err := s.usersRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	return s.stocksRepo.SetStockLoss(ctx, user.MerchantID, user.UserID, req)
}

func (s *StocksService) GetStockProducts(ctx context.Context, token, t string) ([]models.StockCategory, error) {
	user, err := s.usersRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.stocksRepo.GetStockProducts(ctx, user.MerchantID, t)
}
