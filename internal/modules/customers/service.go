package customers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/customers/importer"
)

type CustomersService struct {
	customerRepo *CustomersRepository
	auditService audit.AuditService
}

func NewCustomersService(_customerRepo *CustomersRepository, _auditService audit.AuditService) *CustomersService {
	return &CustomersService{
		customerRepo: _customerRepo,
		auditService: _auditService,
	}
}

func (s *CustomersService) UpdateOrCreateCustomer(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	// TODO: Implémentation complète plus tard
	return map[string]interface{}{
		"status":      "success",
		"customer_id": params["customer_id"],
	}, nil
}

// GetCustomerByID récupère un client par son ID, scopé au merchant. Retourne sql.ErrNoRows si introuvable.
func (s *CustomersService) GetCustomerByID(ctx context.Context, merchantID, customerID string) (*models.Customer, error) {
	return s.customerRepo.GetCustomerByID(ctx, customerID, merchantID)
}

// FindCustomerByEmail recherche un client existant par email (insensible à la casse). Retourne sql.ErrNoRows si introuvable.
func (s *CustomersService) FindCustomerByEmail(ctx context.Context, merchantID, email string) (*models.Customer, error) {
	return s.customerRepo.FindCustomerByEmail(ctx, email, merchantID)
}

// UpsertCustomer crée ou met à jour partiellement un client (seuls les champs non-vides du modèle sont écrits).
func (s *CustomersService) UpsertCustomer(ctx context.Context, c *models.Customer) (*string, error) {
	return s.customerRepo.UpdateOrCreateCustomer(ctx, c)
}

// CreateCustomer crée un client unique (endpoint POST /customers), en
// réutilisant la validation de la saisie manuelle en masse
// (importer.BuildManualCustomerImport, mêmes règles que "Saisir plusieurs
// clients" côté front) et le mapping canonique->models.Customer déjà écrit
// pour le commit d'import (importer.BuildCommitCustomer).
//
// Si un client existant du même marchand partage l'email ou le téléphone
// fourni, il est mis à jour (partiellement — les champs non fournis restent
// intacts) plutôt que d'échouer : décision produit alignée sur ce que fait
// déjà le commit d'import en cas de doublon détecté.
func (s *CustomersService) CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*string, bool, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, false, err
	}

	parsed, err := importer.BuildManualCustomerImport([]importer.ManualCustomerInput{req.toManualCustomerInput()})
	if err != nil {
		return nil, false, err
	}
	canonical := parsed.Customers[0]

	customer := importer.BuildCommitCustomer(user.MerchantID, canonical)

	existingID, err := s.findExistingCustomerID(ctx, user.MerchantID, canonical.Email, canonical.Phone)
	if err != nil {
		return nil, false, err
	}

	created := existingID == nil
	if !created {
		customer.CustomerID = existingID
	}

	id, err := s.customerRepo.UpdateOrCreateCustomer(ctx, &customer)
	if err != nil {
		return nil, false, err
	}

	if s.auditService != nil {
		action := "customer_created"
		if !created {
			action = "customer_updated"
		}
		_ = s.auditService.LogChange(ctx, user.MerchantID, user.UserID, action, models.ResourceCustomer, *id, nil, customer)
	}

	return id, created, nil
}

// findExistingCustomerID cherche un client déjà rattaché au marchand par
// email (priorité) puis par téléphone. Retourne (nil, nil) si aucune
// collision — sql.ErrNoRows n'est pas une erreur ici.
func (s *CustomersService) findExistingCustomerID(ctx context.Context, merchantID string, email, phone *string) (*string, error) {
	if email != nil {
		existing, err := s.customerRepo.FindCustomerByEmail(ctx, *email, merchantID)
		if err == nil {
			return existing.CustomerID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	if phone != nil {
		existing, err := s.customerRepo.FindCustomerByPhone(ctx, *phone, merchantID)
		if err == nil {
			return existing.CustomerID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	return nil, nil
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

	loyaltyProgramID := helpers.GeneratePrefixedID("loyal-prog")
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
