// notification/notification_repository.go

package notification

import (
	"context"
	"database/sql"
	"errors"

	"go.uber.org/zap"
)

type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB, log *zap.Logger) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) GetDeviceTokens(ctx context.Context, merchantID string) ([]string, error) {

	rows, err := r.db.QueryContext(ctx, `
        SELECT fcm_token
        FROM users_devices ud
        INNER JOIN users u ON u.user_id = ud.user_id
        WHERE u.merchant_id = ?
        -- AND u.lastAccess > DATE_ADD(UTC_TIMESTAMP(), INTERVAL -12 HOUR)
    `, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []string

	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}

	return tokens, nil
}

func (r *NotificationRepository) GetValidFCMToken(ctx context.Context) (string, error) {

	var token string

	err := r.db.QueryRowContext(ctx, `
        SELECT access_token
        FROM firebase_fcm_access_token
        WHERE UTC_TIMESTAMP() <= expiration_date
        LIMIT 1
    `).Scan(&token)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	return token, err
}

func (r *NotificationRepository) StoreFCMToken(ctx context.Context, token string) error {

	_, err := r.db.ExecContext(ctx, `
        INSERT INTO firebase_fcm_access_token(access_token, expiration_date)
        VALUES(?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 55 MINUTE))
    `, token)

	return err
}
