package pos

import (
	"context"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// CreateMerchant creates a new merchant with its satellite tables inside a single
// transaction. If req.UserID is non-empty the user is linked in the same transaction.
func (s *POSService) CreateMerchant(ctx context.Context, req CreateMerchantRequest) (CreateMerchantResponse, error) {
	if strings.TrimSpace(req.FullName) == "" ||
		strings.TrimSpace(req.SIRET) == "" ||
		strings.TrimSpace(req.Tel) == "" ||
		strings.TrimSpace(req.PackageID) == "" {
		return CreateMerchantResponse{}, models.ErrInvalidInput
	}

	merchantToken, err := helpers.GenerateToken(10) // 20-char hex token → VARCHAR(20)
	if err != nil {
		return CreateMerchantResponse{}, err
	}

	var merchantID string
	err = dbutils.RunInTx(ctx, s.posRepo.database, func(txCtx context.Context) error {
		// Step 1 — create merchant row
		merchantID, err = s.posRepo.InsertMerchant(txCtx, req, merchantToken)
		if err != nil {
			return err
		}

		// Step 2 — create subscription from the requested package
		if err := s.posRepo.InsertSubscription(txCtx, merchantID, strings.TrimSpace(req.PackageID)); err != nil {
			return err
		}

		// Step 3 — initialise companion tables
		if err := s.posRepo.InitMerchantSatellites(txCtx, merchantID); err != nil {
			return err
		}

		// Step 4 — optional user linkage
		if strings.TrimSpace(req.UserID) != "" {
			if _, _, err := s.insertUserRightsTx(txCtx, req.UserID, merchantID, req.Admin); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return CreateMerchantResponse{}, err
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
