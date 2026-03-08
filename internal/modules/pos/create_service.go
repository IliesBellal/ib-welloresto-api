package pos

import (
	"context"
	"database/sql"
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

	tx, err := s.posRepo.db.BeginTx(ctx, nil)
	if err != nil {
		return CreateMerchantResponse{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Step 1 — create merchant row
	merchantID, err := s.posRepo.InsertMerchant(ctx, tx, req, merchantToken)
	if err != nil {
		return CreateMerchantResponse{}, err
	}

	// Step 2 — initialise companion tables
	if err := s.posRepo.InitMerchantSatellites(ctx, tx, merchantID); err != nil {
		return CreateMerchantResponse{}, err
	}

	// Step 3 — optional user linkage (within same transaction)
	if strings.TrimSpace(req.UserID) != "" {
		if _, _, err := s.insertUserRightsTx(ctx, tx, req.UserID, merchantID, req.Admin, req.Waiter); err != nil {
			return CreateMerchantResponse{}, err
		}
	}

	return CreateMerchantResponse{MerchantID: merchantID}, tx.Commit()
}

// LinkUser links an existing user to an existing merchant with given rights.
func (s *POSService) LinkUser(ctx context.Context, req LinkUserRequest) (LinkUserResponse, error) {
	if strings.TrimSpace(req.UserID) == "" || req.MerchantID == 0 {
		return LinkUserResponse{}, models.ErrInvalidInput
	}

	tx, err := s.posRepo.db.BeginTx(ctx, nil)
	if err != nil {
		return LinkUserResponse{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	token, id, err := s.insertUserRightsTx(ctx, tx, req.UserID, req.MerchantID, req.Admin, req.Waiter)
	if err != nil {
		return LinkUserResponse{}, err
	}

	return LinkUserResponse{RightsID: id, Token: token}, tx.Commit()
}

// insertUserRightsTx is the shared helper that inserts a users_rights row and
// returns (token, rowID, error). It is used both by CreateMerchant and LinkUser.
func (s *POSService) insertUserRightsTx(ctx context.Context, tx *sql.Tx, userID string, merchantID int, admin, waiter bool) (string, int, error) {
	rightsToken, err := helpers.GenerateToken(16) // 32-char hex token → VARCHAR(255)
	if err != nil {
		return "", 0, err
	}

	id, err := s.posRepo.InsertUserRights(ctx, tx, userID, merchantID, admin, waiter, rightsToken)
	if err != nil {
		return "", 0, err
	}

	return rightsToken, id, nil
}
