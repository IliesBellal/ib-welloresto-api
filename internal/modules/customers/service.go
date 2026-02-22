package customers

import (
	"context"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type CustomersService struct {
	customerRepo *CustomersRepository
	userRepo     auth.AuthService
}

func NewCustomersService(_customerRepo *CustomersRepository, u auth.AuthService) *CustomersService {
	return &CustomersService{
		customerRepo: _customerRepo,
		userRepo:     u,
	}
}

func (s *CustomersService) UpdateOrCreateCustomer(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	// TODO: Implémentation complète plus tard
	return map[string]interface{}{
		"status":      "1",
		"customer_id": params["customer_id"],
	}, nil
}

func (s *CustomersService) GetCustomerLoyalty(ctx context.Context, token, customerID string) (*CustomerLoyalty, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.customerRepo.GetCustomerLoyalty(ctx, customerID)
}

func (s *CustomersService) UpdateLoyaltyProgress(ctx context.Context, token string, req *LoyaltyProgressUpdateRequest) (map[string]interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	n, err := s.customerRepo.UpdateLoyaltyProgress(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{"status": "1", "created_rewards": n}, nil
}

func (s *CustomersService) UpdateLoyaltyReward(ctx context.Context, token string, req *LoyaltyRewardUpdateRequest) (map[string]interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	if err := s.customerRepo.UpdateLoyaltyReward(ctx, req); err != nil {
		return nil, err
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *CustomersService) SearchCustomers(ctx context.Context, token, term string) ([]CustomerSearchResult, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrUnauthorized
	}

	return s.customerRepo.SearchCustomers(ctx, user.MerchantID, term)
}

func (s *CustomersService) ReactivateRewards(ctx context.Context, orderID string) error {
	return s.customerRepo.ReactivateRewards(ctx, orderID)
}
