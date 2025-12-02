package services

import (
	"context"
	"welloresto-api/internal/repositories"
)

type CustomersService struct {
	customerRepo *repositories.CustomerRepository
}

func NewCustomersService(_customerRepo *repositories.CustomerRepository) *CustomersService {
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
