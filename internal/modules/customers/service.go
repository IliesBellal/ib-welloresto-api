package customers

import (
	"context"
	"math"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
)

type CustomersService struct {
	customerRepo *CustomersRepository
}

func NewCustomersService(_customerRepo *CustomersRepository) *CustomersService {
	return &CustomersService{
		customerRepo: _customerRepo,
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
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.customerRepo.GetCustomerLoyalty(ctx, customerID, user.MerchantID)
}

func (s *CustomersService) UpdateLoyaltyProgress(ctx context.Context, token string, req *LoyaltyProgressUpdateRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	n, err := s.customerRepo.UpdateLoyaltyProgress(ctx, req, user.MerchantID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{"status": "1", "created_rewards": n}, nil
}

func (s *CustomersService) UpdateLoyaltyReward(ctx context.Context, token string, req *LoyaltyRewardUpdateRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.customerRepo.UpdateLoyaltyReward(ctx, req, user.MerchantID); err != nil {
		return nil, err
	}

	return map[string]interface{}{"status": "1"}, nil
}

func (s *CustomersService) SearchCustomers(ctx context.Context, token, term, sortField, sortDir string, page, pageSize int) (*CustomerListData, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	customers, totalItems, err := s.customerRepo.SearchCustomers(ctx, user.MerchantID, term, sortField, sortDir, page, pageSize)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(pageSize)))
	}

	return &CustomerListData{
		Metadata: CustomerPaginationMetadata{
			TotalItems:  totalItems,
			TotalPages:  totalPages,
			CurrentPage: page,
			Limit:       pageSize,
		},
		Customers: customers,
	}, nil
}

func (s *CustomersService) ListCustomers(ctx context.Context, token, sortField, sortDir string, page, pageSize int) (*CustomerListData, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	customers, totalItems, err := s.customerRepo.ListCustomers(ctx, user.MerchantID, sortField, sortDir, page, pageSize)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(pageSize)))
	}

	return &CustomerListData{
		Metadata: CustomerPaginationMetadata{
			TotalItems:  totalItems,
			TotalPages:  totalPages,
			CurrentPage: page,
			Limit:       pageSize,
		},
		Customers: customers,
	}, nil
}

func (s *CustomersService) ReactivateRewards(ctx context.Context, orderID string) error {
	return s.customerRepo.ReactivateRewards(ctx, orderID)
}

func (s *CustomersService) ProcessOrderLoyalty(ctx context.Context, orderID string) error {
	log := logger.FromContext(ctx)

	err := s.customerRepo.UpdateLoyaltyFromOrder(ctx, orderID)
	if err != nil {
		log.Error("Erreur lors de la mise à jour de la fidélité pour la commande " + orderID + " : " + err.Error())
	}

	return nil
}
