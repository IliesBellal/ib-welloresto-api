package pos

import (
	"context"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// CreateMerchant creates a new merchant with its satellite tables inside a single
// transaction. If req.UserID is non-empty the user is linked in the same transaction.
func (s *POSService) CreateMerchant(ctx context.Context, req CreateMerchantRequest) (CreateMerchantResponse, error) {
	if strings.TrimSpace(req.FullName) == "" ||
		strings.TrimSpace(req.SIRET) == "" ||
		strings.TrimSpace(req.Tel) == "" {
		return CreateMerchantResponse{}, models.ErrInvalidInput
	}

	merchantToken, err := helpers.GenerateToken(10) // 20-char hex token → VARCHAR(20)
	if err != nil {
		return CreateMerchantResponse{}, err
	}

	// Step 1 — create merchant row
	merchantID, err := s.posRepo.InsertMerchant(ctx, req, merchantToken)
	if err != nil {
		return CreateMerchantResponse{}, err
	}

	// Step 2 — initialise companion tables
	if err := s.posRepo.InitMerchantSatellites(ctx, merchantID); err != nil {
		return CreateMerchantResponse{}, err
	}

	// Step 3 — optional user linkage (within same transaction)
	if strings.TrimSpace(req.UserID) != "" {
		if _, _, err := s.insertUserRightsTx(ctx, req.UserID, merchantID, req.Admin); err != nil {
			return CreateMerchantResponse{}, err
		}
	}

	return CreateMerchantResponse{MerchantID: merchantID}, nil
}

// LinkUser links an existing user to an existing merchant with given rights.
func (s *POSService) LinkUser(ctx context.Context, req LinkUserRequest) (LinkUserResponse, error) {
	if strings.TrimSpace(req.UserID) == "" || req.MerchantID == "" {
		return LinkUserResponse{}, models.ErrInvalidInput
	}

	token, id, err := s.insertUserRightsTx(ctx, req.UserID, req.MerchantID, req.Admin)
	if err != nil {
		return LinkUserResponse{}, err
	}

	return LinkUserResponse{RightsID: id, Token: token}, nil
}

// insertUserRightsTx is the shared helper that inserts a users_rights row and
// returns (token, rowID, error). It is used both by CreateMerchant and LinkUser.
func (s *POSService) insertUserRightsTx(ctx context.Context, userID, merchantID string, admin bool) (string, int, error) {
	rightsToken, err := helpers.GenerateToken(16) // 32-char hex token → VARCHAR(255)
	if err != nil {
		return "", 0, err
	}

	id, err := s.posRepo.InsertUserRights(ctx, userID, merchantID, admin, rightsToken)
	if err != nil {
		return "", 0, err
	}

	return rightsToken, id, nil
}
