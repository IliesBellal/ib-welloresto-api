package integrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/logger"
	deliverooModule "welloresto-api/internal/modules/deliveroo"
	uberModule "welloresto-api/internal/modules/ubereats"

	"go.uber.org/zap"
)

// logoDownloadClient is the HTTP client used to fetch logo images from R2.
// A dedicated client with a bounded timeout prevents runaway goroutines under load.
var logoDownloadClient = &http.Client{Timeout: 30 * time.Second}

type Service struct {
	repo             *Repository
	stripeManager    *stripeclient.StripeManager
	uberService      *uberModule.UberEatsService
	deliverooService *deliverooModule.DeliverooService
	// Stripe Connect redirect URLs (loaded from config).
	stripeReturnURL  string
	stripeRefreshURL string
}

func NewService(
	db *sql.DB,
	stripeManager *stripeclient.StripeManager,
	uberService *uberModule.UberEatsService,
	deliverooService *deliverooModule.DeliverooService,
	stripeReturnURL,
	stripeRefreshURL string,
) *Service {
	return &Service{
		repo:             NewRepository(db),
		stripeManager:    stripeManager,
		uberService:      uberService,
		deliverooService: deliverooService,
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

func (s *Service) UpdateUberEatsSettings(ctx context.Context, merchantID string, req *UpdateIntegrationRequest) error {
	if req.PreparationTimeMinutes != nil {
		if *req.PreparationTimeMinutes <= 0 {
			return fmt.Errorf("preparation_time_minutes must be greater than 0")
		}

		if err := s.uberService.UpdateReadyForPickupTime(ctx, merchantID, *req.PreparationTimeMinutes, false); err != nil {
			return fmt.Errorf("failed to update uber eats preparation time: %w", err)
		}
	}

	return s.repo.UpdateUberEatsSettings(ctx, merchantID, req.CommissionRate, req.AutoAcceptOrders, req.PreparationTimeMinutes)
}

func (s *Service) DisableUberEats(ctx context.Context, merchantID string) error {
	return s.repo.DisableUberEats(ctx, merchantID)
}

func (s *Service) UpdateDeliverooSettings(ctx context.Context, merchantID string, req *UpdateIntegrationRequest) error {
	if req.PreparationTimeMinutes != nil {
		if *req.PreparationTimeMinutes <= 0 {
			return fmt.Errorf("preparation_time_minutes must be greater than 0")
		}

		if err := s.deliverooService.UpdatePreparationTime(ctx, merchantID, *req.PreparationTimeMinutes); err != nil {
			logger.FromContext(ctx).Error("failed to update deliveroo preparation time for merchant",
				zap.String("merchant_id", merchantID),
				zap.Int("preparation_time_minutes", *req.PreparationTimeMinutes),
				zap.Error(err),
			)
		}
	}

	return s.repo.UpdateDeliverooSettings(ctx, merchantID, req.CommissionRate, req.AutoAcceptOrders, req.PreparationTimeMinutes)
}

func (s *Service) DisableDeliveroo(ctx context.Context, merchantID string) error {
	return s.repo.DisableDeliveroo(ctx, merchantID)
}

func (s *Service) UpdateScanNOrderSettings(ctx context.Context, merchantID string, req *UpdateScanNOrderRequest) error {
	return s.repo.UpdateScanNOrderSettings(ctx, merchantID, req)
}

func (s *Service) CloseTemporaryIntegrations(ctx context.Context, merchantID string, req *CloseTemporaryIntegrationsRequest) (time.Time, []string, error) {
	if req.DurationMinutes <= 0 {
		return time.Time{}, nil, fmt.Errorf("duration_minutes must be greater than 0")
	}
	if len(req.AffectedIntegrations) == 0 {
		return time.Time{}, nil, fmt.Errorf("affected_integrations cannot be empty")
	}

	closedUntil := time.Now().UTC().Add(time.Duration(req.DurationMinutes) * time.Minute)
	seen := make(map[string]struct{}, len(req.AffectedIntegrations))
	processed := make([]string, 0, len(req.AffectedIntegrations))

	for _, rawName := range req.AffectedIntegrations {
		name := normalizeIntegrationName(rawName)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}

		switch name {
		case "uber_eats":
			if err := s.uberService.CloseStoreTemporary(ctx, merchantID, req.DurationMinutes); err != nil {
				return time.Time{}, nil, fmt.Errorf("failed to close uber eats temporarily: %w", err)
			}
		case "deliveroo":
			if err := s.deliverooService.CloseStoreTemporary(ctx, merchantID, req.DurationMinutes); err != nil {
				return time.Time{}, nil, fmt.Errorf("failed to close deliveroo temporarily: %w", err)
			}
		case "scannorder":
			if err := s.repo.SetScanNOrderClosedUntil(ctx, merchantID, closedUntil); err != nil {
				return time.Time{}, nil, fmt.Errorf("failed to close scannorder temporarily: %w", err)
			}
		default:
			return time.Time{}, nil, fmt.Errorf("unsupported integration: %s", rawName)
		}

		processed = append(processed, name)
	}

	if len(processed) == 0 {
		return time.Time{}, nil, fmt.Errorf("affected_integrations cannot be empty")
	}

	return closedUntil, processed, nil
}

func normalizeIntegrationName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, "-", "_")
	return name
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

// SyncStripeBranding uploads the merchant's ScanNOrder logo to Stripe Files and updates
// the connected account's branding (logo, icon, primary color).
//
// Returns errors:
//   - "logo_required"        — no logo URL configured in scannorder_settings
//   - "logo_download_failed" — the logo could not be fetched from storage
//   - sql.ErrNoRows          — the merchant has no Stripe account or ScanNOrder settings
//   - Stripe API errors      — forwarded as-is
func (s *Service) SyncStripeBranding(ctx context.Context, merchantID string) (*StripeBrandingResult, error) {
	data, err := s.repo.GetStripeBrandingData(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	if !data.LogoURL.Valid || data.LogoURL.String == "" {
		return nil, errors.New("logo_required")
	}

	// Download logo from R2 using the request context so the download is
	// cancelled automatically if the client disconnects.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, data.LogoURL.String, nil)
	if err != nil {
		return nil, fmt.Errorf("logo_download_failed: %w", err)
	}
	resp, err := logoDownloadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logo_download_failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("logo_download_failed: unexpected status %d", resp.StatusCode)
	}

	filename := path.Base(data.LogoURL.String)
	if filename == "" || filename == "." {
		filename = "logo"
	}

	fileID, err := s.stripeManager.UploadBrandingFile(ctx, resp.Body, filename)
	if err != nil {
		return nil, err
	}

	primaryColor := normalizeHexColor(data.PrimaryColor)

	if err := s.stripeManager.UpdateAccountBranding(ctx, data.AccountID, fileID, primaryColor); err != nil {
		return nil, err
	}

	return &StripeBrandingResult{
		LogoFileID:   fileID,
		PrimaryColor: primaryColor,
	}, nil
}

// normalizeHexColor ensures the color starts with '#'.
// Returns "" for empty or non-hex strings to signal Stripe should not update the color.
func normalizeHexColor(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	if !strings.HasPrefix(c, "#") {
		c = "#" + c
	}
	return c
}
