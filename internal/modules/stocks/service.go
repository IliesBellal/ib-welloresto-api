package stocks

import (
	"context"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type StocksService struct {
	stocksRepo *StocksRepository
}

func NewStockService(repo *StocksRepository) *StocksService {
	return &StocksService{
		stocksRepo: repo,
	}
}

func (s *StocksService) GetBarcodeInfo(ctx context.Context, token, code string) (*models.BarcodeInfoResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
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
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.stocksRepo.DeleteBarcode(ctx, user.MerchantID, code)
}

func (s *StocksService) CreateBarcode(ctx context.Context, token, code, componentID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.stocksRepo.CreateBarcode(ctx, user.MerchantID, code, componentID)
}

// AddStockBarcode orchestration
func (s *StocksService) AddStockBarcode(ctx context.Context, token, barcode string, specs models.BarcodeSpecs) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.stocksRepo.AddStockBarcode(ctx, user.MerchantID, user.UserID, barcode, specs)
}

func (s *StocksService) SetStockLoss(ctx context.Context, token string, req models.StockLossRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.stocksRepo.SetStockLoss(ctx, user.MerchantID, user.UserID, req)
}

func (s *StocksService) GetStockProducts(ctx context.Context, token, t string) ([]models.StockCategory, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.stocksRepo.GetStockProducts(ctx, user.MerchantID, t)
}

func (s *StocksService) GetComponentsList(ctx context.Context, token string) ([]models.StockComponentListItem, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.stocksRepo.GetComponentsList(ctx, user.MerchantID)
}

func (s *StocksService) RecordComponentMovement(ctx context.Context, req StockComponentMovementRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	if req.Type != "add" && req.Type != "remove" && req.Type != "loss" {
		return ErrInvalidMovement
	}

	return s.stocksRepo.RecordComponentMovement(ctx, user.MerchantID, user.UserID, req)
}
