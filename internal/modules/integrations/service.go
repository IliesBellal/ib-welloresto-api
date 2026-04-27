package integrations

import (
	"context"
	"database/sql"
	"errors"

	stripeclient "welloresto-api/internal/infrastructure/stripe"
)

type Service struct {
	repo          *Repository
	stripeManager *stripeclient.StripeManager
	// Stripe Connect redirect URLs (loaded from config).
	stripeReturnURL  string
	stripeRefreshURL string
}

func NewService(db *sql.DB, stripeManager *stripeclient.StripeManager, stripeReturnURL, stripeRefreshURL string) *Service {
	return &Service{
		repo:             NewRepository(db),
		stripeManager:    stripeManager,
		stripeReturnURL:  stripeReturnURL,
		stripeRefreshURL: stripeRefreshURL,
	}
}

func (s *Service) GetUberEats(ctx context.Context, merchantID string) (*UberEatsIntegration, error) {
	return s.repo.GetUberEatsIntegration(ctx, merchantID)
}

func (s *Service) GetDeliveroo(ctx context.Context, merchantID string) (*DeliverooIntegration, error) {
	return s.repo.GetDeliverooIntegration(ctx, merchantID)
}

func (s *Service) GetScanNOrder(ctx context.Context, merchantID string) (*ScanNOrderIntegration, error) {
	return s.repo.GetScanNOrderIntegration(ctx, merchantID)
}

func (s *Service) GetScanNOrderCurrentImageURL(ctx context.Context, merchantID, column string) (string, error) {
	return s.repo.GetScanNOrderCurrentImageURL(ctx, merchantID, column)
}

func (s *Service) UpdateScanNOrderImageURL(ctx context.Context, merchantID, column, publicURL string) error {
	return s.repo.UpdateScanNOrderImageURL(ctx, merchantID, column, publicURL)
}

func (s *Service) UpdateUberEatsSettings(ctx context.Context, merchantID string, commissionRate int, autoAccept bool) error {
	return s.repo.UpdateUberEatsSettings(ctx, merchantID, commissionRate, autoAccept)
}

func (s *Service) DisableUberEats(ctx context.Context, merchantID string) error {
	return s.repo.DisableUberEats(ctx, merchantID)
}

func (s *Service) UpdateDeliverooSettings(ctx context.Context, merchantID string, commissionRate int, autoAccept bool) error {
	return s.repo.UpdateDeliverooSettings(ctx, merchantID, commissionRate, autoAccept)
}

func (s *Service) DisableDeliveroo(ctx context.Context, merchantID string) error {
	return s.repo.DisableDeliveroo(ctx, merchantID)
}

func (s *Service) UpdateScanNOrderSettings(ctx context.Context, merchantID string, req *UpdateScanNOrderRequest) error {
	return s.repo.UpdateScanNOrderSettings(ctx, merchantID, req)
}

// ─── Stripe Connect ───────────────────────────────────────────────────────────

// GetStripeStatus returns the live Stripe Connect account status for a merchant.
// The account_id is looked up in the DB; if absent, (nil, sql.ErrNoRows) is returned.
func (s *Service) GetStripeStatus(ctx context.Context, merchantID string) (string, error) {
	accountID, err := s.repo.GetStripeAccountID(ctx, merchantID)
	if err != nil {
		return "", err
	}
	return s.stripeManager.GetConnectAccountStatus(accountID)
}

// CreateStripeOnboardingLink generates a Stripe Connect onboarding link.
// Returns an error wrapping "already_verified" if the account is already verified.
func (s *Service) CreateStripeOnboardingLink(ctx context.Context, merchantID string) (string, error) {
	accountID, err := s.repo.GetStripeAccountID(ctx, merchantID)
	if err != nil {
		return "", err
	}

	status, err := s.stripeManager.GetConnectAccountStatus(accountID)
	if err != nil {
		return "", err
	}
	if status == "verified" {
		return "", errors.New("already_verified")
	}

	return s.stripeManager.CreateOnboardingLink(accountID, s.stripeReturnURL, s.stripeRefreshURL)
}

// GetStripeBankAccounts lists the bank accounts linked to the merchant's Stripe Connect account.
func (s *Service) GetStripeBankAccounts(ctx context.Context, merchantID string) ([]stripeclient.BankAccountInfo, error) {
	accountID, err := s.repo.GetStripeAccountID(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return s.stripeManager.GetBankAccounts(accountID)
}

// CreateStripeBankAccountLink generates an account_update link for the merchant to configure IBAN.
func (s *Service) CreateStripeBankAccountLink(ctx context.Context, merchantID string) (string, error) {
	accountID, err := s.repo.GetStripeAccountID(ctx, merchantID)
	if err != nil {
		return "", err
	}
	return s.stripeManager.CreateBankAccountLink(accountID, s.stripeReturnURL, s.stripeRefreshURL)
}

// GetStripeBalance returns the available and pending balance for the merchant's Stripe Connect account.
func (s *Service) GetStripeBalance(ctx context.Context, merchantID string) (*stripeclient.BalanceInfo, error) {
	accountID, err := s.repo.GetStripeAccountID(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return s.stripeManager.GetConnectBalance(accountID)
}
