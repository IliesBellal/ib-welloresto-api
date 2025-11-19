package repositories

import (
	"context"
	"database/sql"
)

type DeviceRepository struct {
	db *sql.DB
}

func NewDeviceRepository(db *sql.DB) *DeviceRepository {
	return &DeviceRepository{db: db}
}

func (r *DeviceRepository) SaveDevice(ctx context.Context, userID, merchantID, app, deviceID, fcmToken string) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	q := `
INSERT INTO users_devices
(user_id, merchant_id, app, device_id, fcm_token, last_used)
VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP())
ON DUPLICATE KEY UPDATE
    fcm_token = VALUES(fcm_token),
    last_used = UTC_TIMESTAMP(),
    user_id = VALUES(user_id),
    merchant_id = VALUES(merchant_id)
`

	_, execErr := tx.ExecContext(ctx, q, userID, merchantID, app, deviceID, fcmToken)
	if execErr != nil {
		tx.Rollback()
		return execErr
	}

	return tx.Commit()
}
