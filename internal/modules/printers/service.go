package printers

import (
	"context"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// ListPrinters returns all enabled printers for the authenticated merchant.
func (s *Service) ListPrinters(ctx context.Context, token string) ([]PrinterEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListPrinters(ctx, user.MerchantID)
}

// GetPrinter returns a single printer owned by the authenticated merchant.
func (s *Service) GetPrinter(ctx context.Context, token, printerID string) (*PrinterEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.GetPrinter(ctx, user.MerchantID, printerID)
}

// CreatePrinter validates, generates an ID, derives language from role, and persists.
func (s *Service) CreatePrinter(ctx context.Context, token string, req *CreatePrinterRequest) (*PrinterEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	printerID := helpers.GeneratePrefixedID(helpers.PrinterIDPrefix)
	language := languageForRole(req.Role)

	return s.repo.CreatePrinter(ctx, user.MerchantID, req, printerID, language)
}

// UpdatePrinter validates and applies a partial update to a merchant-owned printer.
func (s *Service) UpdatePrinter(ctx context.Context, token, printerID string, req *UpdatePrinterRequest) (*PrinterEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	return s.repo.UpdatePrinter(ctx, user.MerchantID, printerID, req)
}

// DeletePrinter soft-deletes a printer owned by the authenticated merchant.
func (s *Service) DeletePrinter(ctx context.Context, token, printerID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	return s.repo.DeletePrinter(ctx, user.MerchantID, printerID)
}
