package repositories

import (
	"context"
	"database/sql"

	"go.uber.org/zap"
)

type CashDrawerRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewCashDrawerRepository(db *sql.DB, log *zap.Logger) *CashDrawerRepository {
	return &CashDrawerRepository{db: db, log: log}
}

func (r *CashDrawerRepository) OpenCashDrawer(ctx context.Context, userID string, deviceID string) error {
	r.log.Info("OpenCashDrawer", zap.String("user_id", userID), zap.String("device_id", deviceID))

	// Pas d'opération DB encore, mais futur audit/logging
	return nil
}
