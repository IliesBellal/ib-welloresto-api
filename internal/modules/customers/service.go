package customers

import (
	"context"
	"fmt"
	"math"
	"strings"
	"welloresto-api/internal/helpers"
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
		"status":      "success",
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

func (s *CustomersService) GetLoyaltyPrograms(ctx context.Context, token string) (*LoyaltyProgramsData, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	programs, err := s.customerRepo.GetLoyaltyPrograms(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	return &LoyaltyProgramsData{
		Status:          "success",
		LoyaltyPrograms: programs,
	}, nil
}

func (s *CustomersService) GetLoyaltyProgramByID(ctx context.Context, token, loyaltyProgramID string) (*LoyaltyProgram, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.customerRepo.GetLoyaltyProgramByID(ctx, user.MerchantID, loyaltyProgramID)
}

func (s *CustomersService) CreateLoyaltyProgram(ctx context.Context, token string, req *CreateLoyaltyProgramRequest) (*LoyaltyProgram, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateCreateLoyaltyProgramRequest(req); err != nil {
		return nil, err
	}

	loyaltyProgramID := helpers.GeneratePrefixedID("loyalty-program")
	return s.customerRepo.CreateLoyaltyProgram(ctx, user.MerchantID, loyaltyProgramID, req)
}

func (s *CustomersService) UpdateLoyaltyProgram(ctx context.Context, token, loyaltyProgramID string, req *UpdateLoyaltyProgramRequest) (*LoyaltyProgram, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateUpdateLoyaltyProgramRequest(req); err != nil {
		return nil, err
	}

	return s.customerRepo.UpdateLoyaltyProgram(ctx, user.MerchantID, loyaltyProgramID, req)
}

func (s *CustomersService) DeleteLoyaltyProgram(ctx context.Context, token, loyaltyProgramID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.customerRepo.DeleteLoyaltyProgram(ctx, user.MerchantID, loyaltyProgramID)
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

	return map[string]interface{}{"status": "success", "created_rewards": n}, nil
}

func (s *CustomersService) UpdateLoyaltyReward(ctx context.Context, token string, req *LoyaltyRewardUpdateRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.customerRepo.UpdateLoyaltyReward(ctx, req, user.MerchantID); err != nil {
		return nil, err
	}

	return map[string]interface{}{"status": "success"}, nil
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

	// On accepte de ne pas faire progresser la fidelité, le cycle de vie de la commande est plus important que la fidélité
	return nil
}

func validateCreateLoyaltyProgramRequest(req *CreateLoyaltyProgramRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}

	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}

	if req.Target.Value <= 0 {
		return fmt.Errorf("target.value must be > 0")
	}

	if req.Reward.Value < 0 {
		return fmt.Errorf("reward.value must be >= 0")
	}

	if req.Reward.MinOrderValue < 0 {
		return fmt.Errorf("reward.min_order_value must be >= 0")
	}

	if req.Reward.MaxRewardsPerOrder < 0 {
		return fmt.Errorf("reward.max_rewards_per_order must be >= 0")
	}

	if req.Reward.MaxDiscountValue != nil && *req.Reward.MaxDiscountValue < 0 {
		return fmt.Errorf("reward.max_discount_value must be >= 0")
	}

	return nil
}

func validateUpdateLoyaltyProgramRequest(req *UpdateLoyaltyProgramRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}

	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if req.Target != nil && req.Target.Value != nil && *req.Target.Value <= 0 {
		return fmt.Errorf("target.value must be > 0")
	}

	if req.Reward != nil {
		if req.Reward.Value != nil && *req.Reward.Value < 0 {
			return fmt.Errorf("reward.value must be >= 0")
		}
		if req.Reward.MinOrderValue != nil && *req.Reward.MinOrderValue < 0 {
			return fmt.Errorf("reward.min_order_value must be >= 0")
		}
		if req.Reward.MaxRewardsPerOrder != nil && *req.Reward.MaxRewardsPerOrder < 0 {
			return fmt.Errorf("reward.max_rewards_per_order must be >= 0")
		}
		if req.Reward.MaxDiscountValue != nil && *req.Reward.MaxDiscountValue < 0 {
			return fmt.Errorf("reward.max_discount_value must be >= 0")
		}
	}

	return nil
}
